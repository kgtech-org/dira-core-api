package upload

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore records what actually reached storage: la question la plus
// importante d'un contrôle d'entrée est « qu'est-ce qui est passé ? ».
type fakeStore struct {
	puts int
	body []byte
}

func (f *fakeStore) Put(_ context.Context, _, _, _ string, _ int64, r io.Reader) (string, error) {
	f.puts++
	b, err := io.ReadAll(r)
	f.body = b
	return "https://cdn.example.com/feed/x.mp4", err
}

// --- fabrication d'un mp4 minimal de durée choisie -------------------------

func box(name string, payload []byte) []byte {
	b := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(b[0:4], uint32(8+len(payload)))
	copy(b[4:8], name)
	return append(b, payload...)
}

func mp4OfSeconds(seconds uint32) []byte {
	mvhd := make([]byte, 4+16)
	binary.BigEndian.PutUint32(mvhd[12:16], 1000)         // timescale
	binary.BigEndian.PutUint32(mvhd[16:20], seconds*1000) // duration
	var f []byte
	f = append(f, box("ftyp", []byte("isomiso2mp41"))...)
	f = append(f, box("moov", box("mvhd", mvhd))...)
	f = append(f, box("mdat", bytes.Repeat([]byte{0x22}, 256))...)
	return f
}

func upload(t *testing.T, h *Handler, kind, filename, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{`form-data; name="file"; filename="` + filename + `"`}
	hdr["Content-Type"] = []string{contentType}
	part, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, err = part.Write(body)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/uploads?kind="+kind, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.upload(rec, req)
	return rec
}

func TestUploadAcceptsAShort(t *testing.T) {
	store := &fakeStore{}
	rec := upload(t, &Handler{store: store}, "feed", "short.mp4", "video/mp4", mp4OfSeconds(42))

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotEmpty(t, body["url"])
	assert.EqualValues(t, 42, body["duration_seconds"], "la durée lue est rendue à la console")
	assert.Equal(t, 1, store.puts)
	// Le fichier doit arriver ENTIER : la sonde le lit avant le stockage, et
	// un lecteur mal rembobiné tronquerait silencieusement la vidéo.
	assert.Equal(t, mp4OfSeconds(42), store.body)
}

// La limite du format : elle est ici, pas dans le navigateur.
func TestUploadRejectsVideoLongerThanAShort(t *testing.T) {
	store := &fakeStore{}
	rec := upload(t, &Handler{store: store}, "feed", "long.mp4", "video/mp4", mp4OfSeconds(61))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "video_too_long")
	assert.Zero(t, store.puts, "une vidéo trop longue ne doit jamais atteindre le stockage")
}

func TestUploadAcceptsExactlySixtySeconds(t *testing.T) {
	store := &fakeStore{}
	rec := upload(t, &Handler{store: store}, "feed", "limit.mp4", "video/mp4", mp4OfSeconds(60))
	assert.Equal(t, http.StatusCreated, rec.Code, "60 s est dans le format, pas au-delà")
	assert.Equal(t, 1, store.puts)
}

// Un conteneur dont la durée est illisible n'est pas contrôlable : le refuser
// tout de suite vaut mieux qu'un transcodage qui échouera plus tard, sans que
// personne ne le voie.
func TestUploadRejectsAnUnreadableContainer(t *testing.T) {
	store := &fakeStore{}
	rec := upload(t, &Handler{store: store}, "feed", "broken.mp4", "video/mp4", bytes.Repeat([]byte{0x5A}, 4096))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "video_unreadable")
	assert.Zero(t, store.puts)
}

// Le contrôle de durée ne concerne que les vidéos : une image ne doit pas être
// prise dans ce filet.
func TestUploadLeavesImagesAlone(t *testing.T) {
	store := &fakeStore{}
	rec := upload(t, &Handler{store: store}, "dish", "plat.jpg", "image/jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00})

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotContains(t, body, "duration_seconds")
	assert.Equal(t, 1, store.puts)
}

// Une vidéo ne peut pas se déguiser en image de plat pour contourner la règle.
func TestUploadRejectsVideoOutsideTheFeed(t *testing.T) {
	store := &fakeStore{}
	rec := upload(t, &Handler{store: store}, "dish", "short.mp4", "video/mp4", mp4OfSeconds(10))

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Zero(t, store.puts)
}

func TestUploadWithoutStorageAnswers503(t *testing.T) {
	rec := upload(t, NewHandler(nil), "feed", "short.mp4", "video/mp4", mp4OfSeconds(10))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
