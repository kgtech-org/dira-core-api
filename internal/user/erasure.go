package user

// LA SUPPRESSION D'UN COMPTE — en deux temps, et sans effacer les opérations.
//
// Quelqu'un demande que son compte disparaisse. Deux choses s'opposent, et
// aucune ne peut être sacrifiée :
//
//   - la personne a le droit de ne plus être dans nos bases ;
//   - les courses et les commandes qu'elle a payées sont des écritures
//     COMPTABLES. Les effacer trouerait le journal en partie double, ferait
//     échouer le balayage d'intégrité, et retirerait d'une facturation par pays
//     des montants déjà déclarés.
//
// ⚠️ ON EFFACE DONC LA PERSONNE, PAS L'OPÉRATION. La course garde son prix,
// sa commission et sa date ; elle ne désigne plus personne.
//
// ⚠️ ET CELA SUFFIT À ANONYMISER TOUT L'HISTORIQUE, parce que les verticales
// ne stockent NI NOM NI TÉLÉPHONE : une course porte un identifiant de compte
// et demande l'identité au socle au moment de l'afficher (`UserNames`,
// `ContactOf`). Anonymiser ici, c'est anonymiser toutes les courses et toutes
// les commandes, partout où elles se lisent — console comprise. C'est le socle
// commun qui rend cette fonction tenable ; avec un nom recopié dans chaque
// verticale, il aurait fallu les parcourir toutes, et en oublier une.
//
// LES DEUX TEMPS :
//
//  1. LA FERMETURE, immédiate. Le compte ne se connecte plus, ses sessions
//     tombent, ses appareils de notification partent. Rien de nouveau ne peut
//     être commandé.
//  2. L'EFFACEMENT, après un DÉLAI DE GRÂCE (trente jours par défaut). C'est
//     lui qui rend la fonction sûre, et il résout deux problèmes d'un coup :
//     une personne qui a touché le bouton par erreur a le temps d'écrire au
//     support, et une course en cours au moment de la demande se termine
//     normalement — le chauffeur voit encore le nom de qui est dans sa
//     voiture, ce dont il a besoin pour faire son travail.
//
// ⚠️ LE SOCLE NE DEMANDE RIEN AUX VERTICALES, ici comme ailleurs (voir
// `internal/callback`). Il ne leur demande donc pas « puis-je effacer ? » : il
// leur DIT que le compte est effacé, une fois, et chacune purge ce qu'elle
// seule détient — les messages de conversation, les fils de support, les pièces
// de conformité. Un socle qui interrogerait deux verticales avant d'accepter
// une suppression ne pourrait plus être déployé seul.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// DefaultErasureGrace : trente jours entre la fermeture et l'effacement.
//
// Assez long pour qu'une erreur se rattrape et qu'une course en cours se
// termine ; assez court pour que « supprimer » veuille dire quelque chose.
const DefaultErasureGrace = 30 * 24 * time.Hour

// AnonymousName est ce qui s'affiche à la place du nom, partout où une
// opération passée nomme encore son auteur.
//
// ⚠️ UN LIBELLÉ, PAS UNE CHAÎNE VIDE. Un nom vide laisse les écrans dessiner
// un rond d'initiale blanc et une ligne sans rien, et personne ne sait si la
// donnée manque ou si le compte a été supprimé.
const AnonymousName = "Compte supprimé"

var (
	errAccountClosed = apperr.Forbidden("account_closed",
		"this account has been closed")
	errAlreadyClosing = apperr.Conflict("deletion_already_requested",
		"this account is already scheduled for deletion")
	errWalletNotEmpty = apperr.Conflict("wallet_not_empty",
		"this account still holds money; empty it or ask support for a refund before deleting")
	errErasureNotSelfServe = apperr.Forbidden("erasure_not_self_serve",
		"this account is deleted by support, not from the app")
	errAlreadyErased = apperr.Conflict("account_already_erased",
		"this account's data is already erased and cannot be restored")
)

// Balances rend ce qu'un compte détient encore — l'argent du client, les
// jetons d'un agent ou d'un marchand.
//
// Déclarée côté consommateur. FACULTATIVE : sans elle, la vérification d'argent
// ne se fait pas, et le journal le dit. Mieux vaut une suppression non vérifiée
// qu'un module de portefeuille absent qui empêche toute suppression — mais on
// ne s'en aperçoit qu'en lisant le journal, et c'est pour cela qu'il crie.
type Balances interface {
	// NonZeroBalance dit si ce compte détient encore quelque chose, et quoi.
	NonZeroBalance(ctx context.Context, ownerID string) (what string, held bool)
}

// SetBalances branche le portefeuille.
func (s *Service) SetBalances(b Balances) { s.balances = b }

