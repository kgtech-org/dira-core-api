package fleet

// LE PERSONNEL D'UNE FLOTTE — plusieurs comptes pour une même société.
//
// ⚠️⚠️ UNE SOCIÉTÉ N'EST PAS UNE PERSONNE. Jusqu'ici une flotte avait UN compte,
// celui du gérant : il ouvrait la console, voyait le parc, posait les limites de
// dette. Une société de douze voitures a un propriétaire, un répartiteur qui
// place les voitures le matin, et un comptable qui relit les relevés — et aucun
// des trois ne doit faire le travail des deux autres avec le même mot de passe
// partagé. Un mot de passe partagé est d'ailleurs ce qui arrive quand on ne
// donne qu'un compte : trois personnes l'utilisent, et plus rien ne dit qui a
// posé la limite de dette qui a coupé quelqu'un.
//
// ⚠️ TROIS RÔLES, ET PAS DEUX. `viewer` existe parce qu'un comptable n'a aucune
// raison de pouvoir reprendre une voiture ; `manager` parce qu'un répartiteur
// doit pouvoir le faire sans toucher au personnel ; `owner` parce que celui qui
// a signé le contrat est le seul qui puisse décider qui entre.

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// Les rôles DANS une flotte.
const (
	// RoleOwner : celui qui a signé. Seul à pouvoir ajouter ou retirer
	// quelqu'un — un `manager` qui pourrait le faire s'ajouterait un
	// équivalent de propriétaire et mettrait le vrai dehors.
	RoleOwner = "owner"
	// RoleManager : les gestes du quotidien — rattacher, reprendre, poser une
	// limite, déposer les papiers, écrire au support.
	RoleManager = "manager"
	// RoleViewer : LIT, et rien d'autre. Un comptable relit les relevés ; il
	// n'a aucune raison de pouvoir couper le travail de quelqu'un.
	RoleViewer = "viewer"
)

// Member est un compte du personnel de la flotte.
type Member struct {
	UserID primitive.ObjectID `bson:"user_id"`
	Role   string             `bson:"role"`
	// AddedBy et AddedAt : qui a fait entrer cette personne, et quand.
	//
	// ⚠️ GARDÉS, parce que c'est la question du jour où une limite de dette
	// surprend tout le monde : « qui a accès ? » doit avoir une réponse datée.
	AddedBy string    `bson:"added_by,omitempty"`
	AddedAt time.Time `bson:"added_at,omitempty"`
}

// MemberResponse est un membre, tel que la console le lit.
type MemberResponse struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	Name   string `json:"name,omitempty"`
	Phone  string `json:"phone,omitempty"`
	Email  string `json:"email,omitempty"`
	// Owner distingue le signataire du contrat des membres ajoutés. ⚠️ Servi,
	// parce qu'on ne retire pas le propriétaire : l'écran doit cacher le
	// bouton plutôt que de le faire refuser.
	Owner   bool       `json:"owner"`
	AddedBy string     `json:"added_by,omitempty"`
	AddedAt *time.Time `json:"added_at,omitempty"`
}

var (
	errNotOwner = apperr.Forbidden("fleet_not_owner",
		"only the contract holder can manage the fleet's staff")
	errBadFleetRole = apperr.New("fleet_role_unknown",
		"role must be `manager` or `viewer`", 422).
		WithMeta(map[string]any{"fields": []string{"role"}})
	// errOwnerIsNotAMember : on n'ajoute pas le propriétaire comme membre.
	//
	// ⚠️ LE REFUS ÉVITE UN ÉTAT À DEUX VÉRITÉS. Le propriétaire est déjà
	// `owner` par `owner_user_id` ; l'ajouter comme `viewer` aurait créé un
	// compte qui est les deux à la fois — et la prochaine lecture aurait eu à
	// choisir laquelle gagne.
	errOwnerIsNotAMember = apperr.Conflict("fleet_owner_already",
		"this account already holds the contract: it has every right by default")
	errAlreadyMember = apperr.Conflict("fleet_already_member",
		"this account is already part of this fleet's staff")
	errMemberNotFound = apperr.NotFound("fleet_member_not_found",
		"this account is not part of this fleet's staff")
	// errTakenByAnotherRoleMember : même piège que l'ouverture d'accès.
	errMemberOtherRole = apperr.Conflict("phone_belongs_to_another_role",
		"this phone already belongs to a Dira account that is not a partner: use another number")
)

// CheckFleetRole n'admet que les deux rôles qu'on peut DONNER.
//
// ⚠️ `owner` N'EST PAS DANS LA LISTE : il ne se donne pas, il se signe. Le
// laisser passer aurait permis de fabriquer un second propriétaire — et le
// propriétaire est précisément celui qui décide qui entre.
func CheckFleetRole(role string) error {
	if role == RoleManager || role == RoleViewer {
		return nil
	}
	return errBadFleetRole.WithMeta(map[string]any{
		"fields": []string{"role"}, "expected": []string{RoleManager, RoleViewer},
	})
}

