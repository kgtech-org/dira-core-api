package media

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- constructeurs de conteneurs minimaux ----------------------------------

func box(name string, payload []byte) []byte {
	b := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(b[0:4], uint32(8+len(payload)))
	copy(b[4:8], name)
	return append(b, payload...)
}

// mvhdV0 builds a version-0 movie header with the given timescale/duration.
func mvhdV0(timescale, duration uint32) []byte {
	p := make([]byte, 4+16)
	p[0] = 0 // version
	binary.BigEndian.PutUint32(p[12:16], timescale)
	binary.BigEndian.PutUint32(p[16:20], duration)
	return box("mvhd", p)
}

func mvhdV1(timescale uint32, duration uint64) []byte {
	p := make([]byte, 4+28)
	p[0] = 1
	binary.BigEndian.PutUint32(p[20:24], timescale)
	binary.BigEndian.PutUint64(p[24:32], duration)
	return box("mvhd", p)
}

// mp4File wraps the movie header the way a real file does: a leading `ftyp`,
// a payload box, and the header last — l'ordre le plus défavorable, et le plus
// courant en sortie de téléphone.
func mp4File(mvhd []byte) *bytes.Reader {
	var f []byte
	f = append(f, box("ftyp", []byte("isomiso2avc1mp41"))...)
	f = append(f, box("mdat", bytes.Repeat([]byte{0x11}, 512))...)
	f = append(f, box("moov", mvhd)...)
	return bytes.NewReader(f)
}

// ebml encodes one element with its variable-length id and size.
func ebml(id uint32, payload []byte) []byte {
	var idBytes []byte
	switch {
	case id > 0x00FFFFFF:
		idBytes = []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	case id > 0x0000FFFF:
		idBytes = []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	case id > 0x000000FF:
		idBytes = []byte{byte(id >> 8), byte(id)}
	default:
		idBytes = []byte{byte(id)}
	}
	// Tailles courtes uniquement : suffisant pour un conteneur de test.
	size := []byte{byte(0x80 | len(payload))}
	out := append(idBytes, size...)
	return append(out, payload...)
}

func f64(v float64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, math.Float64bits(v))
	return b
}

func webmFile(scale uint64, ticks float64) *bytes.Reader {
	scaleBytes := []byte{byte(scale >> 16), byte(scale >> 8), byte(scale)}
	info := append(ebml(idTimecodeScale, scaleBytes), ebml(idDuration, f64(ticks))...)
	var f []byte
	f = append(f, ebml(0x1A45DFA3, []byte{0x42, 0x86, 0x81, 0x01})...) // entête EBML
	f = append(f, ebml(idSegment, ebml(idInfo, info))...)
	return bytes.NewReader(f)
}

// --- durée -----------------------------------------------------------------

func TestDurationReadsMP4Header(t *testing.T) {
	// 600 ticks à 1000 Hz = 0,6 s ; 90 000 ticks à 600 Hz = 150 s.
	cases := []struct {
		name      string
		timescale uint32
		duration  uint32
		want      time.Duration
	}{
		{"short de 12 s", 1000, 12_000, 12 * time.Second},
		{"pile 60 s", 600, 36_000, 60 * time.Second},
		{"long métrage", 600, 90_000, 150 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Duration(mp4File(mvhdV0(tc.timescale, tc.duration)), "video/mp4")
			require.NoError(t, err)
			assert.InDelta(t, tc.want.Seconds(), got.Seconds(), 0.01)
		})
	}
}

// Les caméras récentes écrivent des mvhd version 1 (horloges 64 bits).
func TestDurationReadsMP4Version1Header(t *testing.T) {
	got, err := Duration(mp4File(mvhdV1(90_000, 90_000*45)), "video/quicktime")
	require.NoError(t, err)
	assert.InDelta(t, 45.0, got.Seconds(), 0.01)
}

func TestDurationReadsWebM(t *testing.T) {
	// 1 ms par tick, 30 000 ticks = 30 s.
	got, err := Duration(webmFile(1_000_000, 30_000), "video/webm")
	require.NoError(t, err)
	assert.InDelta(t, 30.0, got.Seconds(), 0.01)
}

