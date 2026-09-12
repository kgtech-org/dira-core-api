package fleet

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/phone"
)

// Auditor enregistre les gestes sensibles. Facultatif.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Service applies the fleet rules.
type Service struct {
	repo  *Repository
	audit Auditor
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) SetAuditor(a Auditor) { s.audit = a }

func (s *Service) record(ctx context.Context, action, id string, before, after any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "fleet", id, before, after)
}

// CreateRequest is what the back-office sends to register a fleet.
type CreateRequest struct {
	Name         string `json:"name" validate:"required,min=2,max=120"`
	OwnerUserID  string `json:"owner_user_id" validate:"omitempty,len=24,hexadecimal"`
	ContactName  string `json:"contact_name" validate:"omitempty,max=120"`
	ContactPhone string `json:"contact_phone" validate:"omitempty,e164"`
	ContactEmail string `json:"contact_email" validate:"omitempty,email"`
	ContractRef  string `json:"contract_ref" validate:"omitempty,max=120"`
	// CommissionBp : absent = la flotte suit le taux de la plateforme. Voir le
	// commentaire du champ dans `model.go` — zéro est une gratuité négociée,
	// pas une absence.
	CommissionBp *int   `json:"commission_bp"`
	Notes        string `json:"notes" validate:"omitempty,max=1000"`
}

// UpdateRequest carries only what changes. Chaque champ est un POINTEUR :
// sans cela, « ne pas toucher au taux » et « remettre le taux à zéro »
// s'écrivent pareil, et une modification du nom effacerait la commission.
type UpdateRequest struct {
	Name         *string `json:"name" validate:"omitempty,min=2,max=120"`
	OwnerUserID  *string `json:"owner_user_id" validate:"omitempty,len=24,hexadecimal"`
	ContactName  *string `json:"contact_name" validate:"omitempty,max=120"`
	ContactPhone *string `json:"contact_phone" validate:"omitempty,e164"`
	ContactEmail *string `json:"contact_email" validate:"omitempty,email"`
	ContractRef  *string `json:"contract_ref" validate:"omitempty,max=120"`
	CommissionBp *int    `json:"commission_bp"`
	Status       *string `json:"status" validate:"omitempty,oneof=active suspended"`
	Notes        *string `json:"notes" validate:"omitempty,max=1000"`
}

// Response is a fleet as the console sees it.
type Response struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OwnerUserID  string `json:"owner_user_id,omitempty"`
	ContactName  string `json:"contact_name,omitempty"`
	ContactPhone string `json:"contact_phone,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
	ContractRef  string `json:"contract_ref,omitempty"`
	CommissionBp *int   `json:"commission_bp"`
	Status       string `json:"status"`
	Notes        string `json:"notes,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func toResponse(f *Fleet) Response {
	out := Response{
		ID: f.ID.Hex(), Name: f.Name,
		ContactName: f.ContactName, ContactPhone: f.ContactPhone, ContactEmail: f.ContactEmail,
		ContractRef: f.ContractRef, CommissionBp: f.CommissionBp,
		Status: f.Status, Notes: f.Notes,
		CreatedAt: f.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: f.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if f.OwnerUserID != nil {
		out.OwnerUserID = f.OwnerUserID.Hex()
	}
	return out
}

// Create registers a fleet.
func (s *Service) Create(ctx context.Context, actorID string, req CreateRequest) (*Response, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errEmptyName
	}
	if err := checkCommission(req.CommissionBp); err != nil {
		return nil, err
	}
	f := &Fleet{
		Name: name, ContactName: strings.TrimSpace(req.ContactName),
		ContactPhone: canonPhone(req.ContactPhone), ContactEmail: strings.ToLower(strings.TrimSpace(req.ContactEmail)),
		ContractRef: strings.TrimSpace(req.ContractRef), CommissionBp: req.CommissionBp,
		Status: StatusActive, Notes: req.Notes,
	}
	if req.OwnerUserID != "" {
		oid, err := primitive.ObjectIDFromHex(req.OwnerUserID)
		if err != nil {
			return nil, apperr.Validation("owner_user_id is not a valid id")
		}
		f.OwnerUserID = &oid
	}
	if err := s.repo.Create(ctx, f); err != nil {
		return nil, wrap(err)
	}
	out := toResponse(f)
	s.record(ctx, "fleet.create", f.ID.Hex(), nil, out)
	return &out, nil
}

