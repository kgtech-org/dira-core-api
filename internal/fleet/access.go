package fleet

// L'ACCÈS DU PARTENAIRE À SA CONSOLE — le chaînon sans lequel tout le reste
// est inatteignable.
//
// ⚠️⚠️ UN RÔLE `partner` NE S'AUTO-ATTRIBUE PAS, et c'est délibéré :
// `/auth/register` refuse ce rôle. Un partenaire a des pouvoirs sur le TRAVAIL
// D'AUTRES PERSONNES — il pose une limite de dette, il reprend une voiture —, et
// un rôle que n'importe qui obtient en postant un formulaire est un rôle qu'on
// ne peut pas donner à quelqu'un qui n'a pas signé de contrat. C'est donc
// l'exploitation qui ouvre l'accès, depuis la fiche de la flotte, au moment où
// elle enregistre le contrat.

import (
	"context"
	"log/slog"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// Accounts est le socle des comptes, vu par ce module.
//
// ⚠️ Déclarée côté CONSOMMATEUR, comme partout : ce module dit ce dont il a
// besoin — provisionner un compte, savoir ce qu'un compte est déjà — et ignore
// qui le fait.
type Accounts interface {
	EnsureAccount(ctx context.Context, role, phone, name, email, password, avatarURL string) (string, error)
	RoleOf(ctx context.Context, userID string) (string, error)
}

// SetAccounts branche les comptes (câblage). Sans eux, l'ouverture d'accès
// répond « indisponible » — et le reste du module continue de fonctionner.
func (s *Service) SetAccounts(a Accounts) { s.accounts = a }

var (
	errNoAccounts = apperr.Forbidden("fleet_access_unavailable",
		"partner access cannot be opened on this deployment")
	// errNeedsContact : on ne peut pas ouvrir un accès sans savoir à QUI.
	//
	// ⚠️ LES DEUX, ET PAS UN SEUL. Le téléphone identifie le compte sur toute
	// la plateforme ; l'adresse est ce avec quoi on se connecte à une console.
	// Ouvrir l'accès sans l'adresse aurait créé un compte qui ne peut pas
	// ouvrir sa propre console — un accès qui n'en est pas un.
	errNeedsContact = apperr.Validation(
		"the fleet needs a contact phone AND a contact email before its access can be opened").
		WithMeta(map[string]any{"fields": []string{"contact_phone", "contact_email"}})
	// errTakenByAnotherRole : ce numéro est déjà quelqu'un d'autre chez Dira.
	//
	// ⚠️ UN COMPTE, UN RÔLE, UNE APPLICATION — c'est la règle de la
	// plateforme, et la connexion la fait respecter (`403 wrong_app`). Lier
	// quand même ce compte à la flotte aurait produit le pire des cas : la
	// fiche afficherait « accès ouvert », et le partenaire se verrait refuser
	// l'entrée sans que personne ne comprenne pourquoi. Le refus dit quoi
	// faire — un autre numéro — au lieu de laisser découvrir la panne.
	errTakenByAnotherRole = apperr.Conflict("phone_belongs_to_another_role",
		"this phone already belongs to a Dira account that is not a partner: use another number")
)

// AccessResponse dit ce qui s'est passé, et c'est ce qui compte ici.
type AccessResponse struct {
	FleetID string `json:"fleet_id"`
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	// Created distingue « compte créé avec le mot de passe que vous venez de
	// taper » de « compte déjà existant, simplement rattaché ».
	//
	// ⚠️ SANS CE CHAMP, L'EXPLOITANT TRANSMET UN MOT DE PASSE QUI NE MARCHE
	// PAS. Un compte existant garde le sien — l'écraser depuis cette route
	// aurait fait de la fiche d'une flotte un outil pour prendre la main sur un
	// compte. L'écran doit donc dire « compte déjà existant : son mot de passe
	// est inchangé ».
	Created bool `json:"created"`
}

// OpenAccess ouvre à cette flotte l'accès à la console partenaire.
//
// ⚠️ LE COMPTE EST CELUI DU CONTACT DÉJÀ ENREGISTRÉ sur la fiche, pas une
// nouvelle saisie. Deux endroits où taper le nom et le numéro du gérant
// auraient divergé le jour où l'un des deux change — et c'est l'autre qui sert
// à le joindre.
func (s *Service) OpenAccess(ctx context.Context, id, password string) (*AccessResponse, error) {
	if s.accounts == nil {
		return nil, errNoAccounts
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errNotFound
	}
	f, err := s.repo.ByID(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if f == nil {
		return nil, errNotFound
	}
	plan, err := accessPlan(f)
	if err != nil {
		return nil, err
	}
	userID, err := s.accounts.EnsureAccount(ctx, auth.RolePartner,
		plan.Phone, plan.Name, plan.Email, password, "")
	if err != nil {
		return nil, err
	}
	// ⚠️ ON RELIT LE RÔLE APRÈS COUP. `EnsureAccount` rend le compte EXISTANT
	// d'un numéro connu sans toucher à son rôle : le gérant d'une flotte est
	// très souvent déjà un passager Dira. Sans cette relecture, on aurait
	// rattaché à la flotte un compte `client` incapable d'ouvrir la console, et
	// la fiche aurait annoncé un accès ouvert.
	role, err := s.accounts.RoleOf(ctx, userID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if err := ownerRoleOK(role); err != nil {
		return nil, err
	}
	created := f.OwnerUserID == nil || f.OwnerUserID.Hex() != userID
	before := toResponse(f)
	ownerOID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	updated, err := s.repo.Update(ctx, oid, bson.M{"owner_user_id": ownerOID})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, errNotFound
	}
	s.record(ctx, "fleet.access_opened", id, before, toResponse(updated))
	slog.InfoContext(ctx, "fleet: partner access opened",
		"fleet_id", id, "user_id", userID, "email", plan.Email)
	return &AccessResponse{
		FleetID: id, UserID: userID, Email: plan.Email,
		// ⚠️ `created` dit si le mot de passe TAPÉ est celui qui ouvre. Un
		// compte qui existait déjà garde le sien, et l'écran doit le dire.
		Created: created,
	}, nil
}

// accessPlan est le compte à provisionner, tiré de la fiche.
//
// ⚠️ EXTRAIT POUR ÊTRE ÉPROUVÉ : c'est ici qu'on décide avec QUOI on crée un
// compte qui aura des pouvoirs sur le travail d'autres personnes.
type accessPlanT struct {
	Phone string
	Email string
	Name  string
}

func accessPlan(f *Fleet) (accessPlanT, error) {
	p := accessPlanT{
		Phone: strings.TrimSpace(f.ContactPhone),
		Email: strings.ToLower(strings.TrimSpace(f.ContactEmail)),
		Name:  strings.TrimSpace(f.ContactName),
	}
	// ⚠️ LES DEUX SONT EXIGÉS. Le téléphone identifie le compte sur toute la
	// plateforme ; l'adresse est ce avec quoi on se connecte à une console.
	// Sans l'adresse, on crée un compte qui ne peut pas ouvrir sa propre
	// console — un accès qui n'en est pas un.
	if p.Phone == "" || p.Email == "" {
		return accessPlanT{}, errNeedsContact
	}
	if p.Name == "" {
		// Le nom de la SOCIÉTÉ à défaut de celui du gérant : un compte sans
		// nom s'affiche vide partout où on le croise.
		p.Name = strings.TrimSpace(f.Name)
	}
	return p, nil
}

// ownerRoleOK refuse de rattacher un compte qui n'est pas un partenaire.
//
// ⚠️ LE PIÈGE QUE CETTE FONCTION EXISTE POUR ATTRAPER : le gérant d'une flotte
// est très souvent DÉJÀ un passager Dira. `EnsureAccount` rend alors son compte
// `client` sans rien changer — et le rattacher aurait affiché « accès ouvert »
// sur la fiche pendant que la connexion lui répond `403 wrong_app`.
func ownerRoleOK(role string) error {
	if role == auth.RolePartner {
		return nil
	}
	return errTakenByAnotherRole.WithMeta(map[string]any{"role": role})
}
