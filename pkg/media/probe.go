// Package media inspects and transforms uploaded video, without trusting the
// client.
//
// La durée est LUE DANS LE FICHIER, pas déclarée par l'appelant : un contrôle
// côté navigateur se contourne en trois lignes, et une plateforme de shorts
// dont la limite de durée est facultative n'a pas de limite.
package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// ErrUnknownDuration is returned when the container is readable but carries no
// usable duration. L'appelant décide : refuser vaut mieux qu'accepter à
// l'aveugle sur une plateforme où la durée est une règle.
var ErrUnknownDuration = errors.New("media: duration not found in container")

// maxProbeBytes bounds how far we scan for the metadata boxes. Un `moov` placé
// en fin de fichier peut être loin, mais scanner tout un fichier de 60 Mo pour
// une entête serait absurde ; au-delà, on sait déjà que la lecture ne sera pas
// fluide (cf. faststart).
const maxProbeBytes = 24 << 20 // 24 MiB

// Duration reads the playing time of an mp4/mov (ISO BMFF) or webm (Matroska)
// stream. The reader is rewound before returning.
func Duration(r io.ReadSeeker, contentType string) (time.Duration, error) {
	defer func() { _, _ = r.Seek(0, io.SeekStart) }()
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	switch contentType {
	case "video/mp4", "video/quicktime":
		return isoDuration(r)
	case "video/webm":
		return matroskaDuration(r)
	default:
		return 0, ErrUnknownDuration
	}
}

// --- ISO base media file format (mp4, mov) ---------------------------------

// isoDuration walks the top-level boxes to `moov`, then its `mvhd`.
func isoDuration(r io.ReadSeeker) (time.Duration, error) {
	moovOffset, moovSize, err := findBox(r, 0, math.MaxInt64, "moov")
	if err != nil {
		return 0, err
	}
	mvhdOffset, _, err := findBox(r, moovOffset, moovOffset+moovSize, "mvhd")
	if err != nil {
		return 0, err
	}
	if _, err := r.Seek(mvhdOffset, io.SeekStart); err != nil {
		return 0, err
	}
	// mvhd: version(1) flags(3) then, per version, creation/modification
	// times, timescale and duration.
	head := make([]byte, 4)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, err
	}
	var timescale, duration uint64
	switch head[0] {
	case 0:
		b := make([]byte, 12) // created(4) modified(4) timescale(4) duration(4)
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		timescale = uint64(binary.BigEndian.Uint32(b[8:12]))
		d := make([]byte, 4)
		if _, err := io.ReadFull(r, d); err != nil {
			return 0, err
		}
		duration = uint64(binary.BigEndian.Uint32(d))
	case 1:
		b := make([]byte, 20) // created(8) modified(8) timescale(4)
		if _, err := io.ReadFull(r, b); err != nil {
			return 0, err
		}
		timescale = uint64(binary.BigEndian.Uint32(b[16:20]))
		d := make([]byte, 8)
		if _, err := io.ReadFull(r, d); err != nil {
			return 0, err
		}
		duration = binary.BigEndian.Uint64(d)
	default:
		return 0, ErrUnknownDuration
	}
	if timescale == 0 {
		return 0, ErrUnknownDuration
	}
	if duration == 0 {
		// mp4 fragmenté : `mvhd` ne porte pas la durée, elle est déclarée dans
		// `mvex/mehd`. Sans ce repli, les exports de plusieurs applications
		// mobiles seraient refusés comme illisibles.
		if d, err := fragmentedDuration(r, moovOffset, moovSize); err == nil {
			duration = d
		} else {
			return 0, ErrUnknownDuration
		}
	}
	return time.Duration(float64(duration) / float64(timescale) * float64(time.Second)), nil
}

// fragmentedDuration reads moov/mvex/mehd, the fragment-aware total duration.
func fragmentedDuration(r io.ReadSeeker, moovOffset, moovSize int64) (uint64, error) {
	mvexOffset, mvexSize, err := findBox(r, moovOffset, moovOffset+moovSize, "mvex")
	if err != nil {
		return 0, err
	}
	mehdOffset, _, err := findBox(r, mvexOffset, mvexOffset+mvexSize, "mehd")
	if err != nil {
		return 0, err
	}
	if _, err := r.Seek(mehdOffset, io.SeekStart); err != nil {
		return 0, err
	}
	head := make([]byte, 4) // version(1) flags(3)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, err
	}
	width := 4
	if head[0] == 1 {
		width = 8
	}
	b := make([]byte, width)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	if width == 8 {
		return binary.BigEndian.Uint64(b), nil
	}
	return uint64(binary.BigEndian.Uint32(b)), nil
}

