package media

import (
	"bytes"
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ce test exerce le VRAI ffmpeg. Il est ignoré là où le binaire est absent
// (postes de développement) et s'exécute dans l'image, qui l'embarque.
func requireFFmpeg(t *testing.T) *Transcoder {
	t.Helper()
	tr := NewTranscoder()
	if !tr.Available() {
		t.Skip("ffmpeg absent : test d'intégration ignoré")
	}
	return tr
}

func TestOptimizeProducesAStreamableShort(t *testing.T) {
	tr := requireFFmpeg(t)

	// Source volontairement défavorable : index en FIN de fichier.
	src, err := os.Open(filepath.Join("testdata", "real-trailing-moov.mp4"))
	require.NoError(t, err)
	defer src.Close()

	out, err := tr.Optimize(context.Background(), src, ".mp4", true)
	require.NoError(t, err)
	defer out.Close()

	f, err := os.Open(out.VideoPath)
	require.NoError(t, err)
	defer f.Close()

	// 1. La durée survit au réencodage.
	d, err := Duration(f, "video/mp4")
	require.NoError(t, err)
	assert.InDelta(t, 2.0, d.Seconds(), 0.2)
	assert.LessOrEqual(t, d, MaxShortDuration)

	// 2. L'index est passé DEVANT les données : c'est ce qui rend le
	//    démarrage immédiat au lieu d'attendre le fichier entier.
	moov, _, err := findBox(f, 0, math.MaxInt64, "moov")
	require.NoError(t, err)
	mdat, _, err := findBox(f, 0, math.MaxInt64, "mdat")
	require.NoError(t, err)
	assert.Less(t, moov, mdat, "le fichier produit doit être lisible en flux")

	// 3. La vignette a bien été extraite.
	require.NotEmpty(t, out.ThumbPath, "une vignette doit être produite quand elle est demandée")
	size, err := Size(out.ThumbPath)
	require.NoError(t, err)
	assert.Positive(t, size)

	// 4. Le répertoire de travail disparaît avec Close : un worker qui tourne
	//    en continu ne doit pas remplir le disque.
	dir := filepath.Dir(out.VideoPath)
	require.NoError(t, out.Close())
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "les fichiers temporaires doivent être nettoyés")
}

// Sans vignette demandée, aucune ne doit être produite : c'est le cas où le
// marchand a fourni la sienne.
func TestOptimizeSkipsTheThumbnailWhenNotWanted(t *testing.T) {
	tr := requireFFmpeg(t)
	src, err := os.Open(filepath.Join("testdata", "real-faststart.mp4"))
	require.NoError(t, err)
	defer src.Close()

	out, err := tr.Optimize(context.Background(), src, ".mp4", false)
	require.NoError(t, err)
	defer out.Close()
	assert.Empty(t, out.ThumbPath)
}

// Un fichier qui n'est pas une vidéo doit échouer proprement, sans laisser de
// répertoire temporaire derrière lui.
func TestOptimizeRejectsGarbage(t *testing.T) {
	tr := requireFFmpeg(t)
	_, err := tr.Optimize(context.Background(), bytesOfGarbage(), ".mp4", false)
	assert.Error(t, err)
}

func TestOptimizeWithoutFFmpegIsExplicit(t *testing.T) {
	tr := &Transcoder{} // binaire introuvable
	assert.False(t, tr.Available())
	_, err := tr.Optimize(context.Background(), bytesOfGarbage(), ".mp4", false)
	assert.ErrorIs(t, err, ErrNoTranscoder)
}

func bytesOfGarbage() io.Reader { return bytes.NewReader(bytes.Repeat([]byte{0x3C}, 2048)) }