// List returns fleets, newest first.
func (s *Service) List(ctx context.Context, status, q string, page httpx.Page) ([]Response, string, error) {
	if status != "" && status != StatusActive && status != StatusSuspended {
		return nil, "", errBadStatus
	}
	cursor := primitive.NilObjectID
	if page.Cursor != "" {
		id, err := primitive.ObjectIDFromHex(page.Cursor)
		if err != nil {
			return nil, "", apperr.Validation("cursor is not a valid id")
		}
		cursor = id
	}
	fleets, err := s.repo.List(ctx, status, q, cursor, page.Limit)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	out := make([]Response, 0, len(fleets))
	for i := range fleets {
		out = append(out, toResponse(&fleets[i]))
	}
	// Une page incomplète est la DERNIÈRE. Rendre un curseur quand même ferait
	// afficher « charger plus » sur une liste terminée, et le clic rapporterait
	// une page vide — ce que l'utilisateur lit comme une panne.
	next := ""
	if len(fleets) == page.Limit && len(out) > 0 {
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

// Get returns one fleet.
func (s *Service) Get(ctx context.Context, id string) (*Response, error) {
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
	out := toResponse(f)
	return &out, nil
}

// Names resolves fleet names for a vertical decorating its vehicle rows.
//
// ⚠️ Des NOMS, pas les flottes entières. Une verticale affiche « Flotte
// Sodigaz » à côté d'une plaque ; lui rendre le contrat, la commission et le
// téléphone du gérant serait lui confier des données qu'elle n'a aucune raison
// de porter, et qu'elle finirait par recopier.
func (s *Service) Names(ctx context.Context, ids []string) (map[string]string, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, raw := range ids {
		if oid, err := primitive.ObjectIDFromHex(raw); err == nil {
			oids = append(oids, oid)
		}
	}
	fleets, err := s.repo.ByIDs(ctx, oids)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make(map[string]string, len(fleets))
	for id, f := range fleets {
		out[id.Hex()] = f.Name
	}
	return out, nil
}

// Update changes a fleet.
func (s *Service) Update(ctx context.Context, actorID, id string, req UpdateRequest) (*Response, error) {
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
	if err := checkCommission(req.CommissionBp); err != nil {
		return nil, err
	}
	set := bson.M{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errEmptyName
		}
		set["name"] = name
	}
	if req.ContactName != nil {
		set["contact_name"] = strings.TrimSpace(*req.ContactName)
	}
	if req.ContactPhone != nil {
		set["contact_phone"] = canonPhone(*req.ContactPhone)
	}
	if req.ContactEmail != nil {
		set["contact_email"] = strings.ToLower(strings.TrimSpace(*req.ContactEmail))
	}
	if req.ContractRef != nil {
		set["contract_ref"] = strings.TrimSpace(*req.ContractRef)
	}
	if req.CommissionBp != nil {
		set["commission_bp"] = *req.CommissionBp
	}
	if req.Status != nil {
		set["status"] = *req.Status
	}
	if req.Notes != nil {
		set["notes"] = *req.Notes
	}
	if req.OwnerUserID != nil {
		owner, err := primitive.ObjectIDFromHex(*req.OwnerUserID)
		if err != nil {
			return nil, apperr.Validation("owner_user_id is not a valid id")
		}
		set["owner_user_id"] = owner
	}
	if len(set) == 0 {
		out := toResponse(before)
		return &out, nil
	}
	after, err := s.repo.Update(ctx, oid, set)
	if err != nil {
		return nil, wrap(err)
	}
	if after == nil {
		return nil, errNotFound
	}
	out := toResponse(after)
	s.record(ctx, "fleet.update", id, toResponse(before), out)
	return &out, nil
}

// checkCommission borne le taux.
//
// ⚠️ Vérifié ICI et pas seulement par le validateur de la requête : le service
// est aussi appelé par le provisionnement, qui ne passe pas par le décodage
// HTTP. Une règle qui ne vit que dans une balise de struct est une règle que
// le second appelant ignore.
func checkCommission(bp *int) error {
	if bp == nil {
		return nil
	}
	if *bp < 0 || *bp > 10000 {
		return errBadCommis
	}
	return nil
}

func wrap(err error) error {
	if apperr.From(err).Code != "internal" {
		return err
	}
	return apperr.Internal(err)
}

// canonPhone met un numéro de contact sous sa forme canonique. Le tag `e164`
// a déjà validé la valeur ; vide reste vide (le contact est facultatif).
func canonPhone(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	if p, err := phone.Normalize(raw); err == nil {
		return p
	}
	return strings.TrimSpace(raw)
}
