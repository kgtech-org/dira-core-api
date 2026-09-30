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
	Code    string `bson:"_id"`
	Enabled bool   `bson:"enabled"`
	// Currency est la MONNAIE du pays telle que l'exploitation l'a réglée
	// (ISO 4217). Vide = celle du catalogue. Réglable parce qu'un pays peut
	// changer de monnaie — l'« eco » annoncée pour l'UEMOA — sans qu'on
	// redéploie ; et parce que c'est un réglage d'exploitation, pas une
	// vérité géographique.
	Currency string `bson:"currency,omitempty"`
	// Testing marque un pays RÉSERVÉ AUX ESSAIS.
	//
	// ⚠️ SANS CETTE MARQUE, UN PAYS D'ESSAI EMPOISONNE TOUT CE QUI COMPTE. Les
	// jauges de la supervision comptent TOUS les pays — délibérément, parce
	// qu'une panne n'appartient à aucun —, et les rapports périodiques aussi.
	// Un test de charge qui crée cinq cents courses ferait donc monter le
	// tableau « est-ce que ça va ? » et partir un rapport annonçant une
	// journée record. On ne s'en apercevrait qu'en cherchant pourquoi les
	// chiffres ne collent pas avec la caisse.
	//
	// ⚠️ UNE MARQUE, PAS UN CODE EN DUR. « Si le pays vaut GA » écrit à six
	// endroits pourrit : le septième est oublié, et le jour où l'essai change
	// de pays il faut les retrouver tous. La marque se lit, se règle depuis la
	// console, et voyage jusqu'aux verticales par `/internal/countries`.
	Testing   bool      `bson:"testing,omitempty"`
	EnabledAt time.Time `bson:"enabled_at,omitempty"`
	UpdatedAt time.Time `bson:"updated_at"`
	// Maps est le FOND DE CARTE du pays : celui qu'on sert par défaut, et la
	// clé Google de chaque plateforme quand l'exploitation en a configuré une.
	Maps Maps `bson:"maps,omitempty"`
}

// Les deux fonds de carte possibles.
const (
	// BasemapDira est notre fond — celui qui ne coûte rien et ne dépend de
	// personne.
	BasemapDira = "dira"
	// BasemapGoogle : le fond de Google. ⚠️ C'est l'APPLICATION qui porte la
	// clé, restreinte à son empreinte — le serveur dit seulement que c'est le
	// fond à montrer d'abord dans ce pays.
	BasemapGoogle = "google"
)

// Maps porte le réglage de carte d'un pays : UN SEUL FAIT.
//
// ⚠️ LE SERVEUR NE GARDE PLUS AUCUNE CLÉ GOOGLE (v4.40.0). Il en a gardé une
// par pays et par plateforme pendant une semaine, pour pouvoir la faire tourner
// sans republier les applications. C'était incompatible avec la façon dont les
// frontends sont faits : chacun porte SA clé, restreinte à son empreinte, à son
// bundle ou à son référent — une clé servie par l'API arrivait trop tard, pour
// une plateforme qui en avait déjà une. Le serveur ne répond donc plus qu'à une
// question : « dans ce pays, quel fond montre-t-on d'ABORD ? »
type Maps struct {
	// Basemap est le fond par DÉFAUT du pays — et seulement le défaut : la
	// personne choisit ensuite celui qui lui va, et son choix vit chez elle.
	// Vide = `dira`.
	Basemap string `bson:"basemap,omitempty"`
}

// MapsUpdateRequest règle le fond d'un pays depuis la console. Un champ, un
// choix : `dira` ou `google`.
type MapsUpdateRequest struct {
	Basemap *string `json:"basemap" validate:"omitempty,oneof=dira google"`
}

// MapsResponse est le réglage du pays tel que la console le lit — et tel que
// les applications le reçoivent : le fond par défaut, rien d'autre.
type MapsResponse struct {
	Basemap string `json:"basemap"`
}

// Response est un pays tel que le public et la console le lisent : la fiche
// du catalogue, sa monnaie EFFECTIVE, et l'état de son installation.
type Response struct {
	country.Info
	// CurrencyName, CurrencySymbol, CurrencyDecimals décrivent la monnaie en
	// vigueur (`currency` de la fiche, remplacé par le réglage s'il y en a
	// un) — ce qu'une application a besoin de savoir pour AFFICHER un
	// montant : « 2 500 F CFA », « 35 000 FG ».
	CurrencyName     string `json:"currency_name"`
	CurrencySymbol   string `json:"currency_symbol"`
	CurrencyDecimals int    `json:"currency_decimals"`
	Enabled          bool   `json:"enabled"`
	// Default marque le pays par défaut du déploiement — celui qu'une
	// requête sans jeton ni en-tête reçoit.
	Default bool `json:"default,omitempty"`
	// Testing dit que ce pays est réservé aux ESSAIS : ses chiffres ne comptent
	// ni dans la supervision, ni dans les rapports. La console doit le montrer,
	// sans quoi on prendra ses courses pour de vraies.
	Testing bool `json:"testing,omitempty"`
	// Basemap est le FOND DE CARTE par défaut du pays — `dira` ou `google`.
	//
	// ⚠️ ICI PARCE QUE `maps` DE L'AUTHENTIFICATION NE SUFFIT PAS. Ce bloc-là
	// voyage avec le jeton : il dit le fond du pays où l'on s'est CONNECTÉ, une
	// fois. Or une application change de pays sans se reconnecter — un passager
	// togolais qui ouvre l'application à Dakar, une console dont la direction
	// bascule d'un clic — et elle relit ce catalogue à chaque ouverture. Sans ce
	// champ, elle garderait le fond de la veille, dans le mauvais pays.
	Basemap string `json:"basemap"`
}

// UpdateRequest règle un pays : ouvert ou fermé, et sa monnaie. Les deux
// facultatifs — on change l'un sans toucher à l'autre.
type UpdateRequest struct {
	Enabled  *bool   `json:"enabled"`
	Currency *string `json:"currency" validate:"omitempty,len=3"`
	// Testing réserve ce pays aux essais, ou l'en sort.
	Testing *bool `json:"testing"`
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
	errUnknownCountry  = apperr.NotFound("country_unknown", "this country is not in the catalog")
	errUnknownCurrency = apperr.Validation("unknown currency: expected an ISO 4217 code such as XOF or GNF")
	errNothingToUpdate = apperr.Validation("nothing to update: send enabled, currency and/or testing")
	// ⚠️ Le pays PAR DÉFAUT ne peut pas être un pays d'essai : c'est celui
	// qu'une requête sans en-tête reçoit, donc celui où atterrit le trafic
	// réel. L'y marquer ferait disparaître la moitié de la plateforme des
	// tableaux de bord, en silence.
	errDefaultTesting = apperr.Conflict("country_default_testing",
		"the default country cannot be reserved for testing")
	errDefaultCountry = apperr.Conflict("country_default", "the default country cannot be disabled")
	errNoCoordinates  = apperr.Validation("lng and lat go together: send both or neither")
	errNoMapsUpdate   = apperr.Validation("nothing to update: send basemap")
)