// ErasureAnnouncer dit aux verticales qu'un compte vient d'être effacé, pour
// qu'elles purgent ce qu'elles seules détiennent.
//
// ⚠️ UNIDIRECTIONNEL, et au mieux : on ne lui demande pas la permission, on ne
// l'attend pas, et un échec n'annule pas l'effacement du socle. L'identité est
// déjà partie ; ce qui reste chez les verticales est du texte écrit par la
// personne, qu'une relance effacera.
type ErasureAnnouncer interface {
	// ⚠️ LE TÉLÉPHONE PART AVEC, et c'est un arbitrage, pas une facilité :
	// une verticale garde des traces classées par NUMÉRO et non par compte —
	// la conversation du robot WhatsApp, qui porte le numéro et tout ce que la
	// personne a écrit pour commander. Sans lui, elles resteraient là pour
	// toujours, et rien ne dirait comment les retrouver. Voir
	// `jobs.AccountErasedPayload`, qui en paie le prix par une rétention
	// courte.
	AccountErased(ctx context.Context, userID, phone string)
}

// SetErasureAnnouncer branche l'annonce aux verticales.
func (s *Service) SetErasureAnnouncer(a ErasureAnnouncer) { s.erasure = a }

// SetErasureGrace règle le délai entre la fermeture et l'effacement.
func (s *Service) SetErasureGrace(d time.Duration) {
	if d > 0 {
		s.erasureGrace = d
	}
}

func (s *Service) grace() time.Duration {
	if s.erasureGrace > 0 {
		return s.erasureGrace
	}
	return DefaultErasureGrace
}

// RequestErasure ferme le compte de la personne qui le demande, et programme
// son effacement.
//
// ⚠️ ELLE EXIGE DE PROUVER QUI ON EST. C'est irréversible depuis l'application :
// un jeton volé, un téléphone déverrouillé posé sur une table, et le compte de
// quelqu'un disparaît. Le mot de passe quand il y en a un ; un code à usage
// unique quand le compte s'est inscrit par code — la même porte que pour entrer.
//
// ⚠️ RÉSERVÉE AUX CLIENTS. Un chauffeur, un livreur ou un marchand porte une
// DETTE et des versements que le socle ne sait pas lire — ils vivent dans la
// verticale —, et un compte de staff n'est pas à soi. Pour eux, la suppression
// passe par le support, qui a le grand livre sous les yeux avant de décider.
func (s *Service) RequestErasure(ctx context.Context, userID string, req ErasureRequest) error {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if u.Role != auth.RoleClient {
		return errErasureNotSelfServe
	}
	if u.DeletionRequestedAt != nil {
		return errAlreadyClosing
	}
	if err := s.proveIdentity(ctx, u, req); err != nil {
		return err
	}
	return s.closeAccount(ctx, u, "self")
}

// proveIdentity redemande ce qui ouvre le compte.
func (s *Service) proveIdentity(ctx context.Context, u *User, req ErasureRequest) error {
	if u.PasswordHash != "" {
		ok, err := VerifyPassword(req.Password, u.PasswordHash)
		if err != nil {
			return errInvalidCredentials
		}
		if !ok {
			return errInvalidCredentials
		}
		return nil
	}
	// Compte né par code : on en redemande un. `VerifyOTP` ouvrirait une
	// session ; ici on ne veut que la preuve, donc on relit le code
	// directement — et on le consomme, pour qu'il ne serve pas deux fois.
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return apperr.Validation("send the one-time code sent to your phone")
	}
	rec, err := s.repo.FindOTP(ctx, u.Phone)
	if err != nil {
		return apperr.Internal(err)
	}
	if rec == nil || time.Now().UTC().After(rec.ExpiresAt) {
		return errOTPInvalid
	}
	if rec.Hash != s.hashOTP(u.Phone, code) {
		if _, aerr := s.repo.IncOTPAttempts(ctx, u.Phone); aerr != nil {
			return apperr.Internal(aerr)
		}
		return errOTPInvalid
	}
	if err := s.repo.DeleteOTP(ctx, u.Phone); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// closeAccount ferme et programme — le premier des deux temps.
