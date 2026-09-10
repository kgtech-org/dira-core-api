// Package fleet porte les FLOTTES PRIVÉES : les sociétés qui possèdent des
// véhicules conduits par d'autres.
//
// ⚠️ AU SOCLE, ET PAS DANS UNE VERTICALE. C'est le critère de partage de cette
// plateforme appliqué à la lettre : les deux métiers ont besoin de la même
// DONNÉE, pas seulement de la même règle. Une société de transport de Lomé
// possède des motos qui livrent et des voitures qui font des courses ; loger
// la flotte dans la livraison aurait obligé les courses à lire la base de leur
// voisine pour afficher un nom de propriétaire — et l'inverse aurait été aussi
// vrai.
//
// Ce que le socle NE tient PAS : les véhicules eux-mêmes. Ils restent dans
// leur verticale, avec un `fleet_id` qui pointe ici. Une moto de livraison et
// une berline VTC n'ont ni les mêmes champs, ni les mêmes règles, ni les mêmes
// lecteurs — les réunir aurait fait une table où la moitié des colonnes est
// vide selon la ligne.
package fleet

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Collection is the MongoDB collection backing private fleets.
const Collection = "fleets"

// États d'une flotte.
//
// ⚠️ Suspendre une flotte ne suspend PAS ses conducteurs. Ce sont deux
// décisions distinctes : une société dont le contrat expire cesse de percevoir
// ses commissions, mais ses chauffeurs restent des personnes habilitées qui
// peuvent travailler pour leur propre compte. Cascader la suspension aurait
// mis dix personnes à l'arrêt pour un papier non signé.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Fleet is a company owning vehicles driven by others.
type Fleet struct {
	ID   primitive.ObjectID `bson:"_id,omitempty"`
	Name string             `bson:"name"`
	// OwnerUserID est le COMPTE qui gère la flotte — celui qui se connecte,
	// voit ses véhicules et ses conducteurs.
	//
	// Facultatif : une flotte peut exister avant que son gérant n'ait un
	// compte. L'exiger aurait obligé l'exploitation à créer un compte fantôme
	// pour enregistrer un contrat signé sur papier.
	OwnerUserID *primitive.ObjectID `bson:"owner_user_id,omitempty"`
	// Contact est ce qu'on compose quand un véhicule de la flotte pose
	// problème à deux heures du matin. Un identifiant de compte ne se compose
	// pas.
	ContactName  string `bson:"contact_name,omitempty"`
	ContactPhone string `bson:"contact_phone,omitempty"`
	ContactEmail string `bson:"contact_email,omitempty"`
	// ContractRef est la référence du contrat signé, telle qu'elle figure sur
	// le papier. Champ libre : c'est une référence externe, et lui imposer un
	// format ferait refuser le contrat le jour où le notaire change de
	// numérotation.
	ContractRef string `bson:"contract_ref,omitempty"`
	// CommissionBp est la part que Dira prélève sur les courses des véhicules
	// de cette flotte, en dix-millièmes.
	//
	// ⚠️ nil = la flotte suit le taux de la PLATEFORME. Zéro et « non
	// renseigné » ne sont pas la même chose : zéro est une gratuité négociée,
	// et un pointeur seul sait dire la différence. Un `int` nu aurait fait
	// travailler gratuitement toute flotte enregistrée sans taux.
	CommissionBp *int   `bson:"commission_bp,omitempty"`
	Status       string `bson:"status"`
	Notes        string `bson:"notes,omitempty"`
	CreatedAt    time.Time `bson:"created_at"`
	UpdatedAt    time.Time `bson:"updated_at"`
}

var (
	errNotFound   = apperr.NotFound("fleet_not_found", "fleet not found")
	errNameTaken  = apperr.Conflict("fleet_name_taken", "a fleet already carries this name")
	errBadCommis  = apperr.Validation("commission_bp must be between 0 and 10000")
	errBadStatus  = apperr.Validation("status must be active or suspended")
	errEmptyName  = apperr.Validation("name is required")
)
