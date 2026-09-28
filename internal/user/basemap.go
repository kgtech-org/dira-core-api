package user

// LE FOND DE CARTE SERVI À L'AUTHENTIFICATION.
//
// ⚠️ POURQUOI ICI, ET PAS SUR UNE ROUTE À PART. Une application a besoin du
// fond de carte à la seconde où elle ouvre son premier écran ; un appel dédié
// aurait ajouté un aller-retour avant la première carte, et surtout un état de
// plus à gérer — « la clé n'est pas encore arrivée ». Elle voyage donc avec le
// jeton, qui arrive de toute façon avant tout le reste.
//
// ⚠️ ET AUSSI AU RAFRAÎCHISSEMENT, ce qui est le vrai point. Servie seulement à
// la connexion, une clé changée dans la console n'atteindrait un chauffeur resté
// connecté que trente jours plus tard : la rotation — seule raison de servir la
// clé depuis le serveur plutôt que de la compiler dans l'application — ne
// servirait à rien. Au rafraîchissement, la dérive se borne à la durée d'un
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
	Grant(ctx context.Context, countryCode, platform string) (basemap, googleKey, googleMapType string)
}

// SetBasemaps branche le service de pays.
func (s *Service) SetBasemaps(b Basemaps) { s.basemaps = b }

// basemap rend ce que l'application doit afficher, ou `nil` quand il n'y a rien
// à dire — plateforme muette, module absent.
//
// ⚠️ LE PAYS VIENT DE LA REQUÊTE, PAS DU COMPTE, et dans cet ordre. Un passager
// togolais qui ouvre l'application à Dakar doit voir le fond réglé pour le
// Sénégal : c'est là qu'il est, et c'est la clé sénégalaise qui est restreinte
// et facturée pour ce trafic. Le pays du compte ne sert que de repli, quand
// aucun en-tête n'a été posé.
func (s *Service) basemap(ctx context.Context, u *User, platform string) *MapsResponse {
	if s.basemaps == nil || platform == "" {
		return nil
	}
	code := country.FromContext(ctx)
	if code == "" && u != nil {
		code = u.Country
	}
	if code == "" {
		return nil
	}
	base, key, mapType := s.basemaps.Grant(ctx, code, platform)
	if base == "" {
		return nil
	}
	return &MapsResponse{Basemap: base, GoogleKey: key, GoogleMapType: mapType}
}
