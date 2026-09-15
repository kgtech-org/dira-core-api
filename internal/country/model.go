// Package country tient la LISTE DES PAYS OÙ DIRA EST INSTALLÉ, et répond à
// une application qui demande « dans quel pays suis-je ? ».
//
// Le catalogue — les pays possibles, avec leurs frontières — est dans
// `pkg/country`, partagé par tous les services. Ce module ne porte que ce
// qui se DÉCIDE : lequel de ces pays est ouvert, et depuis quand. La
// décision est en base parce qu'elle se prend depuis la console, sans
// déploiement ; le catalogue est dans le code parce qu'ouvrir un pays qui
// n'y est pas demande de toute façon une monnaie, des villes et une flotte.
package country

import (
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Collection is the MongoDB collection of installed countries.
const Collection = "countries"

// Installation dit qu'un pays du catalogue est ouvert — ou l'a été.
//
// ⚠️ Le code est la CLÉ : un pays n'est installé qu'une fois. Fermer un pays
// ne supprime pas la ligne : ses comptes, ses commandes et ses courses
// portent encore son code, et la console doit pouvoir les relire.
type Installation struct {
	Code      string    `bson:"_id"`
	Enabled   bool      `bson:"enabled"`
	EnabledAt time.Time `bson:"enabled_at,omitempty"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Response est un pays tel que le public et la console le lisent : la fiche
// du catalogue, et l'état de son installation.
type Response struct {
	country.Info
	Enabled bool `json:"enabled"`
	// Default marque le pays par défaut du déploiement — celui qu'une
	// requête sans jeton ni en-tête reçoit.
	Default bool `json:"default,omitempty"`
}

// SetEnabledRequest ouvre ou ferme un pays.
type SetEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// ResolveRequest est ce qu'une application envoie pour connaître son pays.
//
// Les deux coordonnées ensemble, ou aucune : sans position, la résolution
// retombe sur l'adresse IP de la requête.
type ResolveRequest struct {
	Lng *float64 `json:"lng" validate:"omitempty,min=-180,max=180"`
	Lat *float64 `json:"lat" validate:"omitempty,min=-90,max=90"`
}

// Sources d'une résolution, du plus sûr au moins sûr.
const (
	// ResolvedByGeo : la position est dans un pays installé.
	ResolvedByGeo = "geo"
	// ResolvedByIP : la position manquait ou n'était dans aucun pays installé
	// ; l'adresse IP en désigne un.
	ResolvedByIP = "ip"
	// ResolvedByAccount : rien de ce qui précède ; le pays du compte reste.
	ResolvedByAccount = "account"
	// ResolvedByDefault : le compte n'avait pas de pays non plus ; celui du
	// déploiement.
	ResolvedByDefault = "default"
)

// ResolveResponse dit dans quel pays l'application doit opérer, et pourquoi.
type ResolveResponse struct {
	// Country est le pays RETENU — celui que l'application doit envoyer dans
	// `X-Dira-Country` et que le compte porte désormais.
	Country string `json:"country"`
	Source  string `json:"source"`
	// Detected est le pays où la position ou l'adresse IP situe la personne,
	// même s'il n'est pas installé — pour que l'application puisse dire « Dira
	// n'est pas encore au Ghana » plutôt que rien.
	Detected string `json:"detected,omitempty"`
	// Supported dit si `Detected` est un pays installé. Faux = la personne
	// est hors zone, et `Country` est un repli.
	Supported bool `json:"supported"`
	// Updated dit si le pays du compte a changé à cette occasion.
	Updated bool `json:"updated"`
}

var (
	errUnknownCountry = apperr.NotFound("country_unknown", "this country is not in the catalog")
	errDefaultCountry = apperr.Conflict("country_default", "the default country cannot be disabled")
	errNoCoordinates  = apperr.Validation("lng and lat go together: send both or neither")
)
