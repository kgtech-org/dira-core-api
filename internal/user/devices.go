package user

// COMBIEN D'APPAREILS UN COMPTE PEUT-IL TENIR — et lequel part quand il y en a
// un de trop.
//
// Un client a le droit d'être sur son téléphone ET sa tablette ; un marchand
// tient sa caisse sur deux écrans ; un membre du staff ouvre trois onglets.
// Jusqu'ici, ce droit n'avait AUCUNE borne : un compte pouvait accumuler vingt
// sessions vivantes, chacune avec un jeton de rafraîchissement de trente jours,
// et rien ne le montrait nulle part. Un compte partagé par tout un quartier
// ressemblait exactement à un compte ordinaire.
//
// Le pays décide du nombre (trois par défaut), et c'est le PLUS ANCIEN qui
// part — pas le nouveau.
//
// ⚠️ « LE PLUS ANCIEN » VEUT DIRE « LE PLUS SILENCIEUX », et c'est voulu. Une
// session est datée de son dernier rafraîchissement, pas de sa création : le
// téléphone qu'on utilise tous les jours se redate tout seul, celui qu'on a
// rangé dans un tiroir il y a trois semaines ne se redate plus. Évincer « le
// premier connecté » aurait déconnecté le téléphone principal de quelqu'un
// parce qu'il l'a depuis longtemps.
//
// ⚠️ ET CELA NE CONCERNE PAS LES CHAUFFEURS NI LES LIVREURS. Eux n'ont qu'un
// appareil, et ce n'est pas un réglage : c'est le dispatch. Deux téléphones en
// ligne pour un seul véhicule, ce sont deux flux de positions, une voiture qui
// saute d'un quartier à l'autre sur la carte du passager, et un appel qui part
// vers le téléphone resté à la maison. Leur règle vit dans `device.go`, elle
// est plus stricte, et elle ne se règle pas depuis un écran.

import (
	"context"
	"log/slog"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// defaultMaxDevices est la valeur retenue quand aucun pays ne répond — la même
// que le défaut du socle (`country.DefaultMaxDevices`), redéclarée ici pour ne
// pas faire dépendre les comptes du module de pays.
const defaultMaxDevices = 3

// enforceDeviceLimit coupe les sessions en trop, et rend combien sont parties.
//
// Appelée APRÈS l'émission du jeton de la nouvelle session — jamais avant :
// trimmer d'abord puis insérer aurait laissé passer une session de plus à
// chaque connexion, ce qui est exactement ce que la borne doit empêcher.
//
// ⚠️ AU MIEUX, TOUJOURS DANS LE SENS DE LAISSER TRAVAILLER. Une erreur de
// nettoyage est journalisée, jamais rendue : quelqu'un vient de donner son mot
// de passe ou son code, sa session est ouverte, et lui répondre « erreur » pour
// un ménage raté serait lui refuser l'entrée pour une raison qui ne le regarde
// pas.
func (s *Service) enforceDeviceLimit(ctx context.Context, u *User, keepHash, deviceID string) {
	if u == nil || singleDevice(u) {
		// Les agents ont leur propre règle, plus stricte, déjà appliquée par
		// `claimDevice` : leurs anciens jetons sont déjà partis.
		return
	}
	max := defaultMaxDevices
	if s.policies != nil {
		code := country.FromContext(ctx)
		if code == "" {
			code = u.Country
		}
		if code != "" {
			if n := s.policies.MaxDevicesOf(ctx, code); n > 0 {
				max = n
			}
		}
	}
	evicted, err := s.repo.TrimRefreshTokens(ctx, u.ID, keepHash, deviceID, max)
	if err != nil {
		slog.WarnContext(ctx, "user: device limit not enforced", "user_id", u.ID.Hex(), "error", err)
		return
	}
	if evicted > 0 {
		// ⚠️ JOURNALISÉ, PARCE QUE C'EST UNE DÉCONNEXION QUE PERSONNE N'A
		// DEMANDÉE. Le jour où quelqu'un dit « je suis déconnecté tout le
		// temps », c'est cette ligne qui le dit — et elle dit aussi combien de
		// sessions ce compte accumulait.
		slog.InfoContext(ctx, "user: oldest sessions signed out (device limit)",
			"user_id", u.ID.Hex(), "evicted", evicted, "max", max)
	}
}

// sessionDeviceOf normalise l'identifiant d'installation déclaré par une
// application, pour TOUT compte — y compris ceux qui n'ont aucune règle
// d'appareil unique.
//
// ⚠️ IL NE VA PAS DANS LE JETON. Le jeton d'un client ne nomme aucun appareil,
// et il ne doit pas commencer : c'est ce qui le soumettrait à la règle de
// chasse des chauffeurs (`deviceForRefresh`). Celui-ci ne vit que sur la ligne
// de session, pour reconnaître le même téléphone qui revient.
func sessionDeviceOf(raw string) string {
	id, _ := deviceFrom(raw, "")
	return id
}
