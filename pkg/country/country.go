// Package country porte la COUCHE PAYS de la plateforme : le catalogue des
// pays où Dira peut s'installer, le pays d'une requête, et la façon de le
// trouver depuis une position.
//
// ⚠️ UN PAYS, PAS UNE VILLE. Les villes restent aux verticales (`vtc/area`,
// le registre de dira-maps) : elles bornent le SERVICE — où l'on sert une
// course. Le pays borne les DONNÉES — ce qu'un compte voit. Un chauffeur de
// Cotonou ne doit pas apparaître sur la console de Lomé, et un client
// d'Abidjan ne doit pas voir les promotions togolaises ; c'est vrai même si
// aucune ville n'est encore ouverte à Abidjan.
//
// Le pays d'une requête vient, dans l'ordre : du JETON (le pays du compte,
// inscrit à l'émission — voir `pkg/auth`), de l'en-tête `X-Dira-Country`
// pour qui n'a pas de compte ou a le droit de changer de pays, et du pays
// PAR DÉFAUT du déploiement. Voir `middleware.Country`.
package country

import (
	"context"
	"strings"
)

// Header est l'en-tête par lequel une application dit dans quel pays elle
// opère. Les majuscules d'ISO 3166-1 alpha-2 : `TG`, `BJ`, `CI`.
//
// Pour un compte ordinaire, il ne fait qu'INFORMER : le serveur répond avec
// le pays retenu dans le même en-tête, et l'application s'aligne. Pour un
// compte qui a le droit de changer de pays — la direction, sur la console —
// il COMMANDE.
const Header = "X-Dira-Country"

// QueryParam remplace l'en-tête là où l'on ne peut pas en poser : une
// ouverture de WebSocket depuis un navigateur, un lien.
const QueryParam = "country"

// Info décrit un pays du catalogue.
type Info struct {
	Code        string     `json:"code"`         // ISO 3166-1 alpha-2
	Name        string     `json:"name"`         // en français, la langue de l'exploitation
	Currency    string     `json:"currency"`     // ISO 4217
	PhonePrefix string     `json:"phone_prefix"` // `+228`
	Locale      string     `json:"locale"`       // langue par défaut des messages
	Timezone    string     `json:"timezone"`     // IANA
	Center      [2]float64 `json:"center"`       // [lng, lat] de la capitale économique
}

// Catalog est la liste des pays où Dira PEUT s'installer : l'UEMOA, la
// Guinée, les voisins anglophones, et la zone CFA d'Afrique centrale
// (Cameroun, Tchad, Gabon).
//
// ⚠️ Ce n'est pas la liste des pays où Dira EST installé — celle-là vit en
// base, au socle (`internal/country`), et se règle depuis la console. Le
// catalogue est statique parce qu'il porte des frontières : ouvrir un pays
// qui n'y figure pas demande un déploiement, et c'est normal — il faut aussi
// une monnaie, des villes et des chauffeurs.
var Catalog = []Info{
	{Code: "TG", Name: "Togo", Currency: "XOF", PhonePrefix: "+228", Locale: "fr", Timezone: "Africa/Lome", Center: [2]float64{1.2255, 6.1319}},
	{Code: "BJ", Name: "Bénin", Currency: "XOF", PhonePrefix: "+229", Locale: "fr", Timezone: "Africa/Porto-Novo", Center: [2]float64{2.4183, 6.3654}},
	{Code: "BF", Name: "Burkina Faso", Currency: "XOF", PhonePrefix: "+226", Locale: "fr", Timezone: "Africa/Ouagadougou", Center: [2]float64{-1.5197, 12.3714}},
	{Code: "CI", Name: "Côte d'Ivoire", Currency: "XOF", PhonePrefix: "+225", Locale: "fr", Timezone: "Africa/Abidjan", Center: [2]float64{-4.0083, 5.3600}},
	{Code: "GW", Name: "Guinée-Bissau", Currency: "XOF", PhonePrefix: "+245", Locale: "pt", Timezone: "Africa/Bissau", Center: [2]float64{-15.5983, 11.8636}},
	{Code: "ML", Name: "Mali", Currency: "XOF", PhonePrefix: "+223", Locale: "fr", Timezone: "Africa/Bamako", Center: [2]float64{-8.0029, 12.6392}},
	{Code: "NE", Name: "Niger", Currency: "XOF", PhonePrefix: "+227", Locale: "fr", Timezone: "Africa/Niamey", Center: [2]float64{2.1098, 13.5116}},
	{Code: "SN", Name: "Sénégal", Currency: "XOF", PhonePrefix: "+221", Locale: "fr", Timezone: "Africa/Dakar", Center: [2]float64{-17.4467, 14.6928}},
	{Code: "GN", Name: "Guinée", Currency: "GNF", PhonePrefix: "+224", Locale: "fr", Timezone: "Africa/Conakry", Center: [2]float64{-13.5784, 9.6412}},
	{Code: "GH", Name: "Ghana", Currency: "GHS", PhonePrefix: "+233", Locale: "en", Timezone: "Africa/Accra", Center: [2]float64{-0.1870, 5.6037}},
	{Code: "NG", Name: "Nigeria", Currency: "NGN", PhonePrefix: "+234", Locale: "en", Timezone: "Africa/Lagos", Center: [2]float64{3.3792, 6.5244}},
	{Code: "CM", Name: "Cameroun", Currency: "XAF", PhonePrefix: "+237", Locale: "fr", Timezone: "Africa/Douala", Center: [2]float64{9.7679, 4.0511}},
	{Code: "TD", Name: "Tchad", Currency: "XAF", PhonePrefix: "+235", Locale: "fr", Timezone: "Africa/Ndjamena", Center: [2]float64{15.0600, 12.1100}},
	{Code: "GA", Name: "Gabon", Currency: "XAF", PhonePrefix: "+241", Locale: "fr", Timezone: "Africa/Libreville", Center: [2]float64{9.4500, 0.4100}},
}

