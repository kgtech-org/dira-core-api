package compliance

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// SubmitDocumentRequest is the driver's upload of one compliance paper.
type SubmitDocumentRequest struct {
	Kind    string `json:"kind" validate:"required,oneof=licence id_card registration insurance"`
	FileURL string `json:"file_url" validate:"required,url,max=2048"`
	// VehicleID est REQUIS pour une carte grise ou une assurance, et REFUSÉ
	// pour un permis ou une pièce d'identité. Le type de la pièce décide ; ce
	// n'est pas à l'application de le savoir, mais elle est prévenue.
	VehicleID string `json:"vehicle_id" validate:"omitempty,len=24,hexadecimal"`
	// ExpiresAt : absent = la pièce ne périme pas (une carte grise). Une
	// valeur DÉJÀ passée est refusée : déposer un papier périmé n'est pas une
	// mise en conformité, et l'accepter ferait croire à l'une comme à l'autre.
	ExpiresAt *time.Time `json:"expires_at"`
}

// ReviewDocumentRequest is the administrator's decision.
type ReviewDocumentRequest struct {
	Status string `json:"status" validate:"required,oneof=valid rejected"`
	// Reason n'a de sens qu'au refus, et le livreur la lit : « photo floue »
	// lui dit quoi refaire, un refus muet le laisse redéposer la même image.
	Reason string `json:"reason" validate:"omitempty,max=300"`
}

// DocumentResponse is one compliance paper as the apps see it.
type DocumentResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	VehicleID string `json:"vehicle_id,omitempty"`
	FileURL   string `json:"file_url,omitempty"`
	// State est l'état EFFECTIF, calculé à la lecture : `pending`, `valid`,
	// `expiring`, `expired` ou `rejected`. C'est lui qu'il faut afficher.
	State          string     `json:"state"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RejectedReason string     `json:"rejected_reason,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ComplianceResponse is a driver's whole compliance state.
type ComplianceResponse struct {
	Documents []DocumentResponse `json:"documents"`
	// Missing sont les pièces JAMAIS déposées. Rendues à part parce qu'une
	// liste vide ne se distingue pas d'une liste complète : sans ce champ,
	// un livreur qui n'a rien déposé verrait un écran sans défaut.
	//
	// Les pièces de VÉHICULE ne peuvent manquer que si le livreur a un
	// véhicule : on ne réclame pas une assurance à quelqu'un qui n'a rien
	// déclaré.
	Missing []string `json:"missing,omitempty"`
	// Compliant dit si TOUT est en règle — pièces personnelles ET pièces de
	// chaque véhicule déclaré.
	//
	// ⚠️ Informatif : rien n'est bloqué côté serveur. C'est l'exploitation qui
	// suspend, depuis la liste de conformité de la console.
	Compliant bool `json:"compliant"`
}

// Fleet is the VERTICALE, qui possède ses chauffeurs et leurs véhicules.
//
// Déclarée côté consommateur, et en identifiants NUS : cette bibliothèque n'a
// pas à connaître le type `Agent` de la livraison ni le type `Driver` des
// courses. Elle a besoin de quatre réponses, pas de leurs structures.
type Fleet interface {
	// DriverOf rend le chauffeur derrière un compte, ou "" s'il n'y en a pas.
	DriverOf(ctx context.Context, userID string) (driverID string, err error)
	// DriverExists dit si cet identifiant est bien l'un des nôtres.
	DriverExists(ctx context.Context, driverID string) (bool, error)
	// VehicleOwner rend le propriétaire d'un véhicule, ou "".
	//
	// ⚠️ C'est ce qui empêche de déposer une assurance sur le véhicule d'un
	// autre — ce qui le rendrait conforme sans que son propriétaire le sache.
	VehicleOwner(ctx context.Context, vehicleID string) (driverID string, err error)
	// VehiclesOf liste les véhicules d'un chauffeur.
	//
	// Ils décident de ce qui MANQUE : on ne réclame pas une assurance à
	// quelqu'un qui n'a déclaré aucun véhicule.
	VehiclesOf(ctx context.Context, driverID string) (vehicleIDs []string, err error)
}

