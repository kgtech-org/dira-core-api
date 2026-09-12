package media

import (
	"encoding/binary"
	"io"
	"time"
)

// --- mp4 fragmenté SANS `mehd` ----------------------------------------------
//
// Un mp4 fragmenté (`moov` en tête, puis des paires `moof`/`mdat`) est ce que
// produisent beaucoup d'encodeurs en flux — dont ffmpeg avec `-movflags
// frag_keyframe+empty_moov`, et plusieurs applications de capture. Dans ce
// cas `mvhd.duration` vaut zéro, et la durée totale N'EST écrite nulle part
// si l'encodeur ne connaissait pas la fin au moment d'écrire l'entête :
// `mvex/mehd` est absent. La seule vérité est alors dans les fragments
// eux-mêmes — chaque `trun` déclare ses échantillons et leur durée.
//
// On ADDITIONNE donc les fragments, piste par piste, dans l'échelle de temps
// de la piste (`mdhd.timescale`, pas celle du film). Le fichier est parcouru
// en entier, mais par ses entêtes de boîtes seulement : une trentaine de
// sauts pour 36 Mo, pas une lecture.

// fragmentsDuration sums the sample durations declared in `moof/traf/trun`
// boxes, using the per-track timescale from `moov/trak/mdia/mdhd` and the
// defaults from `moov/mvex/trex`.
func fragmentsDuration(r io.ReadSeeker, moovOffset, moovSize int64) (time.Duration, error) {
	timescales := map[uint32]uint32{}
	trexDefaults := map[uint32]uint32{}
	err := eachBox(r, moovOffset, moovOffset+moovSize, func(typ string, off, size int64) error {
		switch typ {
		case "trak":
			id, ts, err := trackHeader(r, off, off+size)
			if err == nil && ts > 0 {
				timescales[id] = ts
			}
		case "mvex":
			return eachBox(r, off, off+size, func(typ string, off, size int64) error {
				if typ == "trex" {
					b := make([]byte, 12) // version/flags(4) track_id(4) default_sample_description_index(4)
					if err := readAt(r, off, b); err != nil {
						return err
					}
					d := make([]byte, 4) // default_sample_duration
					if err := readAt(r, off+12, d); err != nil {
						return err
					}
					trexDefaults[binary.BigEndian.Uint32(b[4:8])] = binary.BigEndian.Uint32(d)
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(timescales) == 0 {
		return 0, ErrUnknownDuration
	}

	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	sums := map[uint32]uint64{}
	err = eachBox(r, 0, end, func(typ string, off, size int64) error {
		if typ != "moof" {
			return nil
		}
		return eachBox(r, off, off+size, func(typ string, off, size int64) error {
			if typ != "traf" {
				return nil
			}
			return sumTrackFragment(r, off, off+size, trexDefaults, sums)
		})
	})
	if err != nil {
		return 0, err
	}
	var longest time.Duration
	for id, ticks := range sums {
		ts, ok := timescales[id]
		if !ok || ts == 0 {
			continue
		}
		if d := time.Duration(float64(ticks) / float64(ts) * float64(time.Second)); d > longest {
			longest = d
		}
	}
	if longest == 0 {
		return 0, ErrUnknownDuration
	}
	return longest, nil
}

// sumTrackFragment adds the durations of one `traf` to its track's total.
func sumTrackFragment(r io.ReadSeeker, from, until int64, trexDefaults map[uint32]uint32, sums map[uint32]uint64) error {
	var trackID uint32
	var defaultDuration uint32
	haveDefault := false
	return eachBox(r, from, until, func(typ string, off, size int64) error {
		switch typ {
		case "tfhd":
			head := make([]byte, 8) // version(1) flags(3) track_id(4)
			if err := readAt(r, off, head); err != nil {
				return err
			}
			flags := binary.BigEndian.Uint32(head[0:4]) & 0xffffff
			trackID = binary.BigEndian.Uint32(head[4:8])
			pos := off + 8
			if flags&0x1 != 0 { // base_data_offset
				pos += 8
			}
			if flags&0x2 != 0 { // sample_description_index
				pos += 4
			}
			if flags&0x8 != 0 { // default_sample_duration
				d := make([]byte, 4)
				if err := readAt(r, pos, d); err != nil {
					return err
				}
				defaultDuration = binary.BigEndian.Uint32(d)
				haveDefault = true
			}
		case "trun":
			head := make([]byte, 8) // version(1) flags(3) sample_count(4)
			if err := readAt(r, off, head); err != nil {
				return err
			}
			flags := binary.BigEndian.Uint32(head[0:4]) & 0xffffff
			count := binary.BigEndian.Uint32(head[4:8])
			perSample := flags&0x100 != 0
			if !perSample {
				d := defaultDuration
				if !haveDefault {
					d = trexDefaults[trackID]
				}
				sums[trackID] += uint64(count) * uint64(d)
				return nil
			}
			pos := off + 8
			if flags&0x1 != 0 { // data_offset
				pos += 4
			}
			if flags&0x4 != 0 { // first_sample_flags
				pos += 4
			}
			// Chaque échantillon : durée(4) si 0x100, taille(4) si 0x200,
			// drapeaux(4) si 0x400, décalage de composition(4) si 0x800.
			stride := int64(4)
			for _, f := range []uint32{0x200, 0x400, 0x800} {
				if flags&f != 0 {
					stride += 4
				}
			}
			if int64(count)*stride > size {
				return ErrUnknownDuration // la boîte ment sur son contenu
			}
			buf := make([]byte, int64(count)*stride)
			if err := readAt(r, pos, buf); err != nil {
				return err
			}
			for i := uint32(0); i < count; i++ {
				sums[trackID] += uint64(binary.BigEndian.Uint32(buf[int64(i)*stride:]))
			}
		}
		return nil
	})
}

// trackHeader reads a track's id (`tkhd`) and timescale (`mdia/mdhd`).
func trackHeader(r io.ReadSeeker, from, until int64) (id, timescale uint32, err error) {
	err = eachBox(r, from, until, func(typ string, off, size int64) error {
		switch typ {
		case "tkhd":
			v := make([]byte, 1)
			if err := readAt(r, off, v); err != nil {
				return err
			}
			pos := off + 4 + 8 // version/flags, creation, modification (32 bits)
			if v[0] == 1 {
				pos = off + 4 + 16
			}
			b := make([]byte, 4)
			if err := readAt(r, pos, b); err != nil {
				return err
			}
			id = binary.BigEndian.Uint32(b)
		case "mdia":
			return eachBox(r, off, off+size, func(typ string, off, size int64) error {
				if typ != "mdhd" {
					return nil
				}
				v := make([]byte, 1)
				if err := readAt(r, off, v); err != nil {
					return err
				}
				pos := off + 4 + 8
				if v[0] == 1 {
					pos = off + 4 + 16
				}
				b := make([]byte, 4)
				if err := readAt(r, pos, b); err != nil {
					return err
				}
				timescale = binary.BigEndian.Uint32(b)
				return nil
			})
		}
		return nil
	})
	return id, timescale, err
}

// eachBox walks the boxes in [from, until) and calls fn with each payload.
// Pas de plafond de lecture ici, contrairement à findBox : la marche saute
// les données, elle ne les lit pas, et le fichier est déjà borné en taille
// par la limite d'envoi.
func eachBox(r io.ReadSeeker, from, until int64, fn func(typ string, payloadOffset, payloadSize int64) error) error {
	pos := from
	header := make([]byte, 8)
	for pos+8 <= until {
		if err := readAt(r, pos, header); err != nil {
			return ErrUnknownDuration
		}
		boxSize := int64(binary.BigEndian.Uint32(header[0:4]))
		boxType := string(header[4:8])
		headerLen := int64(8)
		switch {
		case boxSize == 1:
			ext := make([]byte, 8)
			if err := readAt(r, pos+8, ext); err != nil {
				return ErrUnknownDuration
			}
			boxSize = int64(binary.BigEndian.Uint64(ext))
			headerLen = 16
		case boxSize == 0:
			boxSize = until - pos
		}
		if boxSize < headerLen {
			return ErrUnknownDuration
		}
		if err := fn(boxType, pos+headerLen, boxSize-headerLen); err != nil {
			return err
		}
		pos += boxSize
	}
	return nil
}

func readAt(r io.ReadSeeker, off int64, b []byte) error {
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		return err
	}
	_, err := io.ReadFull(r, b)
	return err
}