// CanWrite dit si ce rôle peut agir, ou seulement regarder.
//
// ⚠️ UNE SEULE FONCTION POUR TOUTE LA PLATEFORME, et elle est ici : les
// verticales reçoivent le rôle et appellent ceci. Trois modules qui
// décideraient chacun « est-ce que `viewer` peut ? » finiraient par répondre
// trois choses, et c'est celui qu'on regarde le moins qui laisserait passer.
func CanWrite(role string) bool { return role == RoleOwner || role == RoleManager }

// MemberOf rend la flotte d'un compte — propriétaire OU membre — et son rôle.
//
// ⚠️ UNE SEULE LECTURE POUR LES DEUX CAS. Chercher d'abord par propriétaire
// puis par membre aurait fait deux allers-retours sur la route la plus appelée
// de la console partenaire (elle est demandée à CHAQUE requête).
func (s *Service) MemberOf(ctx context.Context, userID string) (*Fleet, string, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, "", errNoFleet
	}
	f, err := s.repo.ByMember(ctx, uid)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	if f == nil {
		return nil, "", errNoFleet
	}
	if f.Status == StatusSuspended {
		return nil, "", errFleetSuspended
	}
	return f, roleIn(f, uid), nil
}

// roleIn dit ce que ce compte est dans cette flotte.
//
// ⚠️ LE PROPRIÉTAIRE D'ABORD : s'il figurait aussi dans `members` (données
// anciennes, import), c'est son titre de propriétaire qui doit gagner — sinon un
// signataire se retrouverait `viewer` sur sa propre société.
func roleIn(f *Fleet, uid primitive.ObjectID) string {
	if f.OwnerUserID != nil && *f.OwnerUserID == uid {
		return RoleOwner
	}
	for _, m := range f.Members {
		if m.UserID == uid {
			return m.Role
		}
	}
	return ""
}

// Members rend le personnel de la flotte du compte appelant.
func (s *Service) Members(ctx context.Context, userID string) ([]MemberResponse, error) {
	f, _, err := s.MemberOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(f.Members)+1)
	if f.OwnerUserID != nil {
		ids = append(ids, f.OwnerUserID.Hex())
	}
	for _, m := range f.Members {
		ids = append(ids, m.UserID.Hex())
	}
	// ⚠️ AU MIEUX : un annuaire injoignable rend la liste avec les
	// identifiants. Une page de personnel sans noms reste actionnable ; une
	// page qui échoue ne l'est pas — et c'est l'écran où l'on retire l'accès
	// de quelqu'un qui vient de partir.
	people := map[string]Identity{}
	if s.identities != nil && len(ids) > 0 {
		if m, err := s.identities.Identities(ctx, ids); err == nil {
			people = m
		}
	}
	out := make([]MemberResponse, 0, len(ids))
	if f.OwnerUserID != nil {
		id := f.OwnerUserID.Hex()
		out = append(out, MemberResponse{
			UserID: id, Role: RoleOwner, Owner: true,
			Name: people[id].Name, Phone: people[id].Phone, Email: people[id].Email,
		})
	}
	for _, m := range f.Members {
		id := m.UserID.Hex()
		row := MemberResponse{
			UserID: id, Role: m.Role, AddedBy: m.AddedBy,
			Name: people[id].Name, Phone: people[id].Phone, Email: people[id].Email,
		}
		if !m.AddedAt.IsZero() {
			at := m.AddedAt
			row.AddedAt = &at
		}
		out = append(out, row)
	}
	return out, nil
}

// AddMemberRequest est ce que le propriétaire envoie pour faire entrer
// quelqu'un.
type AddMemberRequest struct {
	Name  string `json:"name" validate:"required,min=1,max=120"`
	Phone string `json:"phone" validate:"required,e164"`
	Email string `json:"email" validate:"required,email"`
	Role  string `json:"role" validate:"required"`
	// Password : ⚠️ EXIGÉ, comme à l'ouverture de l'accès du gérant. Le
	// propriétaire le transmettra de vive voix ; un tirage du serveur masqué
	// finit recopié de travers.
	Password string `json:"password" validate:"required,min=8,max=120"`
}