// Auditor enregistre les gestes sensibles. Facultatif.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Store is the persistence this service needs.
//
// Déclarée côté consommateur, et satisfaite par `*Repository` : c'est ce qui
// permet de vérifier les RÈGLES — qui possède quoi, quand une pièce cesse de
// couvrir — sans base de données. Une règle qu'on ne peut tester qu'avec Mongo
// finit par n'être testée qu'en production.
type Store interface {
	UpsertDocument(ctx context.Context, d *Document) (*Document, error)
	DocumentsByOwner(ctx context.Context, ownerID primitive.ObjectID) ([]Document, error)
	DocumentByID(ctx context.Context, id primitive.ObjectID) (*Document, error)
	ReviewDocument(ctx context.Context, id, reviewer primitive.ObjectID, status, reason string) (*Document, error)
	PendingOrExpiredDocuments(ctx context.Context, now time.Time, limit int) ([]Document, error)
}

// Service applies the compliance rules of ONE vertical.
type Service struct {
	repo  Store
	fleet Fleet
	audit Auditor
}

// NewService builds the compliance service.
func NewService(repo Store, fleet Fleet) *Service {
	return &Service{repo: repo, fleet: fleet}
}

// SetAuditor branche la trace d'audit (câblage).
func (s *Service) SetAuditor(a Auditor) { s.audit = a }

func (s *Service) record(ctx context.Context, action, id string, before, after any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "compliance_document", id, before, after)
}

var (
	errDriverNotFound  = apperr.NotFound("driver_not_found", "driver not found")
	errVehicleNotFound = apperr.NotFound("vehicle_not_found", "vehicle not found")
)

// SubmitDocument records a driver's paper, replacing the previous one of the
// same kind.
//
// ⚠️ Un dépôt repasse TOUJOURS la pièce en `pending`, même si l'ancienne était
// validée : une image nouvelle est une image que personne n'a regardée.
func (s *Service) SubmitDocument(ctx context.Context, userID string, req SubmitDocumentRequest) (*DocumentResponse, error) {
	driverID, err := s.fleet.DriverOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if driverID == "" {
		return nil, errDriverNotFound
	}
	ownerOID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		return nil, errDriverNotFound
	}
	var vehicleOID *primitive.ObjectID
	if req.VehicleID != "" {
		vid, err := primitive.ObjectIDFromHex(req.VehicleID)
		if err != nil {
			return nil, errVehicleNotFound
		}
		owner, err := s.fleet.VehicleOwner(ctx, req.VehicleID)
		if err != nil {
			return nil, err
		}
		// Le véhicule doit être LE SIEN : déposer une assurance sur la moto
		// d'un autre la rendrait conforme sans que son propriétaire le sache.
		if owner != driverID {
			return nil, errVehicleNotFound
		}
		vehicleOID = &vid
	}
	if err := CheckOwner(req.Kind, vehicleOID); err != nil {
		return nil, err
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now().UTC()) {
		return nil, apperr.Validation("expires_at is already past: this document does not bring the driver back in order")
	}
	doc, err := s.repo.UpsertDocument(ctx, &Document{
		OwnerID: ownerOID, VehicleID: vehicleOID, Kind: req.Kind,
		FileURL: req.FileURL, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, "compliance.document.submit", doc.ID.Hex(), nil,
		map[string]any{"kind": doc.Kind, "owner_id": driverID})
	out := toDocumentResponse(doc, time.Now().UTC())
	return &out, nil
}

// Compliance rend l'état de conformité d'un livreur.
func (s *Service) Compliance(ctx context.Context, userID string) (*ComplianceResponse, error) {
	driverID, err := s.fleet.DriverOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if driverID == "" {
		return nil, errDriverNotFound
	}
	return s.complianceOf(ctx, driverID)
}

// ComplianceOfDriver rend l'état de conformité d'un chauffeur DÉSIGNÉ (admin).
func (s *Service) ComplianceOfDriver(ctx context.Context, driverID string) (*ComplianceResponse, error) {
	if _, err := primitive.ObjectIDFromHex(driverID); err != nil {
		return nil, errDriverNotFound
	}
	ok, err := s.fleet.DriverExists(ctx, driverID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errDriverNotFound
	}
	return s.complianceOf(ctx, driverID)
}

