package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNoTranscoder signals that ffmpeg is absent from the host. L'appelant doit
// dégrader, pas échouer : une vidéo non optimisée reste lisible.
var ErrNoTranscoder = errors.New("media: ffmpeg is not available")

// Profil d'encodage des shorts.
//
// ⚠️ Réglages produit/technique : ils arbitrent poids contre qualité sur des
// réseaux mobiles ouest-africains. Les valeurs visent ~1,5 Mo par tranche de
// 10 s, soit une vidéo de 60 s sous les 10 Mo.
const (
	// maxEdge bounds the long side. Un short se regarde en plein écran sur un
	// téléphone : au-delà de 1280 on transporte des pixels invisibles.
	maxEdge = 1280
	// maxShortEdge bounds the short side (portrait 720×1280).
	maxShortEdge = 720
	// crf : 26 est le point où l'artefact devient invisible sur un écran de
	// téléphone tout en divisant le poids d'un export brut par 3 à 5.
	crf = "26"
	// gopSeconds : un keyframe toutes les 2 s. C'est ce qui rend le
	// déplacement dans la vidéo immédiat — sans cela le lecteur doit remonter
	// au keyframe précédent, parfois plusieurs secondes en arrière.
	gopSeconds = 2
	fps        = 30
	// transcodeTimeout borne un encodage : au-delà, la tâche est reprise par
	// Asynq plutôt que d'immobiliser un worker.
	transcodeTimeout = 3 * time.Minute
)

// Transcoder re-encodes uploaded shorts with ffmpeg.
type Transcoder struct {
	bin     string
	timeout time.Duration
}

// NewTranscoder locates ffmpeg (FFMPEG_BIN, then PATH). An absent binary is
// not an error here: Available() reports it and the caller degrades.
func NewTranscoder() *Transcoder {
	bin := os.Getenv("FFMPEG_BIN")
	if bin == "" {
		bin = "ffmpeg"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return &Transcoder{timeout: transcodeTimeout}
	}
	return &Transcoder{bin: resolved, timeout: transcodeTimeout}
}

// Available reports whether ffmpeg was found.
func (t *Transcoder) Available() bool { return t != nil && t.bin != "" }

// Optimized holds the products of a transcode. Close removes the temp files.
type Optimized struct {
	VideoPath string
	// ThumbPath is empty when no thumbnail was requested.
	ThumbPath string
	dir       string
}

// Close removes the working directory.
func (o *Optimized) Close() error {
	if o == nil || o.dir == "" {
		return nil
	}
	return os.RemoveAll(o.dir)
}

// Size returns the byte size of a produced file.
func Size(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// Optimize re-encodes src into a streamable MP4 and, when withThumbnail is
// set, extracts its first frame as a JPEG.
//
// Le flux source est écrit sur disque avant l'appel : ffmpeg doit pouvoir se
// déplacer dans l'entrée (un mp4 dont le `moov` est en fin de fichier est
// illisible en flux séquentiel — c'est précisément le défaut qu'on corrige).
func (t *Transcoder) Optimize(ctx context.Context, src io.Reader, ext string, withThumbnail bool) (*Optimized, error) {
	if !t.Available() {
		return nil, ErrNoTranscoder
	}
	dir, err := os.MkdirTemp("", "dira-short-*")
	if err != nil {
		return nil, err
	}
	out := &Optimized{dir: dir}

	if ext == "" {
		ext = ".mp4"
	}
	srcPath := filepath.Join(dir, "source"+ext)
	if err := writeFile(srcPath, src); err != nil {
		_ = out.Close()
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	videoPath := filepath.Join(dir, "short.mp4")
	if err := t.run(ctx, encodeArgs(srcPath, videoPath)); err != nil {
		_ = out.Close()
		return nil, err
	}
	out.VideoPath = videoPath

	if withThumbnail {
		thumbPath := filepath.Join(dir, "thumb.jpg")
		// Une vignette manquante ne doit pas perdre la vidéo réencodée :
		// l'échec est signalé par l'absence de chemin, pas par une erreur.
		if err := t.run(ctx, thumbnailArgs(videoPath, thumbPath)); err == nil {
			out.ThumbPath = thumbPath
		}
	}
	return out, nil
}

// encodeArgs builds the ffmpeg command line for a short.
func encodeArgs(src, dst string) []string {
	// Réduction dans la boîte 720×1280 en conservant le rapport, puis
	// arrondi à des dimensions paires (exigence de yuv420p).
	scale := fmt.Sprintf(
		"scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease,"+
			"scale=trunc(iw/2)*2:trunc(ih/2)*2,fps=%d",
		maxShortEdge, maxEdge, fps)
	gop := fmt.Sprintf("%d", gopSeconds*fps)
	return []string{
		"-nostdin", "-y", "-loglevel", "error",
		"-i", src,
		// Coupe dure à la limite du format : un fichier plus long ne peut pas
		// se glisser dans le feed par un conteneur mal formé.
		"-t", fmt.Sprintf("%.0f", MaxShortDuration.Seconds()),
		"-vf", scale,
		"-c:v", "libx264", "-profile:v", "high", "-preset", "veryfast",
		"-crf", crf, "-maxrate", "2500k", "-bufsize", "5000k",
		"-pix_fmt", "yuv420p",
		"-g", gop, "-keyint_min", gop, "-sc_threshold", "0",
		"-c:a", "aac", "-b:a", "96k", "-ac", "2", "-ar", "44100",
		// L'index en TÊTE de fichier : le lecteur démarre sur les premiers
		// octets au lieu d'attendre le téléchargement complet.
		"-movflags", "+faststart",
		dst,
	}
}

// thumbnailArgs extracts the first frame as a JPEG.
func thumbnailArgs(src, dst string) []string {
	return []string{
		"-nostdin", "-y", "-loglevel", "error",
		"-i", src, "-frames:v", "1", "-q:v", "4", dst,
	}
}

func (t *Transcoder) run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, t.bin, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return fmt.Errorf("media: ffmpeg: %w: %s", err, msg)
	}
	return nil
}

func writeFile(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	return f.Sync()
}