// findBox scans boxes between [from, until) and returns the payload offset and
// size of the first one named `name`. Recherche NON récursive : les boîtes
// visées (`moov` au premier niveau, `mvhd` dans `moov`) sont des enfants
// directs, inutile de descendre plus bas.
func findBox(r io.ReadSeeker, from, until int64, name string) (offset, size int64, err error) {
	pos := from
	header := make([]byte, 8)
	for pos < until && pos < maxProbeBytes {
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return 0, 0, err
		}
		if _, err := io.ReadFull(r, header); err != nil {
			return 0, 0, ErrUnknownDuration
		}
		boxSize := int64(binary.BigEndian.Uint32(header[0:4]))
		boxType := string(header[4:8])
		headerLen := int64(8)
		switch {
		case boxSize == 1: // taille 64 bits, juste après l'entête
			ext := make([]byte, 8)
			if _, err := io.ReadFull(r, ext); err != nil {
				return 0, 0, ErrUnknownDuration
			}
			boxSize = int64(binary.BigEndian.Uint64(ext))
			headerLen = 16
		case boxSize == 0: // la boîte court jusqu'à la fin du fichier
			end, err := r.Seek(0, io.SeekEnd)
			if err != nil {
				return 0, 0, err
			}
			boxSize = end - pos
		}
		if boxSize < headerLen {
			return 0, 0, ErrUnknownDuration
		}
		if boxType == name {
			return pos + headerLen, boxSize - headerLen, nil
		}
		pos += boxSize
	}
	return 0, 0, ErrUnknownDuration
}

// --- Matroska / WebM -------------------------------------------------------

const (
	idSegment       = 0x18538067
	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idDuration      = 0x4489
)

// matroskaDuration descends Segment → Info and combines Duration with
// TimecodeScale (nanoseconds per tick, 1 ms by default).
func matroskaDuration(r io.ReadSeeker) (time.Duration, error) {
	if err := descendTo(r, idSegment); err != nil {
		return 0, err
	}
	if err := descendTo(r, idInfo); err != nil {
		return 0, err
	}
	scale := uint64(1_000_000) // défaut Matroska : 1 ms par tick
	var ticks float64
	for {
		id, size, err := readElementHeader(r)
		if err != nil {
			break
		}
		switch id {
		case idTimecodeScale:
			v, err := readUint(r, size)
			if err != nil {
				return 0, err
			}
			if v > 0 {
				scale = v
			}
		case idDuration:
			v, err := readFloat(r, size)
			if err != nil {
				return 0, err
			}
			ticks = v
		default:
			if _, err := r.Seek(size, io.SeekCurrent); err != nil {
				return 0, err
			}
		}
		if ticks > 0 {
			break
		}
	}
	if ticks <= 0 {
		return 0, ErrUnknownDuration
	}
	return time.Duration(ticks * float64(scale)), nil
}

// descendTo finds a master element among siblings and positions the reader on
// its first child.
func descendTo(r io.ReadSeeker, want uint32) error {
	for {
		pos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		if pos > maxProbeBytes {
			return ErrUnknownDuration
		}
		id, size, err := readElementHeader(r)
		if err != nil {
			return ErrUnknownDuration
		}
		if id == want {
			return nil // le curseur est déjà sur le premier enfant
		}
		if _, err := r.Seek(size, io.SeekCurrent); err != nil {
			return ErrUnknownDuration
		}
	}
}

// readElementHeader reads an EBML id and size (both variable-length).
func readElementHeader(r io.Reader) (id uint32, size int64, err error) {
	first := make([]byte, 1)
	if _, err := io.ReadFull(r, first); err != nil {
		return 0, 0, err
	}
	idLen := leadingZeros(first[0]) + 1
	if idLen > 4 {
		return 0, 0, fmt.Errorf("media: invalid ebml id length %d", idLen)
	}
	buf := make([]byte, idLen)
	buf[0] = first[0]
	if idLen > 1 {
		if _, err := io.ReadFull(r, buf[1:]); err != nil {
			return 0, 0, err
		}
	}
	for _, b := range buf {
		id = id<<8 | uint32(b)
	}

	if _, err := io.ReadFull(r, first); err != nil {
		return 0, 0, err
	}
	sizeLen := leadingZeros(first[0]) + 1
	if sizeLen > 8 {
		return 0, 0, fmt.Errorf("media: invalid ebml size length %d", sizeLen)
	}
	// Le marqueur de longueur est retiré du premier octet.
	value := uint64(first[0]) & (0xFF >> uint(sizeLen))
	if sizeLen > 1 {
		rest := make([]byte, sizeLen-1)
		if _, err := io.ReadFull(r, rest); err != nil {
			return 0, 0, err
		}
		for _, b := range rest {
			value = value<<8 | uint64(b)
		}
	}
	return id, int64(value), nil
}

func leadingZeros(b byte) int {
	for i := 0; i < 8; i++ {
		if b&(0x80>>uint(i)) != 0 {
			return i
		}
	}
	return 8
}

func readUint(r io.Reader, size int64) (uint64, error) {
	if size <= 0 || size > 8 {
		return 0, ErrUnknownDuration
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v, nil
}

func readFloat(r io.Reader, size int64) (float64, error) {
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	switch size {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), nil
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
	default:
		return 0, ErrUnknownDuration
	}
}
