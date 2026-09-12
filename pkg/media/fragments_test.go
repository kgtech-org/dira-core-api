package media

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- constructeurs d'un mp4 fragmenté minimal --------------------------------

func u32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }

func fullBox(name string, version byte, flags uint32, payload []byte) []byte {
	head := u32(flags & 0xffffff)
	head[0] = version
	return box(name, append(head, payload...))
}

// trak : tkhd (id) + mdia/mdhd (timescale).
func trak(id, timescale uint32) []byte {
	tkhd := fullBox("tkhd", 0, 0, append(make([]byte, 8), u32(id)...))
	mdhd := fullBox("mdhd", 0, 0, append(make([]byte, 8), u32(timescale)...))
	return box("trak", append(tkhd, box("mdia", mdhd)...))
}

func trex(id, defaultDuration uint32) []byte {
	return fullBox("trex", 0, 0, bytes.Join([][]byte{u32(id), u32(1), u32(defaultDuration), u32(0), u32(0)}, nil))
}

// moof avec un traf : tfhd (durée par défaut si > 0) et un trun de `count`
// échantillons ; `perSample` écrit une durée par échantillon.
func moof(id, count, defaultDuration uint32, perSample []uint32) []byte {
	var tfhd []byte
	if defaultDuration > 0 {
		tfhd = fullBox("tfhd", 0, 0x8, append(u32(id), u32(defaultDuration)...))
	} else {
		tfhd = fullBox("tfhd", 0, 0, u32(id))
	}
	var trun []byte
	if perSample != nil {
		// 0x1 data_offset · 0x100 durée · 0x200 taille : la forme la plus
		// courante quand la durée varie.
		p := append(u32(uint32(len(perSample))), u32(0)...)
		for _, d := range perSample {
			p = append(p, u32(d)...)
			p = append(p, u32(1234)...)
		}
		trun = fullBox("trun", 0, 0x301, p)
	} else {
		trun = fullBox("trun", 0, 0x205, bytes.Join([][]byte{u32(count), u32(0), u32(0)}, nil))
	}
	return box("moof", append(box("mfhd", make([]byte, 8)), box("traf", append(tfhd, trun...))...))
}

func fragmentedFile(moovChildren []byte, fragments ...[]byte) *bytes.Reader {
	var f []byte
	f = append(f, box("ftyp", []byte("isomiso2avc1iso6"))...)
	f = append(f, box("moov", moovChildren)...)
	for _, frag := range fragments {
		f = append(f, frag...)
		f = append(f, box("mdat", bytes.Repeat([]byte{0x22}, 256))...)
	}
	return bytes.NewReader(f)
}

// Un export en flux (ffmpeg `frag_keyframe+empty_moov`, applications de
// capture) : `mvhd` à zéro, PAS de `mehd`. La durée est la somme des
// fragments, dans l'échelle de la piste.
func TestDurationSumsFragmentsWhenTheHeaderHasNoTotal(t *testing.T) {
	moov := bytes.Join([][]byte{mvhdV0(1000, 0), trak(1, 15360), box("mvex", trex(1, 0))}, nil)
	// 4 × 250 + 89 échantillons de 256 ticks à 15 360 Hz = 18,15 s — le
	// profil exact du fichier qui a déclenché la correction.
	frags := [][]byte{}
	for i := 0; i < 4; i++ {
		frags = append(frags, moof(1, 250, 256, nil))
	}
	frags = append(frags, moof(1, 89, 256, nil))

	d, err := Duration(fragmentedFile(moov, frags...), "video/mp4")
	require.NoError(t, err)
	assert.InDelta(t, 18.15, d.Seconds(), 0.01)
}

func TestFragmentsFallBackOnTrexDefaultAndReadPerSampleDurations(t *testing.T) {
	moov := bytes.Join([][]byte{mvhdV0(1000, 0), trak(1, 1000), box("mvex", trex(1, 40))}, nil)
	// Premier fragment : pas de durée dans tfhd → celle de trex (40 ms × 25).
	// Second : une durée par échantillon (0x100).
	d, err := Duration(fragmentedFile(moov, moof(1, 25, 0, nil), moof(1, 0, 0, []uint32{500, 250, 250})), "video/mp4")
	require.NoError(t, err)
	assert.InDelta(t, 2.0, d.Seconds(), 0.001)
}

// Deux pistes (vidéo et son) : la durée du film est celle de la plus longue,
// chacune dans sa propre échelle.
func TestFragmentsKeepTheLongestTrack(t *testing.T) {
	moov := bytes.Join([][]byte{mvhdV0(1000, 0), trak(1, 30), trak(2, 48000), box("mvex", append(trex(1, 1), trex(2, 1024)...))}, nil)
	d, err := Duration(fragmentedFile(moov, moof(1, 90, 0, nil), moof(2, 150, 0, nil)), "video/mp4")
	require.NoError(t, err)
	assert.InDelta(t, 3.2, d.Seconds(), 0.001, "150 × 1024 / 48 000 = 3,2 s > 90 / 30 = 3 s")
}

// `mehd` présent : il reste la référence, les fragments ne sont pas relus.
func TestMehdStillWinsOverFragments(t *testing.T) {
	mehd := make([]byte, 8)
	binary.BigEndian.PutUint32(mehd[4:8], 1000*42)
	moov := bytes.Join([][]byte{mvhdV0(1000, 0), trak(1, 1000), box("mvex", append(box("mehd", mehd), trex(1, 1)...))}, nil)
	d, err := Duration(fragmentedFile(moov, moof(1, 5, 0, nil)), "video/mp4")
	require.NoError(t, err)
	assert.InDelta(t, 42.0, d.Seconds(), 0.01)
}

// Une entête sans fragment derrière — ou un trun qui annonce plus
// d'échantillons que sa boîte n'en contient — reste « inconnue », jamais 0 s.
func TestFragmentsStayUnknownWhenTheyLie(t *testing.T) {
	moov := bytes.Join([][]byte{mvhdV0(1000, 0), trak(1, 1000), box("mvex", trex(1, 40))}, nil)
	_, err := Duration(fragmentedFile(moov), "video/mp4")
	assert.ErrorIs(t, err, ErrUnknownDuration)

	lying := fullBox("trun", 0, 0x101, append(u32(1_000_000), u32(0)...))
	frag := box("moof", box("traf", append(fullBox("tfhd", 0, 0, u32(1)), lying...)))
	_, err = Duration(fragmentedFile(moov, frag), "video/mp4")
	assert.ErrorIs(t, err, ErrUnknownDuration)
}
