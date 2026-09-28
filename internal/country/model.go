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
	// BasemapGoogle exige une clé configurée pour la plateforme qui demande.
	BasemapGoogle = "google"
)

// Les plateformes qui portent chacune SA clé.
//
// ⚠️ UNE CLÉ PAR PLATEFORME, ET CE N'EST PAS UN CAPRICE. Google restreint une
// clé par ce qui l'utilise : empreinte SHA-1 plus nom de paquet sur Android,
// identifiant de bundle sur iOS, référent HTTP sur le web. Une clé unique pour
// les trois ne peut être restreinte à AUCUN des trois — c'est-à-dire qu'elle
// reste utilisable par n'importe qui l'aura lue, et c'est justement la seule
// protection qui vaille ici.
const (
	PlatformWeb     = "web"
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
)

// Les vues que Google sait rendre.
const (
	MapTypeRoadmap   = "roadmap"
	MapTypeSatellite = "satellite"
	MapTypeTerrain   = "terrain"
)

// GoogleMaps est la clé d'une plateforme, et ce qu'elle affiche.
type GoogleMaps struct {
	// Key est la clé Google Maps. ⚠️ ELLE NE RESSORT JAMAIS PAR LA CONSOLE :
	// on la pose, on ne la relit pas. La console n'en voit que les quatre
	// derniers caractères, de quoi reconnaître laquelle est en place sans
	// pouvoir l'emporter. Une clé qu'un écran d'administration réaffiche est
	// une clé qui finit dans une capture d'écran, un ticket, un canal de
	// discussion.
	Key       string    `bson:"key"`
	MapType   string    `bson:"map_type,omitempty"`
	UpdatedAt time.Time `bson:"updated_at,omitempty"`
}

// Maps porte les réglages de carte d'un pays.
type Maps struct {
	// Basemap est le fond par DÉFAUT du pays — et seulement le défaut : la
	// personne choisit ensuite celui qui lui va, et son choix vit chez elle.
	// Vide = `dira`.
	Basemap string `bson:"basemap,omitempty"`
	// Google : une clé par plateforme, absente quand il n'y en a pas.
	Google map[string]GoogleMaps `bson:"google,omitempty"`
}

// GoogleUpdate pose ou retire la clé d'une plateforme.
//
// Les deux champs sont des POINTEURS : absent = ne pas y toucher. C'est la même
// convention que `UpdateRequest` — on règle le type de carte sans avoir à
// renvoyer la clé, ce qui obligerait à la connaître pour changer autre chose.
type GoogleUpdate struct {
	// Key vide (`""`) RETIRE la clé. ⚠️ Il faut un moyen explicite de retirer :
	// sans lui, une clé compromise ne pourrait qu'être remplacée, jamais
	// enlevée — et le pays resterait sur un fond Google qu'aucune clé ne sert.
	Key     *string `json:"key" validate:"omitempty,max=200"`
	MapType *string `json:"map_type" validate:"omitempty,oneof=roadmap satellite terrain"`
}

// MapsUpdateRequest règle le fond d'un pays depuis la console.
type MapsUpdateRequest struct {
	Basemap *string                 `json:"basemap" validate:"omitempty,oneof=dira google"`
	Google  map[string]GoogleUpdate `json:"google"`
}

// GoogleKeyStatus est ce que la console voit d'une clé : qu'elle existe, de
// quoi la reconnaître, et depuis quand.
type GoogleKeyStatus struct {
	Configured bool `json:"configured"`
	// Hint est la fin de la clé (quatre caractères). Assez pour dire « c'est
	// bien celle du 12 mars », pas assez pour s'en servir.
	Hint    string `json:"hint,omitempty"`
	MapType string `json:"map_type,omitempty"`
	// ⚠️ UN POINTEUR, PAS UNE DATE. `omitempty` ne sait pas taire un `time.Time`
	// vide : une plateforme sans clé rendait `"0001-01-01T00:00:00Z"`, une date
	// que la console afficherait telle quelle. Un champ absent se lit ; une date
	// de l'an 1 se lit aussi, et elle ment.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// MapsResponse est le réglage du pays tel que la console le lit.
type MapsResponse struct {
	Basemap string                     `json:"basemap"`
	Google  map[string]GoogleKeyStatus `json:"google"`
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
	errDefaultCountry  = apperr.Conflict("country_default", "the default country cannot be disabled")
	errNoCoordinates   = apperr.Validation("lng and lat go together: send both or neither")
	errNoMapsUpdate    = apperr.Validation("nothing to update: send basemap and/or google")
	errUnknownPlatform = apperr.Validation("unknown platform: expected web, android or ios")
	// ⚠️ On refuse de mettre un pays sur `google` sans clé : sinon la bascule
	// est « réussie » et toutes les cartes du pays deviennent grises, sans que
	// rien nulle part ne dise pourquoi.
	errGoogleWithoutKey = apperr.Conflict("maps_google_without_key",
		"configure at least one platform key before switching this country to the Google basemap")
)
