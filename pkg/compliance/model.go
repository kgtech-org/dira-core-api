// Package compliance porte les PIÈCES d'un chauffeur et de ses véhicules.
//
// ⚠️ UNE BIBLIOTHÈQUE, PAS UN SERVICE. Les deux verticales ont besoin des mêmes
// RÈGLES — quelles pièces sont attendues, laquelle se rattache à quoi, quand
// une pièce cesse de couvrir — mais pas des mêmes DONNÉES : un livreur et un
// chauffeur VTC sont deux populations disjointes, et leurs pièces ne se lisent
// jamais ensemble.
//
// Chaque verticale câble donc sa collection et répond, par `Fleet`, à ce que
// cette bibliothèque ne peut pas savoir : qui est chauffeur, et à qui
// appartient un véhicule.
package compliance

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Pièces attendues. Quatre, et le TYPE dit à quoi elles se rattachent.
//
// ⚠️ DÉCISION PRODUIT : les pièces de la PERSONNE et celles du VÉHICULE sont
// séparées, parce que c'est la réalité du terrain. Un livreur avec deux motos
// a deux assurances ; vendre une moto ne doit pas invalider son permis, et une
// moto non assurée doit devenir inutilisable sans mettre son propriétaire à
// l'arrêt — il bascule sur l'autre.
//
// Tout porter sur le livreur aurait été plus simple à afficher, et n'aurait
// pas su dire QUELLE moto est assurée.
const (
	DocLicence      = "licence"      // permis de conduire — la PERSONNE
	DocIDCard       = "id_card"      // pièce d'identité — la PERSONNE
	DocRegistration = "registration" // carte grise — le VÉHICULE
	DocInsurance    = "insurance"    // assurance — le VÉHICULE
	// DocInspection : le contrôle technique.
	//
	// ⚠️ Une pièce de VÉHICULE comme les deux précédentes, et non un champ de
	// date posé sur le véhicule. Elle a une image, une validité, et un humain
	// qui la regarde — c'est-à-dire exactement le cycle que ce paquet tient
	// déjà. Une date nue sur le véhicule aurait fait un second mécanisme
	// d'expiration, avec sa propre file d'attente à surveiller.
	DocInspection = "inspection"
)

// PersonKinds et VehicleKinds sont les pièces attendues de chaque côté.
//
// Exportées parce que l'AGRÉGATION s'en sert pour dire ce qui manque, et que
// deux listes — une pour vérifier, une pour réclamer — divergeraient.
var (
	PersonKinds  = []string{DocLicence, DocIDCard}
	VehicleKinds = []string{DocRegistration, DocInsurance, DocInspection}
)

// docOwner dit à quoi chaque type de pièce se rattache.
//
// C'est une TABLE et non une convention : le service la consulte pour refuser
// une carte grise sans véhicule, ou un permis attaché à une moto. Sans elle,
// la cohérence reposerait sur la discipline de chaque appelant.
var docOwner = map[string]string{
	DocLicence:      ownerPerson,
	DocIDCard:       ownerPerson,
	DocRegistration: ownerVehicle,
	DocInsurance:    ownerVehicle,
	DocInspection:   ownerVehicle,
}

const (
	ownerPerson  = "person"
	ownerVehicle = "vehicle"
)

// États d'une pièce.
//
// `pending`, `valid` et `rejected` sont STOCKÉS — ils disent ce qu'un humain a
// décidé. `expiring` et `expired` sont CALCULÉS à la lecture, depuis la date
// d'expiration : les stocker demanderait une tâche de fond pour les faire
// basculer, et une pièce resterait « valide » jusqu'au prochain passage.
const (
	DocPending  = "pending"  // déposée, pas encore regardée
	DocValid    = "valid"    // validée par l'administration
	DocRejected = "rejected" // refusée — illisible, non conforme, périmée à la remise
	DocExpiring = "expiring" // calculé : valide, mais bientôt périmée
	DocExpired  = "expired"  // calculé : la date est passée
)

// ExpiryWarningDays est le préavis. Quinze jours laissent le temps de passer
// chez l'assureur sans transformer l'alerte en bruit de fond.
//
// ⚠️ Réglage produit, à revoir avec l'équipe.
const ExpiryWarningDays = 15

