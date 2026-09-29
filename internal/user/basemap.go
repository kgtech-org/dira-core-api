package user

// LE FOND DE CARTE SERVI À L'AUTHENTIFICATION.
//
// ⚠️ POURQUOI ICI, ET PAS SUR UNE ROUTE À PART. Une application a besoin du
// fond de carte à la seconde où elle ouvre son premier écran ; un appel dédié
// aurait ajouté un aller-retour avant la première carte, et un état de plus à
// gérer — « je ne sais pas encore quoi montrer ». Il voyage donc avec le jeton,
// qui arrive de toute façon avant tout le reste.
//
// ⚠️ ET AUCUNE CLÉ N'Y VOYAGE (v4.40.0) : la clé Google est celle de
// l'application, restreinte à son empreinte. Le serveur ne dit QUE le fond à
// montrer d'abord.
//
// ⚠️ ET AUSSI AU RAFRAÎCHISSEMENT. Servi seulement à la connexion, un défaut
// changé dans la console n'atteindrait un chauffeur resté connecté que trente
// jours plus tard. Au rafraîchissement, la dérive se borne à la durée d'un
// jeton d'accès.

import (
	"context"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Basemaps rend le fond de carte d'un pays pour une plateforme.
//
// FACULTATIF, et il doit le rester : un module de pays non branché, une base
// injoignable ou un pays sans réglage ne doivent jamais empêcher quelqu'un de se
// connecter. Sans lui, l'application garde notre fond — ce qui marche.
type Basemaps interface {
	Grant(ctx context.Context, countryCode string) string
}

// SetBasemaps branche le service de pays.
func (s *Service) SetBasemaps(b Basemaps) { s.basemaps = b }

// basemap rend ce que l'application doit afficher, ou `nil` quand il n'y a rien
// à dire — plateforme muette, module absent.
//
// ⚠️ LE PAYS VIENT DE LA REQUÊTE, PAS DU COMPTE, et dans cet ordre. Un passager
// togolais qui ouvre l'application à Dakar doit voir le fond réglé pour le
// Sénégal : c'est là qu'il est. Le pays du compte ne sert que de repli, quand
// aucun en-tête n'a été posé.
//
// ⚠️ `platform` N'ENTRE PLUS EN JEU (v4.40.0) : le réglage était par plateforme
// tant qu'il portait une clé — une clé Android ne vaut pas sur le web. Un
// DÉFAUT de pays, lui, est le même pour tout le monde ; le garder par
// plateforme n'aurait laissé qu'un piège, celui d'un réglage qui a l'air posé
// et ne sort que sur deux applications sur trois.
func (s *Service) basemap(ctx context.Context, u *User) *MapsResponse {
	if s.basemaps == nil {
		return nil
	}
	code := country.FromContext(ctx)
	if code == "" && u != nil {
		code = u.Country
	}
	if code == "" {
		return nil
	}
	base := s.basemaps.Grant(ctx, code)
	if base == "" {
		return nil
	}
	return &MapsResponse{Basemap: base}
}
