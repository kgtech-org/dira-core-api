// Package marker règle LES MARQUEURS DE CARTE qui ne sont pas des véhicules :
// le livreur, le client, le marchand, l'arrêt.
//
// Les modes de véhicule portent déjà leurs deux images (l'icône d'affichage
// et l'icône de carte, réglées depuis la console — dira-vtc-api, classes).
// Les autres acteurs d'une carte n'avaient rien : chaque application et la
// console dessinaient leur propre pictogramme, et l'exploitation ne pouvait
// pas donner à la plateforme un client, une boutique ou un arrêt à elle.
//
// Un marqueur par GENRE, pour toute la plateforme (pas par pays : un client
// se dessine pareil à Lomé et à Dakar). Chacun porte deux images facultatives,
// comme un mode : `icon_url` (à côté d'un nom, dans une liste) et
// `map_icon_url` (dans la pastille du marqueur). Vides, l'application garde
// son pictogramme — le réglage ajoute, il ne retire rien.
package marker

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/db"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

const Collection = "map_markers"

// Les GENRES, dans l'ordre où la console les montre. `courier` est le
// livreur (le chauffeur VTC est son mode de véhicule) ; `stop` est un arrêt
// de course — le départ, une étape — quand il n'est ni le client ni un
// marchand.
const (
	KindCourier  = "courier"
	KindClient   = "client"
	KindMerchant = "merchant"
	KindStop     = "stop"
)

var Kinds = []string{KindCourier, KindClient, KindMerchant, KindStop}

// Marker : ce qu'un genre porte. Toujours rendu, même jamais réglé — une
// application n'a pas à deviner si un genre existe.
type Marker struct {
	Kind       string     `bson:"_id" json:"kind"`
	IconURL    string     `bson:"icon_url,omitempty" json:"icon_url,omitempty"`
	MapIconURL string     `bson:"map_icon_url,omitempty" json:"map_icon_url,omitempty"`
	UpdatedAt  *time.Time `bson:"updated_at,omitempty" json:"updated_at,omitempty"`
}

// UpdateRequest : les deux images, vides pour retirer.
type UpdateRequest struct {
	IconURL    string `json:"icon_url" validate:"omitempty,url,max=2048"`
	MapIconURL string `json:"map_icon_url" validate:"omitempty,url,max=2048"`
}

// Auditor journalise un réglage de marqueur.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

type Service struct {
	col   *mongo.Collection
	audit Auditor
}

func NewService(m *db.Mongo, audit Auditor) *Service {
	return &Service{col: m.Collection(Collection), audit: audit}
}

func validKind(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// List rend les quatre genres, réglés ou non, dans l'ordre de `Kinds`.
func (s *Service) List(ctx context.Context) ([]Marker, error) {
	cur, err := s.col.Find(ctx, bson.M{})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	var stored []Marker
	if err := cur.All(ctx, &stored); err != nil {
		return nil, apperr.Internal(err)
	}
	byKind := map[string]Marker{}
	for _, m := range stored {
		byKind[m.Kind] = m
	}
	out := make([]Marker, 0, len(Kinds))
	for _, k := range Kinds {
		m, ok := byKind[k]
		if !ok {
			m = Marker{Kind: k}
		}
		out = append(out, m)
	}
	return out, nil
}

// Update pose (ou retire) les images d'un genre.
func (s *Service) Update(ctx context.Context, kind string, req UpdateRequest) (*Marker, error) {
	if !validKind(kind) {
		return nil, apperr.NotFound("marker_not_found", "unknown marker kind: "+kind)
	}
	var before Marker
	if err := s.col.FindOne(ctx, bson.M{"_id": kind}).Decode(&before); err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.Internal(err)
	}
	before.Kind = kind
	now := time.Now().UTC()
	m := Marker{Kind: kind, IconURL: req.IconURL, MapIconURL: req.MapIconURL, UpdatedAt: &now}
	if _, err := s.col.ReplaceOne(ctx, bson.M{"_id": kind}, m, options.Replace().SetUpsert(true)); err != nil {
		return nil, apperr.Internal(err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, "marker.update", "map_marker", kind, before, m)
	}
	return &m, nil
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount : lecture PUBLIQUE (les applications dessinent des cartes avant
// même la connexion — choisir une adresse, voir les boutiques autour) ;
// réglage par l'exploitation du socle.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Get("/map-markers", h.list)
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RoleAdmin), middleware.RequireScope(auth.ScopeCore))
		g.Put("/admin/map-markers/{kind}", h.update)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req UpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Update(r.Context(), chi.URLParam(r, "kind"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