var (
	errUnknownDocKind  = apperr.Validation("unknown document kind")
	errDocNeedsVehicle = apperr.Validation(
		"this document belongs to a vehicle: vehicle_id is required")
	errDocRefusesVehicle = apperr.Validation(
		"this document belongs to the driver, not to a vehicle: remove vehicle_id")
	errDocumentNotFound = apperr.NotFound("document_not_found", "document not found")
)

// Document is one compliance paper.
//
// ⚠️ EXACTEMENT UN propriétaire : OwnerID pour une pièce de la personne,
// VehicleID pour une pièce du véhicule. Les deux renseignés, ou aucun, est un
// document qu'aucune vue ne saurait classer.
type Document struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
	// OwnerID est le CHAUFFEUR — livreur ou conducteur VTC. Le champ s'appelait
	// `agent_id` : la même mécanique sert deux métiers, et un nom qui désigne
	// l'un obligerait l'autre à s'écrire sous un mot qui n'est pas le sien.
	OwnerID   primitive.ObjectID  `bson:"owner_id"`
	VehicleID *primitive.ObjectID `bson:"vehicle_id,omitempty"`
	Kind      string              `bson:"kind"`
	// FileURL est la pièce elle-même, déposée par le module d'envoi de
	// fichiers. Le document ne stocke pas l'image : il la référence.
	FileURL string `bson:"file_url"`
	// ExpiresAt : nil = la pièce NE PÉRIME PAS. Une carte grise n'a pas de
	// date, un permis en a une. Traiter l'absence comme « expirée » mettrait
	// tout le monde en défaut ; la traiter comme « valide pour toujours » est
	// ce que dit le document lui-même.
	ExpiresAt      *time.Time          `bson:"expires_at,omitempty"`
	Status         string              `bson:"status"`
	ReviewedBy     *primitive.ObjectID `bson:"reviewed_by,omitempty"`
	ReviewedAt     *time.Time          `bson:"reviewed_at,omitempty"`
	RejectedReason string              `bson:"rejected_reason,omitempty"`
	CreatedAt      time.Time           `bson:"created_at"`
	UpdatedAt      time.Time           `bson:"updated_at"`
}

// State rend l'état EFFECTIF de la pièce à l'instant `now`.
//
// Une pièce non validée reste dans son état stocké quoi qu'il arrive : une
// pièce `pending` dont la date est passée n'est pas « expirée », elle n'a
// jamais compté. Les deux se traitent différemment — l'une attend un
// administrateur, l'autre attend le livreur.
func (d *Document) State(now time.Time) string {
	if d.Status != DocValid {
		return d.Status
	}
	if d.ExpiresAt == nil {
		return DocValid
	}
	switch {
	case !d.ExpiresAt.After(now):
		return DocExpired
	case d.ExpiresAt.Before(now.AddDate(0, 0, ExpiryWarningDays)):
		return DocExpiring
	default:
		return DocValid
	}
}

// Compliant dit si la pièce couvre réellement quelque chose aujourd'hui.
//
// `expiring` est COMPLIANT : la pièce est encore valable, le préavis n'est
// qu'un rappel. La traiter comme un défaut mettrait un livreur à l'arrêt deux
// semaines avant l'échéance.
func (d *Document) Compliant(now time.Time) bool {
	st := d.State(now)
	return st == DocValid || st == DocExpiring
}

// CheckOwner vérifie que la pièce est rattachée à ce qu'elle doit l'être.
func CheckOwner(kind string, vehicleID *primitive.ObjectID) error {
	owner, ok := docOwner[kind]
	if !ok {
		return errUnknownDocKind.WithMeta(map[string]any{"kind": kind})
	}
	if owner == ownerVehicle && vehicleID == nil {
		return errDocNeedsVehicle.WithMeta(map[string]any{"kind": kind})
	}
	if owner == ownerPerson && vehicleID != nil {
		return errDocRefusesVehicle.WithMeta(map[string]any{"kind": kind})
	}
	return nil
}
