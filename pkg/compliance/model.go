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

	// DocCriminalRecord : l'extrait de CASIER JUDICIAIRE — la PERSONNE.
	//
	// ⚠️ IL PORTE UNE DATE D'EXPIRATION OBLIGATOIRE, et c'est le seul type dans
	// ce cas. Un casier est un INSTANTANÉ : il dit ce qu'on savait le jour où il
	// a été délivré, et rien du lendemain. Admis « sans date », il vaudrait pour
	// toujours — et un extrait de 2019 marqué « valide » rendrait le contrôle
	// entier décoratif, précisément sur la pièce qui existe pour protéger les
	// passagers. Voir `expiryRequired`.
	//
	// ⚠️ ET IL EST DEMANDÉ À TOUT LE MONDE, indépendamment du véhicule —
	// contrairement au permis. Transporter des gens ou leur argent est ce qui
	// crée l'obligation, pas le fait de conduire : un livreur à vélo manipule
	// des espèces et entre dans des cours d'immeubles.
	DocCriminalRecord = "criminal_record"

	// DocSelfie : une photo du VISAGE, prise à l'inscription — la PERSONNE.
	//
	// ⚠️ ELLE N'EST PAS LA PHOTO DE PROFIL. Celle-ci est choisie par la
	// personne et sert à la reconnaître ; le selfie est une pièce qu'un humain
	// COMPARE à la pièce d'identité. Les confondre aurait fait d'un avatar
	// choisi librement une preuve d'identité — et retiré au contrôle la seule
	// chose qu'il vérifie ici : que celui qui s'inscrit est bien celui dont on
	// lit la carte.
	DocSelfie = "selfie"
)

// Les PHOTOS DU VÉHICULE — trois angles, trois pièces.
//
// ⚠️ TROIS TYPES ET NON UN SEUL « photos », parce qu'une pièce porte UNE image :
// le dépôt remplace celle du même type (voir `UpsertDocument`), et un type
// unique aurait fait que la photo de l'arrière écrase celle de l'avant. La
// personne aurait déposé trois fois et il n'en serait resté qu'une — sans
// message, sans trace.
//
// ⚠️ ET C'EST AUSSI CE QUI PERMET DE DIRE CE QUI MANQUE. « Il manque une photo »
// n'indique pas laquelle reprendre ; « il manque la photo de la plaque » se
// règle en trente secondes. L'agrégation rend les types manquants un par un.
//
// ⚠️ TROIS ANGLES COMMUNS À UNE VOITURE ET À UNE MOTO. L'intérieur n'a pas de
// sens sur une moto, et une liste qui dépendrait du genre de véhicule aurait
// fait deux règles de conformité à tenir d'accord.
const (
	// DocVehicleFront : l'avant, PLAQUE LISIBLE. C'est la seule photo qui
	// rattache le véhicule à sa carte grise.
	DocVehicleFront = "vehicle_front"
	// DocVehicleRear : l'arrière.
	DocVehicleRear = "vehicle_rear"
	// DocVehicleSide : de côté — l'état général, ce qu'un assureur regarde.
	DocVehicleSide = "vehicle_side"
)

// PersonKinds et VehicleKinds sont les pièces attendues de chaque côté.
//
// Exportées parce que l'AGRÉGATION s'en sert pour dire ce qui manque, et que
// deux listes — une pour vérifier, une pour réclamer — divergeraient.
var (
	// PersonKinds : ce qu'on attend de TOUTE personne, quel que soit son
	// véhicule. Le PERMIS n'en fait pas partie — voir `PersonKindsFor`.
	//
	// ⚠️ LE CASIER ET LE SELFIE Y SONT, eux : ils ne dépendent pas de ce qu'on
	// conduit. Un livreur à vélo manipule des espèces et entre chez des gens.
	PersonKinds = []string{DocIDCard, DocSelfie, DocCriminalRecord}
	// VehicleKinds : ce qu'on attend d'un véhicule MOTORISÉ — ses papiers, et
	// ses trois photos.
	VehicleKinds = []string{
		DocRegistration, DocInsurance, DocInspection,
		DocVehicleFront, DocVehicleRear, DocVehicleSide,
	}
	// VehiclePhotoKinds : les photos seules, pour les écrans qui les montrent
	// ensemble. Déclarées ICI et non recopiées : une seconde liste finirait par
	// oublier un angle.
	VehiclePhotoKinds = []string{DocVehicleFront, DocVehicleRear, DocVehicleSide}
)