func (s *Service) closeAccount(ctx context.Context, u *User, by string) error {
	if s.balances != nil {
		if what, held := s.balances.NonZeroBalance(ctx, u.ID.Hex()); held {
			// ⚠️ ON NE DÉTRUIT PAS DE L'ARGENT EN SILENCE. Un solde de douze
			// mille francs qui disparaît parce que quelqu'un a touché
			// « supprimer mon compte » est une perte qu'il découvrira trop
			// tard, et qu'aucune écriture ne pourra lui rendre.
			return errWalletNotEmpty.WithMeta(map[string]any{"reason": what})
		}
	} else {
		slog.WarnContext(ctx, "user: erasure without a balance check — wallet module not wired",
			"user_id", u.ID.Hex())
	}

	now := time.Now().UTC()
	if err := s.repo.CloseAccount(ctx, u.ID, now); err != nil {
		return apperr.Internal(err)
	}
	// Les sessions tombent TOUT DE SUITE. Garder un jeton vivant trente jours
	// sur un compte qu'on vient de fermer laisserait commander pendant tout le
	// délai de grâce — et il n'y aurait plus personne pour en répondre.
	if _, err := s.repo.DeleteRefreshTokensOfUser(ctx, u.ID); err != nil {
		slog.WarnContext(ctx, "user: sessions not dropped on close", "user_id", u.ID.Hex(), "error", err)
	}
	if s.sessions != nil && u.Device != nil {
		// Le registre d'appareil d'un agent : la clé part, le suivi cesse
		// d'accepter son socket.
		s.sessions.Release(ctx, u.ID.Hex(), u.Device.ID)
	}
	if s.devices != nil {
		s.devices.ForgetAll(ctx, u.ID.Hex())
	}
	if s.auditor != nil {
		// ⚠️ NI NOM NI TÉLÉPHONE DANS LE JOURNAL D'AUDIT. Il est conservé sept
		// ans : y recopier l'identité à l'instant où on l'efface annulerait
		// l'effacement, dans le seul endroit que personne ne pense à relire.
		s.auditor.Record(ctx, "user.close", "user", u.ID.Hex(),
			map[string]any{"role": u.Role, "by": by, "erase_at": now.Add(s.grace())}, nil)
	}
	slog.InfoContext(ctx, "user: account closed, erasure scheduled",
		"user_id", u.ID.Hex(), "by", by, "erase_at", now.Add(s.grace()))
	return nil
}

// EraseDue efface les comptes dont le délai de grâce est écoulé, et rend
// combien l'ont été. Appelée par un balayage périodique.
func (s *Service) EraseDue(ctx context.Context, limit int) (int, error) {
	due, err := s.repo.AccountsDueForErasure(ctx, time.Now().UTC().Add(-s.grace()), limit)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, u := range due {
		erased, err := s.erase(ctx, u)
		if err != nil {
			slog.ErrorContext(ctx, "user: erasure failed", "user_id", u.ID.Hex(), "error", err)
			continue
		}
		if erased {
			done++
		}
	}
	return done, nil
}

// EraseNow efface sans attendre — la voie de l'exploitation, pour une demande
// légale ou un compte frauduleux.
func (s *Service) EraseNow(ctx context.Context, userID string) error {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return err
	}
	if u.DeletionRequestedAt == nil {
		if err := s.closeAccount(ctx, u, "admin"); err != nil {
			return err
		}
		u, err = s.findUser(ctx, userID)
		if err != nil {
			return err
		}
	}
	_, err = s.erase(ctx, u)
	return err
}