// Preloaded sont les pays OUVERTS D'OFFICE au démarrage du socle : ceux où
// Dira se lance. Ouverts s'ils n'ont jamais été enregistrés — fermer l'un
// d'eux depuis la console tient, un redémarrage ne le rouvre pas.
var Preloaded = []string{"TG", "SN", "GN", "TD", "GA"}

// Normalize rend le code en majuscules, ou "" si ce n'est pas un code alpha-2.
//
// Tolérant sur la casse — `tg`, `Tg` —, strict sur la forme : « Togo » ou
// « TGO » ne sont pas devinés. Un en-tête posé par une application se lit
// tel quel ; c'est à elle d'envoyer un code.
func Normalize(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
		return ""
	}
	return c
}

// Lookup rend la fiche d'un pays du catalogue.
func Lookup(code string) (Info, bool) {
	code = Normalize(code)
	for _, c := range Catalog {
		if c.Code == code {
			return c, true
		}
	}
	return Info{}, false
}

// Known dit si le code est au catalogue.
func Known(code string) bool {
	_, ok := Lookup(code)
	return ok
}

// ByPhone déduit le pays d'un numéro E.164 par son indicatif.
//
// C'est le signal de l'INSCRIPTION : une personne qui s'inscrit avec un
// `+229` est béninoise jusqu'à preuve du contraire, et c'est une meilleure
// supposition que le pays par défaut du déploiement. Vide si l'indicatif
// n'est pas au catalogue.
func ByPhone(e164 string) (string, bool) {
	for _, c := range Catalog {
		if strings.HasPrefix(e164, c.PhonePrefix) {
			return c.Code, true
		}
	}
	return "", false
}

// Source dit d'OÙ vient le pays d'une requête.
type Source string

const (
	// SourceClaims : le pays du compte, lu dans le jeton. La vérité pour un
	// compte ordinaire.
	SourceClaims Source = "claims"
	// SourceHeader : l'en-tête `X-Dira-Country`, admis parce que le porteur
	// n'a pas de compte (inscription) ou a le droit de changer de pays.
	SourceHeader Source = "header"
	// SourceDefault : ni jeton ni en-tête utilisable ; le pays du
	// déploiement.
	SourceDefault Source = "default"
)

type ctxKey struct{}

type ctxCountry struct {
	code   string
	source Source
}

// WithCountry pose le pays effectif d'une requête dans le contexte.
func WithCountry(ctx context.Context, code string, source Source) context.Context {
	return context.WithValue(ctx, ctxKey{}, ctxCountry{code: Normalize(code), source: source})
}

// FromContext rend le pays effectif, ou "" si aucun middleware ne l'a posé.
//
// ⚠️ Vide ne veut PAS dire « tous les pays » — il veut dire qu'un service a
// oublié de monter `middleware.Country`. `Restrict` ne filtre alors pas, et
// c'est le seul endroit où cette lecture existe.
func FromContext(ctx context.Context) string {
	c, _ := ctx.Value(ctxKey{}).(ctxCountry)
	return c.code
}

// SourceFromContext dit d'où vient le pays effectif.
func SourceFromContext(ctx context.Context) Source {
	c, _ := ctx.Value(ctxKey{}).(ctxCountry)
	return c.source
}
