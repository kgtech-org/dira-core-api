// Package marker règle LES MARQUEURS DE CARTE qui ne sont pas des véhicules :
// le livreur, le client, le marchand.
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
//
// ⚠️ LES PINS NUMÉROTÉS (v4.18.0). Une carte porte souvent PLUSIEURS points
// du même genre : une commande collectée chez trois marchands, une course qui
// s'arrête deux fois avant d'arriver. Un seul pictogramme pour tous ces
// points ne dit pas dans quel ORDRE on y passe — et c'est justement la seule
// chose qu'on cherche sur la carte. Le client et le marchand portent donc,
// en plus de leur pin PRINCIPAL, jusqu'à quatre pins NUMÉROTÉS.
//
// ⚠️ LE NUMÉRO EST UNE AFFAIRE DE CARTE, pas de liste. Dans une liste, le
// nom de la boutique distingue déjà les points ; c'est sur la carte, où
// aucun nom ne tient, que le chiffre est nécessaire. Les pins numérotés ne
// portent donc QUE `map_icon_url` — un second champ que personne ne
// remplirait n'aurait fait qu'encombrer l'écran de réglage.
//
// ⚠️ `stop` A FUSIONNÉ DANS `client` (v4.18.0). Un arrêt de course et
// l'adresse d'un client sont le même objet vu à deux moments : un point où
// le trajet touche quelqu'un. Les régler séparément obligeait l'exploitation
// à envoyer deux fois la même image pour que la carte reste cohérente — et
// laissait la porte ouverte à ce qu'elle ne le soit pas. Les étapes d'une
// course se dessinent désormais avec les pins numérotés du client.
package marker

import (
	"context"
	"errors"
	"net/http"
	"sort"
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
// livreur ; le chauffeur VTC, lui, est son mode de véhicule.
const (
	KindCourier  = "courier"
	KindClient   = "client"
	KindMerchant = "merchant"
)

var Kinds = []string{KindCourier, KindClient, KindMerchant}

// MaxNumbered borne les pins numérotés.
//
// ⚠️ QUATRE, ET C'EST UN CHOIX PRODUIT. Une course accepte cinq arrêts, donc
// trois intermédiaires ; une commande passe rarement chez plus de trois ou
// quatre marchands. Au-delà, un chiffre dans une pastille de vingt-quatre
// pixels ne se lit plus, et l'application retombe sur le pin PRINCIPAL —
// c'est écrit dans les specs, et c'est mieux qu'un « 7 » illisible.
const MaxNumbered = 4

// Numbered dit si un genre porte des pins numérotés.
//
// Le livreur n'en a pas : il n'y en a qu'un par course. Lui en donner aurait
// été un réglage que personne n'aurait su à quoi rattacher.
func Numbered(kind string) bool { return kind == KindClient || kind == KindMerchant }

// Pin est UN pin numéroté : le rang auquel on passe, et l'image qui le
// dessine.
type Pin struct {
	// Index va de 1 à MaxNumbered. Il n'y a pas de pin « 0 » : le premier
	// point d'une tournée porte le chiffre 1, comme sur l'écran du livreur.
	Index      int    `bson:"index" json:"index"`
	MapIconURL string `bson:"map_icon_url,omitempty" json:"map_icon_url,omitempty"`
}

// Marker : ce qu'un genre porte. Toujours rendu, même jamais réglé — une
// application n'a pas à deviner si un genre existe.
type Marker struct {
	Kind       string `bson:"_id" json:"kind"`
	IconURL    string `bson:"icon_url,omitempty" json:"icon_url,omitempty"`
	MapIconURL string `bson:"map_icon_url,omitempty" json:"map_icon_url,omitempty"`
	// Numbered : les pins numérotés, TOUJOURS servis au complet (1..4) pour
	// les genres qui en portent, même vides.
	//
	// Au complet plutôt qu'en creux : une application qui reçoit une liste
	// trouée devrait deviner si le rang 3 manque parce qu'il n'est pas réglé
	// ou parce qu'il n'existe pas. Absent pour le livreur, qui n'en a pas.
	Numbered []Pin `bson:"numbered,omitempty" json:"numbered,omitempty"`
	// MaxNumbered dit combien il y en a, pour que l'application n'ait pas à
	// coder le nombre en dur. Calculé, jamais stocké.
	MaxNumbered int        `bson:"-" json:"max_numbered,omitempty"`
	UpdatedAt   *time.Time `bson:"updated_at,omitempty" json:"updated_at,omitempty"`
}

// UpdateRequest : les deux images du pin principal, et les pins numérotés.
// Vides pour retirer.
type UpdateRequest struct {
	IconURL    string `json:"icon_url" validate:"omitempty,url,max=2048"`
	MapIconURL string `json:"map_icon_url" validate:"omitempty,url,max=2048"`
	// Numbered : les rangs qu'on règle. Ceux qu'on omet sont EFFACÉS — la
	// requête décrit l'état voulu, comme les deux images au-dessus. Un
	// enregistrement partiel aurait laissé traîner un pin qu'on croyait
	// retiré.
	Numbered []Pin `json:"numbered" validate:"omitempty,max=4,dive"`
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

// List rend les trois genres, réglés ou non, dans l'ordre de `Kinds`.
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
		out = append(out, fill(m))
	}
	return out, nil
}

// fill complète les pins numérotés d'un genre : toujours 1..MaxNumbered,
// dans l'ordre, même vides.
func fill(m Marker) Marker {
	if !Numbered(m.Kind) {
		m.Numbered = nil
		return m
	}
	m.MaxNumbered = MaxNumbered
	byIndex := map[int]string{}
	for _, p := range m.Numbered {
		byIndex[p.Index] = p.MapIconURL
	}
	pins := make([]Pin, 0, MaxNumbered)
	for i := 1; i <= MaxNumbered; i++ {
		pins = append(pins, Pin{Index: i, MapIconURL: byIndex[i]})
	}
	m.Numbered = pins
	return m
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
	before = fill(before)
	before.Kind = kind
	pins, err := cleanPins(kind, req.Numbered)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	m := Marker{Kind: kind, IconURL: req.IconURL, MapIconURL: req.MapIconURL, Numbered: pins, UpdatedAt: &now}
	if _, err := s.col.ReplaceOne(ctx, bson.M{"_id": kind}, m, options.Replace().SetUpsert(true)); err != nil {
		return nil, apperr.Internal(err)
	}
	out := fill(m)
	if s.audit != nil {
		s.audit.Record(ctx, "marker.update", "map_marker", kind, before, out)
	}
	return &out, nil
}

// cleanPins vérifie et range les pins numérotés.
//
// ⚠️ UN RANG HORS BORNES OU EN DOUBLE EST REFUSÉ, jamais ignoré. Un pin
// silencieusement écarté se remarque des semaines plus tard, quand quelqu'un
// s'étonne que la carte n'ait pas changé.
//
// Les pins SANS image sont écartés, eux : c'est ainsi qu'on en retire un, et
// la console envoie toujours les quatre rangs.
func cleanPins(kind string, in []Pin) ([]Pin, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if !Numbered(kind) {
		return nil, apperr.Validation("this marker kind carries no numbered pin")
	}
	seen := map[int]bool{}
	out := make([]Pin, 0, len(in))
	for _, p := range in {
		if p.Index < 1 || p.Index > MaxNumbered {
			return nil, apperr.Validation("numbered pin index must be between 1 and 4")
		}
		if seen[p.Index] {
			return nil, apperr.Validation("duplicate numbered pin index")
		}
		seen[p.Index] = true
		if p.MapIconURL == "" {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
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