// La sonde doit rendre le flux relisible : l'appelant enchaîne sur le stockage.
func TestDurationRewindsTheReader(t *testing.T) {
	r := mp4File(mvhdV0(1000, 5000))
	_, err := Duration(r, "video/mp4")
	require.NoError(t, err)

	head := make([]byte, 8)
	_, err = r.Read(head)
	require.NoError(t, err)
	assert.Equal(t, "ftyp", string(head[4:8]), "le curseur doit être revenu au début")
}

// Un fichier illisible ne doit pas passer pour une vidéo de durée nulle :
// l'appelant doit pouvoir distinguer « 0 s » de « je ne sais pas ».
func TestDurationRejectsUnreadableInput(t *testing.T) {
	cases := map[string]struct {
		body        []byte
		contentType string
	}{
		"octets aléatoires": {bytes.Repeat([]byte{0x7F}, 256), "video/mp4"},
		"vide":              {nil, "video/mp4"},
		"mp4 sans moov":     {box("ftyp", []byte("isom")), "video/mp4"},
		"type non géré":     {bytes.Repeat([]byte{0x00}, 64), "video/x-msvideo"},
		"webm sans segment": {ebml(0x1A45DFA3, []byte{0x42}), "video/webm"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Duration(bytes.NewReader(tc.body), tc.contentType)
			assert.Error(t, err)
		})
	}
}

// Une taille de boîte plus petite que son entête ferait boucler indéfiniment
// un scanner naïf : un fichier hostile ne doit pas immobiliser le serveur.
func TestDurationSurvivesMalformedBoxSizes(t *testing.T) {
	malformed := []byte{0x00, 0x00, 0x00, 0x02, 'm', 'o', 'o', 'v'} // taille 2 < entête
	done := make(chan struct{})
	go func() {
		_, _ = Duration(bytes.NewReader(malformed), "video/mp4")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("la sonde ne se termine pas sur une boîte de taille invalide")
	}
}

// Plusieurs applications mobiles exportent du mp4 fragmenté : `mvhd` y déclare
// une durée nulle, la vraie valeur vit dans `mvex/mehd`.
func TestDurationReadsFragmentedMP4(t *testing.T) {
	mehd := make([]byte, 4+4)
	binary.BigEndian.PutUint32(mehd[4:8], 1000*42) // 42 s à 1000 Hz
	moov := append(mvhdV0(1000, 0), box("mvex", box("mehd", mehd))...)

	got, err := Duration(mp4File(moov), "video/mp4")
	require.NoError(t, err)
	assert.InDelta(t, 42.0, got.Seconds(), 0.01)
}

// Les conteneurs fabriqués plus haut valident la logique ; ceux-ci valident la
// réalité. Ce sont deux sorties ffmpeg authentiques du même contenu : l'une
// avec `+faststart` (index en tête), l'autre sans (index en fin de fichier).
func TestDurationReadsRealFFmpegOutput(t *testing.T) {
	for _, name := range []string{"real-faststart.mp4", "real-trailing-moov.mp4"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("testdata", name))
			require.NoError(t, err)
			defer f.Close()

			d, err := Duration(f, "video/mp4")
			require.NoError(t, err)
			assert.InDelta(t, 2.0, d.Seconds(), 0.15)
		})
	}
}

// La position de `moov` EST le sujet : un index en fin de fichier oblige le
// navigateur à tout télécharger avant d'afficher la première image. Ce test
// fige la propriété que le transcodage doit produire.
func TestFaststartPutsTheIndexBeforeTheData(t *testing.T) {
	offsets := func(name string) (moov, mdat int64) {
		f, err := os.Open(filepath.Join("testdata", name))
		require.NoError(t, err)
		defer f.Close()
		moov, _, err = findBox(f, 0, math.MaxInt64, "moov")
		require.NoError(t, err)
		mdat, _, err = findBox(f, 0, math.MaxInt64, "mdat")
		require.NoError(t, err)
		return moov, mdat
	}

	moov, mdat := offsets("real-faststart.mp4")
	assert.Less(t, moov, mdat, "avec +faststart, l'index précède les données")

	moov, mdat = offsets("real-trailing-moov.mp4")
	assert.Greater(t, moov, mdat, "sans +faststart, l'index suit les données — c'est le défaut corrigé")
}
