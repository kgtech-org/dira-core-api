// Package upload exposes the authenticated media upload endpoint. Files land
// in object storage; the caller persists the returned public URL on the
// relevant document (avatar, vehicle photo, compliance document, dish images,
// store image, brand logo, feed video, banner).
//
// AU SOCLE, et pas dans une verticale. Il y a vécu par accident d'histoire :
// écrit dans le monolithe, resté dans la livraison à la scission parce que
// ses appelants l'y trouvaient. Mais un avatar est un objet du profil, une
// photo de véhicule ou un permis servent aux deux métiers — et les courses
// n'avaient AUCUNE porte d'envoi : un chauffeur VTC photographiait sa berline
// via `/food/uploads`. Une seule règle de poids et de type, un seul bucket,
// une seule configuration à tenir.
package upload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/media"
	"github.com/kgtech-org/dira-core-api/pkg/storage"
)

// objectStore is the storage contract consumed by the handler. L'interface
// existe pour que la règle des shorts (durée, type, poids) soit vérifiable
// sans MinIO : c'est elle qui protège le format, pas le stockage.
type objectStore interface {
	Put(ctx context.Context, kind, entity, contentType string, size int64, r io.Reader) (string, error)
}

type Handler struct {
	store objectStore
}

// NewHandler builds the upload handler. A nil store keeps the endpoint
// mounted but answering 503.
func NewHandler(store *storage.Store) *Handler {
	if store == nil {
		return &Handler{} // évite une interface non-nil enveloppant un pointeur nil
	}
	return &Handler{store: store}
}

// Mount registers POST /uploads (any authenticated role).
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		g.Post("/uploads", h.upload)
	})
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.Error(w, r, apperr.New("storage_unavailable", "object storage is not configured", http.StatusServiceUnavailable))
		return
	}
	kind := r.URL.Query().Get("kind")
	if !storage.Kinds[kind] {
		httpx.Error(w, r, apperr.Validation("kind must be one of dish|store|brand|vehicle|avatar|feed|banner"))
		return
	}
	// Le plafond dépend du kind : une vidéo de feed pèse bien plus qu'une
	// photo de plat, mais rien d'autre ne doit profiter de ce plafond.
	maxBytes := storage.MaxBytesFor(kind)
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		httpx.Error(w, r, apperr.Validation("file too large or invalid multipart body").WithCause(err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, r, apperr.Validation("multipart field 'file' is required").WithCause(err))
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if _, ok := storage.Accepts(kind, contentType); !ok {
		if kind == "feed" {
			httpx.Error(w, r, apperr.Validation("unsupported type (images: jpeg, png, webp, svg — videos: mp4, webm, mov)"))
			return
		}
		httpx.Error(w, r, apperr.Validation("unsupported image type (jpeg, png, webp, svg)"))
		return
	}
	// Une vidéo de feed est un SHORT : la durée est lue dans le conteneur, pas
	// déclarée par l'appelant. Le contrôle du navigateur sert le confort de
	// l'utilisateur ; celui-ci sert la règle.
	var duration time.Duration
	if _, isVideo := storage.VideoTypes[contentType]; isVideo {
		var tooLong media.ErrTooLong
		d, err := media.CheckShort(file, contentType)
		switch {
		case errors.As(err, &tooLong):
			httpx.Error(w, r, apperr.New("video_too_long",
				fmt.Sprintf("la vidéo dure %.0f s, la limite est de %.0f s",
					d.Seconds(), media.MaxShortDuration.Seconds()),
				http.StatusUnprocessableEntity))
			return
		case err != nil:
			// Refuser plutôt qu'accepter à l'aveugle : un conteneur dont on ne
			// sait pas lire la durée n'est pas contrôlable, et le transcodage
			// échouerait de toute façon plus tard, après coup et sans message.
			httpx.Error(w, r, apperr.New("video_unreadable",
				"vidéo illisible : réexportez-la en MP4 (H.264) et réessayez",
				http.StatusUnprocessableEntity).WithCause(err))
			return
		}
		duration = d
	}

	url, err := h.store.Put(r.Context(), kind, r.URL.Query().Get("entity"), contentType, header.Size, file)
	if err != nil {
		httpx.Error(w, r, apperr.Internal(err))
		return
	}
	// La durée revient à l'appelant : la console l'affiche et la repasse au
	// feed sans avoir à re-sonder le fichier.
	body := map[string]any{"url": url}
	if duration > 0 {
		body["duration_seconds"] = int(duration.Round(time.Second).Seconds())
	}
	httpx.JSON(w, http.StatusCreated, body)
}