// expiryRequired : les types dont une date d'expiration est OBLIGATOIRE.
//
// ⚠️ UN SEUL POUR L'INSTANT, et c'est délibéré. Une assurance et un contrôle
// technique ont toujours une fin eux aussi, mais les rendre obligatoires
// aujourd'hui refuserait des dépôts que la plateforme accepte depuis des mois —
// on ne resserre pas une règle existante dans le même geste qu'on en ajoute une.
// Le casier, lui, est nouveau : sa règle naît stricte, ce qui est le seul moment
// où ça ne casse rien.
var expiryRequired = map[string]bool{DocCriminalRecord: true}

// NeedsExpiry dit si ce type exige une date d'expiration.
func NeedsExpiry(kind string) bool { return expiryRequired[kind] }

// docOwner dit à quoi chaque type de pièce se rattache.
//
// C'est une TABLE et non une convention : le service la consulte pour refuser
// une carte grise sans véhicule, ou un permis attaché à une moto. Sans elle,
// la cohérence reposerait sur la discipline de chaque appelant.
var docOwner = map[string]string{
	DocLicence:        ownerPerson,
	DocIDCard:         ownerPerson,
	DocCriminalRecord: ownerPerson,
	DocSelfie:         ownerPerson,
	DocRegistration:   ownerVehicle,
	DocInsurance:      ownerVehicle,
	DocInspection:     ownerVehicle,
	DocVehicleFront:   ownerVehicle,
	DocVehicleRear:    ownerVehicle,
	DocVehicleSide:    ownerVehicle,
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
	// ⚠️ NOMMÉ, et non un « champ requis » générique : la personne doit savoir
	// QUOI chercher sur son extrait — la date de délivrance, dont la validité
	// court. Un refus muet la fait redéposer la même image.
	errDocNeedsExpiry = apperr.Validation(
		"this document is a snapshot: expires_at is required")
)

// Document is one compliance paper.
//
// ⚠️ EXACTEMENT UN propriétaire : OwnerID pour une pièce de la personne,
// VehicleID pour une pièce du véhicule. Les deux renseignés, ou aucun, est un
// document qu'aucune vue ne saurait classer.
type Document struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
	// OwnerID est QUI RÉPOND DE CETTE PIÈCE : un chauffeur — livreur ou
	// conducteur VTC — ou, depuis le 9 octobre 2026, une FLOTTE PRIVÉE (voir
	// `FleetID`). Le champ s'appelait `agent_id` : la même mécanique sert deux
	// métiers, et un nom qui désigne l'un obligerait l'autre à s'écrire sous un
	// mot qui n'est pas le sien.
	OwnerID primitive.ObjectID `bson:"owner_id"`
	// FleetID marque une pièce déposée par le PROPRIÉTAIRE du véhicule, et non
	// par celui qui le conduit. `OwnerID` vaut alors la flotte.
	//
	// ⚠️⚠️ IL EXISTE PARCE QU'ON RÉCLAMAIT À UN CHAUFFEUR LA CARTE GRISE D'UNE
	// VOITURE QUI N'EST PAS LA SIENNE. Les papiers d'un véhicule de société
	// sont chez son propriétaire : le chauffeur restait « en défaut » pour une
	// pièce qu'il ne pouvait pas fournir, et la relance automatique le lui
	// redisait tous les trois jours. Un rappel qu'on ne peut pas satisfaire
	// n'est pas un rappel, c'est du bruit — et il use la seule catégorie de
	// messages qu'on ne laisse pas couper.
	//
	// ⚠️ ET LA PIÈCE SUIT LE VÉHICULE, PAS LE CONDUCTEUR : quand la voiture
	// change de chauffeur, son assurance reste valable. Portée par le
	// conducteur, elle aurait été à redéposer à chaque rotation.
	FleetID   *primitive.ObjectID `bson:"fleet_id,omitempty"`
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

// isVehicleKind dit si cette pièce est celle d'un VÉHICULE.
//
// ⚠️ Distinct de `CheckOwner`, qui vérifie la COHÉRENCE d'un dépôt (« un permis
// ne porte pas de véhicule »). Ici la question est de savoir QUI a le droit de
// déposer : une société dépose les papiers de ses voitures, jamais la pièce
// d'identité de quelqu'un. Les deux règles se ressemblent et ne disent pas la
// même chose — les confondre aurait rangé le permis d'un chauffeur dans le
// dossier d'une entreprise.
func isVehicleKind(kind string) bool { return docOwner[kind] == ownerVehicle }

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