func (s *Service) complianceOf(ctx context.Context, driverID string) (*ComplianceResponse, error) {
	oid, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		return nil, errDriverNotFound
	}
	docs, err := s.repo.DocumentsByOwner(ctx, oid)
	if err != nil {
		return nil, err
	}
	vehicles, err := s.fleet.VehiclesOf(ctx, driverID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := &ComplianceResponse{Documents: make([]DocumentResponse, 0, len(docs)), Compliant: true}

	// Ce qui est COUVERT, par (type, véhicule). La clé porte le véhicule :
	// une assurance couvre UNE moto, pas le parc.
	covered := make(map[string]bool, len(docs))
	for i := range docs {
		d := &docs[i]
		out.Documents = append(out.Documents, toDocumentResponse(d, now))
		if d.Compliant(now) {
			covered[docKey(d.Kind, d.VehicleID)] = true
		}
	}
	for _, kind := range PersonKinds {
		if !covered[docKey(kind, nil)] {
			out.Missing = append(out.Missing, kind)
			out.Compliant = false
		}
	}
	for _, raw := range vehicles {
		vid, err := primitive.ObjectIDFromHex(raw)
		if err != nil {
			continue
		}
		for _, kind := range VehicleKinds {
			if !covered[docKey(kind, &vid)] {
				// Le véhicule est nommé : « assurance manquante » sur un parc
				// de deux motos ne dit pas laquelle rouler.
				out.Missing = append(out.Missing, kind+":"+raw)
				out.Compliant = false
			}
		}
	}
	return out, nil
}

// ReviewDocument records an administrator's decision on one paper.
func (s *Service) ReviewDocument(ctx context.Context, adminID, documentID string, req ReviewDocumentRequest) (*DocumentResponse, error) {
	did, err := primitive.ObjectIDFromHex(documentID)
	if err != nil {
		return nil, errDocumentNotFound
	}
	aid, err := primitive.ObjectIDFromHex(adminID)
	if err != nil {
		return nil, apperr.Validation("invalid admin id").WithCause(err)
	}
	if req.Status == DocRejected && req.Reason == "" {
		// Un refus muet laisse le livreur redéposer la même image. Le motif
		// est ce qui lui dit quoi refaire.
		return nil, apperr.Validation("a rejection needs a reason: the driver reads it")
	}
	doc, err := s.repo.ReviewDocument(ctx, did, aid, req.Status, req.Reason)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errDocumentNotFound
	}
	s.record(ctx, "delivery.document.review", documentID, nil,
		map[string]any{"status": req.Status, "reason": req.Reason})
	out := toDocumentResponse(doc, time.Now().UTC())
	return &out, nil
}

// ComplianceQueue lists the papers an operator must act on.
//
// ⚠️ C'est la CONTREPARTIE du choix de ne rien bloquer automatiquement. Sans
// cette file, « l'exploitation suspend à la main » veut dire « personne ne
// suspend » : un livreur dont l'assurance a expiré continue d'être appelé, et
// rien ne le signale nulle part.
func (s *Service) ComplianceQueue(ctx context.Context, limit int) ([]ComplianceQueueItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	now := time.Now().UTC()
	docs, err := s.repo.PendingOrExpiredDocuments(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ComplianceQueueItem, 0, len(docs))
	for i := range docs {
		d := &docs[i]
		out = append(out, ComplianceQueueItem{
			DocumentResponse: toDocumentResponse(d, now),
			DriverID:         d.OwnerID.Hex(),
		})
	}
	return out, nil
}

// ComplianceQueueItem is one paper to look at, with who it belongs to.
//
// ⚠️ Le CHAUFFEUR, pas son compte : cette bibliothèque ne connaît pas les
// comptes. La verticale — qui les connaît — y attache le nom avant de rendre
// la file à la console, comme elle le fait pour ses autres listes.
type ComplianceQueueItem struct {
	DocumentResponse
	DriverID string `json:"driver_id"`
}

func docKey(kind string, vehicleID *primitive.ObjectID) string {
	if vehicleID == nil {
		return kind
	}
	return kind + ":" + vehicleID.Hex()
}

func toDocumentResponse(d *Document, now time.Time) DocumentResponse {
	out := DocumentResponse{
		ID: d.ID.Hex(), Kind: d.Kind, FileURL: d.FileURL,
		State: d.State(now), ExpiresAt: d.ExpiresAt,
		RejectedReason: d.RejectedReason, UpdatedAt: d.UpdatedAt,
	}
	if d.VehicleID != nil {
		out.VehicleID = d.VehicleID.Hex()
	}
	return out
}