// AddMember fait entrer un compte dans le personnel de la flotte.
//
// ⚠️ SEUL LE PROPRIÉTAIRE, et c'est la règle qui tient les autres : un
// `manager` qui pourrait ajouter quelqu'un s'ajouterait un second compte de
// `manager` — ou mettrait le propriétaire dehors.
func (s *Service) AddMember(ctx context.Context, userID string, req AddMemberRequest) (*MemberResponse, error) {
	f, role, err := s.MemberOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if role != RoleOwner {
		return nil, errNotOwner
	}
	if err := CheckFleetRole(req.Role); err != nil {
		return nil, err
	}
	if s.accounts == nil {
		return nil, errNoAccounts
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	memberID, err := s.accounts.EnsureAccount(ctx, auth.RolePartner,
		strings.TrimSpace(req.Phone), strings.TrimSpace(req.Name), email, req.Password, "")
	if err != nil {
		return nil, err
	}
	// ⚠️ MÊME PIÈGE QU'À L'OUVERTURE DE L'ACCÈS : `EnsureAccount` rend le
	// compte EXISTANT d'un numéro connu sans toucher à son rôle. Un répartiteur
	// qui est déjà passager Dira aurait été ajouté au personnel avec un compte
	// `client` — et la fiche aurait annoncé un accès que la connexion refuse.
	accountRole, err := s.accounts.RoleOf(ctx, memberID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if accountRole != auth.RolePartner {
		return nil, errMemberOtherRole.WithMeta(map[string]any{"role": accountRole})
	}
	mid, err := primitive.ObjectIDFromHex(memberID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if f.OwnerUserID != nil && *f.OwnerUserID == mid {
		return nil, errOwnerIsNotAMember
	}
	for _, m := range f.Members {
		if m.UserID == mid {
			return nil, errAlreadyMember
		}
	}
	member := Member{UserID: mid, Role: req.Role, AddedBy: userID, AddedAt: time.Now().UTC()}
	if err := s.repo.AddMember(ctx, f.ID, member); err != nil {
		return nil, err
	}
	s.record(ctx, "fleet.member_added", f.ID.Hex(), nil,
		map[string]any{"user_id": memberID, "role": req.Role})
	slog.InfoContext(ctx, "fleet: staff member added",
		"fleet_id", f.ID.Hex(), "user_id", memberID, "role", req.Role, "by", userID)
	at := member.AddedAt
	return &MemberResponse{
		UserID: memberID, Role: req.Role, Name: req.Name, Phone: req.Phone,
		Email: email, AddedBy: userID, AddedAt: &at,
	}, nil
}

// RemoveMember retire un compte du personnel.
//
// ⚠️ LE COMPTE N'EST PAS SUPPRIMÉ, seulement détaché : c'est peut-être le
// compte personnel de quelqu'un, et une société n'a pas à pouvoir effacer la
// personne qu'elle congédie. Détaché, il perd l'accès à la console (plus de
// flotte) et garde tout le reste.
func (s *Service) RemoveMember(ctx context.Context, userID, memberID string) error {
	f, role, err := s.MemberOf(ctx, userID)
	if err != nil {
		return err
	}
	if role != RoleOwner {
		return errNotOwner
	}
	mid, err := primitive.ObjectIDFromHex(memberID)
	if err != nil {
		return errMemberNotFound
	}
	// ⚠️ ON NE RETIRE PAS LE PROPRIÉTAIRE, même lui-même : une flotte sans
	// personne pour gérer le personnel ne peut plus en faire entrer, et il
	// faudrait l'exploitation pour rouvrir la porte. Le refus est explicite
	// plutôt que silencieux.
	if f.OwnerUserID != nil && *f.OwnerUserID == mid {
		return errOwnerIsNotAMember
	}
	ok, err := s.repo.RemoveMember(ctx, f.ID, mid)
	if err != nil {
		return err
	}
	if !ok {
		return errMemberNotFound
	}
	s.record(ctx, "fleet.member_removed", f.ID.Hex(), map[string]any{"user_id": memberID}, nil)
	slog.InfoContext(ctx, "fleet: staff member removed",
		"fleet_id", f.ID.Hex(), "user_id", memberID, "by", userID)
	return nil
}

// Identity est une personne du personnel, vue par ce module.
type Identity struct {
	Name  string
	Phone string
	Email string
}

// Identities nomme des comptes — déclarée côté consommateur, branchée au
// câblage.
type Identities interface {
	Identities(ctx context.Context, ids []string) (map[string]Identity, error)
}

// SetIdentities branche l'annuaire (câblage). Facultatif : sans lui, la liste
// du personnel s'affiche avec les identifiants.
func (s *Service) SetIdentities(i Identities) { s.identities = i }

// memberFilter trouve une flotte par son propriétaire OU par son personnel.
//
// ⚠️ EXTRAIT POUR ÊTRE ÉPROUVÉ EN DONNÉES. Si la clause des membres
// disparaissait, un répartiteur parfaitement légitime recevrait
// `partner_no_fleet` — un refus qui dit « votre contrat n'est pas enregistré »
// à quelqu'un dont le contrat l'est. Et l'inverse est pire : un filtre trop
// large rendrait la flotte de quelqu'un d'autre.
func memberFilter(uid primitive.ObjectID) bson.M {
	return bson.M{"$or": []bson.M{
		{"owner_user_id": uid},
		{"members.user_id": uid},
	}}
}
