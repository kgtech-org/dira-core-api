package staff

import (
	"context"
	"slices"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Accounts est ce que ce paquet demande à l'annuaire des comptes.
//
// Déclarée côté consommateur : ce module n'a pas à connaître le type `User` de
// `internal/user`, il a besoin de trois réponses.
type Accounts interface {
	// RoleOf rend le rôle d'un compte, ou "" s'il n'existe pas.
	RoleOf(ctx context.Context, userID string) (string, error)
	// Identities rend nom, téléphone et statut de plusieurs comptes.
	Identities(ctx context.Context, ids []string) (map[string]Identity, error)
}

// Identity est le strict nécessaire pour afficher une ligne de staff.
type Identity struct {
	Name   string `json:"name"`
	Phone  string `json:"phone"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

// Auditor enregistre les gestes sensibles. Facultatif.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Service applies the staff rules.
type Service struct {
	repo     *Repository
	accounts Accounts
	audit    Auditor
}

func NewService(repo *Repository, accounts Accounts) *Service {
	return &Service{repo: repo, accounts: accounts}
}

func (s *Service) SetAuditor(a Auditor) { s.audit = a }

func (s *Service) record(ctx context.Context, action, id string, before, after any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "staff", id, before, after)
}

// Response is a staff member as the console sees it.
type Response struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Name   string `json:"name,omitempty"`
	Phone  string `json:"phone,omitempty"`
	Email  string `json:"email,omitempty"`
	// AccountStatus vient du COMPTE ; Status est celui de la FICHE. Les deux,
	// parce qu'ils ne disent pas la même chose : un compte suspendu ne peut
	// plus se connecter, une fiche suspendue retire les habilitations sans
	// fermer la porte. L'exploitation a besoin de distinguer les deux gestes.
	AccountStatus string   `json:"account_status,omitempty"`
	Function      string   `json:"function"`
	Title         string   `json:"title,omitempty"`
	Scopes        []string `json:"scopes"`
	// CoversEverything est un CONFORT d'affichage, calculé depuis `scopes`.
	// La console peut résumer « toute la plateforme » sans recompter, et
	// `scopes` reste la seule vérité — c'est lui qu'on modifie.
	CoversEverything bool   `json:"covers_everything"`
	Status           string `json:"status"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

func toResponse(m *Member) Response {
	return Response{
		ID: m.ID.Hex(), UserID: m.UserID.Hex(), Function: m.Function, Title: m.Title,
		Scopes: m.Scopes, CoversEverything: m.CoversEverything(), Status: m.Status,
		CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: m.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// CreateRequest attaches a staff record to an EXISTING admin account.
type CreateRequest struct {
	UserID   string   `json:"user_id" validate:"required,len=24,hexadecimal"`
	Function string   `json:"function" validate:"required"`
	Title    string   `json:"title" validate:"omitempty,max=120"`
	Scopes   []string `json:"scopes"`
}

// UpdateRequest carries only what changes — chaque champ est un POINTEUR,
// sinon « ne pas toucher au périmètre » et « retirer toute restriction »
// s'écrivent pareil, et changer un intitulé rendrait quelqu'un tout-puissant.
type UpdateRequest struct {
	Function *string   `json:"function"`
	Title    *string   `json:"title" validate:"omitempty,max=120"`
	Scopes   *[]string `json:"scopes"`
	Status   *string   `json:"status" validate:"omitempty,oneof=active suspended"`
}

// requireDirection refuse à un membre du staff de modifier l'ÉQUIPE s'il n'a
// pas la fonction de direction.
//
// ⚠️ Lire l'équipe et la MODIFIER ne sont pas le même geste. Sans cette
// distinction, un chargé de support pouvait s'attribuer toutes les portées :
// le périmètre ne bornait alors plus personne, puisque tout le monde pouvait
// se le retirer. Une habilitation qu'on peut s'accorder soi-même n'en est pas
// une.
//
// Le PROVISIONNEMENT passe : il s'exécute hors requête, sans acteur dans le
// contexte, et c'est lui qui crée le premier administrateur — exiger une
// direction préexistante empêcherait de démarrer.
func (s *Service) requireDirection(ctx context.Context) error {
	actor, ok := auth.UserFromContext(ctx)
	if !ok || actor == "" {
		return nil // hors requête : provisionnement
	}
	uid, err := primitive.ObjectIDFromHex(actor)
	if err != nil {
		return errNotDirection
	}
	m, err := s.repo.ByUserID(ctx, uid)
	if err != nil {
		return apperr.Internal(err)
	}
	if !mayChangeTeam(m) {
		return errNotDirection
	}
	return nil
}

// mayChangeTeam porte la RÈGLE, séparée de la lecture en base pour être
// testable.
//
// ⚠️ Une fonction pure, appelée par le test. Écrire la condition une seconde
// fois dans le test aurait produit une garantie qui reste verte quand le code
// change — c'est exactement ce qui était arrivé à la règle des portées
// suspendues.
func mayChangeTeam(m *Member) bool {
	return m != nil && m.Status == StatusActive && m.Function == FunctionAdmin
}

// Create attaches a staff record.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Response, error) {
	if err := s.requireDirection(ctx); err != nil {
		return nil, err
	}
	if !slices.Contains(Functions, req.Function) {
		return nil, errBadFunction.WithMeta(map[string]any{"expected": Functions})
	}
	scopes, err := normaliseScopes(req.Scopes)
	if err != nil {
		return nil, err
	}
	uid, err := primitive.ObjectIDFromHex(req.UserID)
	if err != nil {
		return nil, apperr.Validation("user_id is not a valid id")
	}
	// ⚠️ LE COMPTE DOIT ÊTRE ADMIN. Attacher une fiche de staff à un client
	// lui donnerait un titre et un périmètre sans lui donner l'accès — la
	// console l'afficherait parmi les employés, et il ne pourrait pas se
	// connecter. Un écran qui liste des gens qui ne peuvent rien faire fait
	// perdre du temps à chaque fois qu'on le lit.
	role, err := s.accounts.RoleOf(ctx, req.UserID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if role != auth.RoleAdmin {
		return nil, errNotAdmin.WithMeta(map[string]any{"role": role})
	}
	m := &Member{
		UserID: uid, Function: req.Function, Title: strings.TrimSpace(req.Title),
		Scopes: scopes, Status: StatusActive,
	}
	if actor, ok := auth.UserFromContext(ctx); ok {
		if aid, err := primitive.ObjectIDFromHex(actor); err == nil {
			m.CreatedBy = aid
		}
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, passThrough(err)
	}
	out := toResponse(m)
	s.record(ctx, "staff.create", m.ID.Hex(), nil, out)
	return &out, nil
}

// List returns staff records with their account identity attached.
func (s *Service) List(ctx context.Context, function, scope, status string, page httpx.Page) ([]Response, string, error) {
	if function != "" && !slices.Contains(Functions, function) {
		return nil, "", errBadFunction.WithMeta(map[string]any{"expected": Functions})
	}
	if scope != "" && !slices.Contains(Scopes, scope) {
		return nil, "", errBadScope
	}
	cursor := primitive.NilObjectID
	if page.Cursor != "" {
		id, err := primitive.ObjectIDFromHex(page.Cursor)
		if err != nil {
			return nil, "", apperr.Validation("cursor is not a valid id")
		}
		cursor = id
	}
	members, err := s.repo.List(ctx, function, scope, status, cursor, page.Limit)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	out := make([]Response, 0, len(members))
	ids := make([]string, 0, len(members))
	for i := range members {
		out = append(out, toResponse(&members[i]))
		ids = append(ids, members[i].UserID.Hex())
	}
	// AU MIEUX sur l'identité : une liste sans noms reste actionnable par ses
	// identifiants, une liste qui échoue ne l'est pas du tout.
	if s.accounts != nil && len(ids) > 0 {
		if ids, err := s.accounts.Identities(ctx, ids); err == nil {
			for i := range out {
				if id, ok := ids[out[i].UserID]; ok {
					out[i].Name, out[i].Phone, out[i].Email = id.Name, id.Phone, id.Email
					out[i].AccountStatus = id.Status
				}
			}
		}
	}
	next := ""
	if len(members) == page.Limit && len(out) > 0 {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// Update changes a staff record.
func (s *Service) Update(ctx context.Context, id string, req UpdateRequest) (*Response, error) {
	if err := s.requireDirection(ctx); err != nil {
		return nil, err
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errNotFound
	}
	before, err := s.repo.ByID(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if before == nil {
		return nil, errNotFound
	}
	set := bson.M{}
	if req.Function != nil {
		if !slices.Contains(Functions, *req.Function) {
			return nil, errBadFunction.WithMeta(map[string]any{"expected": Functions})
		}
		set["function"] = *req.Function
	}
	if req.Title != nil {
		set["title"] = strings.TrimSpace(*req.Title)
	}
	if req.Scopes != nil {
		scopes, err := normaliseScopes(*req.Scopes)
		if err != nil {
			return nil, err
		}
		set["scopes"] = scopes
	}
	if req.Status != nil {
		set["status"] = *req.Status
	}
	if len(set) == 0 {
		out := toResponse(before)
		return &out, nil
	}
	after, err := s.repo.Update(ctx, oid, set)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if after == nil {
		return nil, errNotFound
	}
	out := toResponse(after)
	s.record(ctx, "staff.update", id, toResponse(before), out)
	return &out, nil
}

// Remove deletes a staff record — the account survives.
func (s *Service) Remove(ctx context.Context, id string) error {
	if err := s.requireDirection(ctx); err != nil {
		return err
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return errNotFound
	}
	before, err := s.repo.ByID(ctx, oid)
	if err != nil {
		return apperr.Internal(err)
	}
	if before == nil {
		return errNotFound
	}
	ok, err := s.repo.Delete(ctx, oid)
	if err != nil {
		return apperr.Internal(err)
	}
	if !ok {
		return errNotFound
	}
	s.record(ctx, "staff.remove", id, toResponse(before), nil)
	return nil
}

// ScopesOf rend les portées à inscrire dans le jeton d'un compte.
//
// ⚠️ Appelée au moment de la CONNEXION. Un compte sans fiche de staff rend
// une liste vide, ce qui veut dire « aucune restriction » — c'est ce qui fait
// que tous les administrateurs existants gardent l'accès qu'ils avaient.
//
// Une fiche SUSPENDUE rend elle aussi la liste vide, et c'est délibéré : ce
// n'est pas ce champ qui ferme la porte, c'est le statut du COMPTE. Confondre
// les deux ferait qu'une fiche suspendue transforme quelqu'un en
// administrateur sans limites, ce qui est exactement l'inverse de l'intention
// — d'où la vérification explicite ci-dessous.
func (s *Service) ScopesOf(ctx context.Context, userID string) ([]string, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, nil
	}
	m, err := s.repo.ByUserID(ctx, uid)
	if err != nil {
		return nil, err
	}
	return scopesFor(m), nil
}

// scopesFor porte la RÈGLE, séparée de la lecture en base pour être testable.
//
// ⚠️ Une fonction pure plutôt qu'un bloc au milieu de `ScopesOf` : un test
// écrit sur des claims fabriqués à la main aurait continué de passer si cette
// décision changeait. Une garantie qui ne touche pas le code qu'elle prétend
// couvrir n'en est pas une.
//
// Les trois cas rendent une liste VIDE, qui n'accorde rien — pas de valeur
// sentinelle, pas de cas particulier : c'est ce que la convention littérale
// achète.
func scopesFor(m *Member) []string {
	if m == nil {
		return nil // pas de fiche de staff : n'administre rien
	}
	if m.Status == StatusSuspended {
		return nil // habilitations retirées
	}
	return m.Scopes
}

// normaliseScopes valide et déduplique.
func normaliseScopes(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, errNoScope
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		if !slices.Contains(Scopes, s) {
			return nil, errBadScope.WithMeta(map[string]any{"got": s, "expected": Scopes})
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, errNoScope
	}
	// ⚠️ AUCUNE réduction : les trois portées se stockent telles quelles. Les
	// remplacer par une valeur « toutes » aurait fait deux façons d'écrire la
	// même chose — et le jour où une quatrième verticale apparaît, cette
	// valeur aurait continué de désigner les trois d'hier, sans que rien ne le
	// signale. Écrire la liste, c'est écrire une date.
	return out, nil
}

func passThrough(err error) error {
	if apperr.From(err).Code != "internal" {
		return err
	}
	return apperr.Internal(err)
}

// EnsureMember crée la fiche de staff si elle n'existe pas, et la rend telle
// qu'elle est sinon.
//
// ⚠️ IDEMPOTENT, et il ne RÉÉCRIT PAS une fiche existante. Un provisionnement
// rejoué ne doit pas écraser un périmètre que l'exploitation a restreint à la
// main — c'est la même règle que pour le mot de passe de l'administrateur, et
// pour la même raison : un seed relancé ne doit rien reprendre à personne.
func (s *Service) EnsureMember(ctx context.Context, userID, function, title string, scopes []string) (*Response, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("user_id is not a valid id")
	}
	existing, err := s.repo.ByUserID(ctx, uid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if existing != nil {
		out := toResponse(existing)
		return &out, nil
	}
	return s.Create(ctx, CreateRequest{
		UserID: userID, Function: function, Title: title, Scopes: scopes,
	})
}
