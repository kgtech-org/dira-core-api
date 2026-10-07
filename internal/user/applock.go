package user

// LE VERROU DE L'APPLICATION, SERVI À L'AUTHENTIFICATION.
//
// Biométrie ou code secret devant l'application : qui ouvre le téléphone de
// quelqu'un n'ouvre pas pour autant son compte Dira. Le verrou lui-même vit
// DANS l'application — le serveur ne peut ni le poser, ni le vérifier, ni
// savoir si le téléphone resté chez soi est verrouillé. Ce que le serveur dit,
// c'est la POLITIQUE du pays : proposer, imposer, ou se taire.
//
// ⚠️ IL VOYAGE AVEC LE JETON, comme le fond de carte, et pour la même raison :
// l'application doit savoir s'il faut verrouiller AVANT de dessiner son premier
// écran. Une route dédiée aurait ajouté un aller-retour pendant lequel le
// contenu est déjà à l'écran — c'est-à-dire exactement ce que le verrou doit
// empêcher.
//
// ⚠️ ET AUSSI AU RAFRAÎCHISSEMENT : sans cela, un pays qui impose le verrou
// n'atteindrait les applications déjà connectées que trente jours plus tard.

import (
	"context"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// CountryPolicies rend ce qu'un PAYS décide pour les applications : le verrou,
// et le nombre d'appareils qu'un compte peut tenir.
//
// Déclarée côté consommateur, comme `Basemaps`. FACULTATIVE : sans elle,
// l'application garde le réglage qu'elle avait et la borne retombe sur son
// défaut — une politique illisible ne doit jamais empêcher quelqu'un de se
// connecter.
type CountryPolicies interface {
	AppLockOf(ctx context.Context, countryCode string) (mode string, biometrics bool, pinLength, graceSeconds, maxAttempts int, ok bool)
	// MaxDevicesOf rend le nombre d'appareils admis — voir `devices.go`.
	MaxDevicesOf(ctx context.Context, countryCode string) int
}

// SetCountryPolicies branche le service de pays.
func (s *Service) SetCountryPolicies(p CountryPolicies) { s.policies = p }

// appLock rend la politique à appliquer, ou `nil` quand il n'y a rien à dire.
//
// ⚠️ LE PAYS VIENT DE LA REQUÊTE, PAS DU COMPTE, et dans cet ordre — comme le
// fond de carte. Quelqu'un qui ouvre l'application dans un pays qui impose le
// verrou doit le voir imposé, même si son compte est ailleurs : la règle
// protège le téléphone qui est là, pas le compte qui est loin.
func (s *Service) appLock(ctx context.Context, u *User) *AppLockResponse {
	if s.policies == nil {
		return nil
	}
	code := country.FromContext(ctx)
	if code == "" && u != nil {
		code = u.Country
	}
	if code == "" {
		return nil
	}
	mode, biometrics, pinLength, grace, maxAttempts, ok := s.policies.AppLockOf(ctx, code)
	if !ok {
		return nil
	}
	return &AppLockResponse{
		Mode:         mode,
		Biometrics:   biometrics,
		PINLength:    pinLength,
		GraceSeconds: grace,
		MaxAttempts:  maxAttempts,
	}
}