// erase est le second temps : l'identité s'en va, l'opération reste. Rend
// `false` quand il n'y avait rien à faire — déjà effacé, ou pas à effacer.
func (s *Service) erase(ctx context.Context, u *User) (bool, error) {
	if u.AnonymisedAt != nil {
		return false, nil
	}
	// ⚠️ DEUXIÈME VERROU : un compte qui n'est pas fermé ne s'efface pas, même
	// s'il porte encore une date de demande. Réactiver efface cette date
	// (`SetAccountStatus`), mais ce balayage tourne toutes les heures sur la
	// collection des comptes : si un jour un chemin oublie de la retirer, la
	// faute doit être un journal à lire, pas un client actif qui perd son nom
	// trois semaines plus tard.
	if u.Status != StatusClosed {
		slog.WarnContext(ctx, "user: erasure skipped — this account is not closed",
			"user_id", u.ID.Hex(), "status", u.Status)
		return false, nil
	}
	// ⚠️ LE TÉLÉPHONE EST BROUILLÉ, PAS VIDÉ, et c'est l'index unique qui
	// l'exige : deux comptes effacés avec un téléphone vide entreraient en
	// collision, et le second effacement échouerait. Le brouiller LIBÈRE en
	// plus le numéro — la personne peut revenir et s'inscrire à neuf, ce qui
	// est précisément son droit.
	scrambled := fmt.Sprintf("deleted-%s", u.ID.Hex())
	if err := s.repo.AnonymiseUser(ctx, u.ID, scrambled, AnonymousName, time.Now().UTC()); err != nil {
		return false, err
	}
	// Ce que le socle détient d'elle et qui n'est pas une écriture comptable.
	if err := s.repo.DeleteAddressesOf(ctx, u.ID); err != nil {
		slog.WarnContext(ctx, "user: addresses not deleted", "user_id", u.ID.Hex(), "error", err)
	}
	// LA PHOTO DE PROFIL — le fichier, pas seulement le champ.
	//
	// ⚠️ APRÈS L'ANONYMISATION, DONC AVEC L'URL QU'ON TIENT ENCORE EN MÉMOIRE :
	// le document ne la porte plus. L'ordre inverse aurait perdu l'adresse de
	// l'image avant de l'avoir supprimée.
	if u.AvatarURL != "" {
		if s.files == nil {
			slog.ErrorContext(ctx, "user: no object store wired — the PHOTO of an erased account stays in the bucket",
				"user_id", u.ID.Hex())
		} else if err := s.files.Remove(ctx, u.AvatarURL); err != nil {
			slog.ErrorContext(ctx, "user: the photo of an erased account survived",
				"user_id", u.ID.Hex(), "error", err)
		}
	}
	if s.inbox != nil {
		s.inbox.PurgeOf(ctx, u.ID.Hex())
	}
	// Et ce que les verticales détiennent : les conversations, les fils de
	// support, les pièces de conformité. Au mieux, une fois.
	if s.erasure != nil {
		// ⚠️ `u.Phone` EST ENCORE L'ANCIEN ICI : la ligne a été anonymisée en
		// base, mais l'exemplaire qu'on tient en mémoire date d'avant. C'est
		// voulu, et c'est la seule fenêtre où ce numéro existe encore pour
		// aller purger ce qui est classé dessus.
		s.erasure.AccountErased(ctx, u.ID.Hex(), u.Phone)
	} else {
		slog.WarnContext(ctx, "user: verticals not told of the erasure — nothing wired",
			"user_id", u.ID.Hex())
	}
	if s.auditor != nil {
		s.auditor.Record(ctx, "user.erase", "user", u.ID.Hex(),
			map[string]any{"role": u.Role}, nil)
	}
	slog.InfoContext(ctx, "user: account erased", "user_id", u.ID.Hex())
	return true, nil
}

// Inbox est la boîte de notifications d'un compte — purgée à l'effacement.
//
// Déclarée côté consommateur, FACULTATIVE : une boîte non branchée ne doit pas
// empêcher d'effacer une identité.
type Inbox interface {
	PurgeOf(ctx context.Context, userID string)
}

// SetInbox branche la boîte.
func (s *Service) SetInbox(i Inbox) { s.inbox = i }

// PushDevices sont les appareils à qui l'on pousse des notifications.
type PushDevices interface {
	ForgetAll(ctx context.Context, userID string)
}

// SetPushDevices branche le registre des appareils de notification.
func (s *Service) SetPushDevices(d PushDevices) { s.devices = d }

// Files retire un fichier du stockage d'objets, désigné par son URL publique.
//
// Déclarée côté consommateur — c'est `pkg/storage.Store`. FACULTATIVE : sans
// elle, l'identité part quand même et le journal crie.
//
// ⚠️ ELLE EXISTE POUR LA PHOTO DE PROFIL, et c'est tout sauf un détail.
// `anonymised_at` posé et `avatar_url` retiré du document laissent le VISAGE de
// la personne dans le bucket, et plus rien ne le désigne : impossible à
// retrouver pour le supprimer, impossible à justifier si on le trouve. Un
// compte « effacé » dont la photo survit n'est pas effacé.
type Files interface {
	Remove(ctx context.Context, publicURL string) error
}

// SetFiles branche le stockage d'objets.
func (s *Service) SetFiles(f Files) { s.files = f }

// erasureAt rend la date d'effacement prévue d'un compte fermé.
func (s *Service) erasureAt(u *User) *time.Time {
	if u == nil || u.DeletionRequestedAt == nil {
		return nil
	}
	at := u.DeletionRequestedAt.Add(s.grace())
	return &at
}

// userResponse rend le compte avec sa date d'effacement prévue.
//
// ⚠️ ELLE EST CALCULÉE, PAS STOCKÉE, et c'est pour que le délai de grâce reste
// un RÉGLAGE : le raccourcir pour un pays qui l'exige doit avancer l'échéance
// des comptes déjà fermés, pas seulement celle des suivants. Une date écrite à
// la fermeture aurait figé l'ancien délai sur tout ce qui attendait.
func (s *Service) accountResponse(u *User) UserResponse {
	out := newUserResponse(u)
	out.EraseAt = s.erasureAt(u)
	return out
}

// ErasureStatus rend ce qu'il faut afficher après la demande : le compte est
// fermé, et voici quand l'identité partira.
func (s *Service) ErasureStatus(ctx context.Context, userID string) ErasureResponse {
	out := ErasureResponse{Status: StatusClosed}
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return out
	}
	if at := s.erasureAt(u); at != nil {
		out.EraseAt = *at
	}
	return out
}
