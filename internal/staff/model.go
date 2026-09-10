// Package staff porte les EMPLOYÉS DE DIRA : leur fonction, et le périmètre
// de plateforme qu'ils administrent.
//
// ⚠️ CE N'EST PAS UN SECOND ANNUAIRE. Un membre du staff EST un compte de
// `internal/user`, avec le rôle `admin` ; ce paquet ne fait qu'y attacher ce
// que le compte ne sait pas dire — pour quelle équipe cette personne
// travaille, et jusqu'où va son autorité. Dupliquer le nom, le téléphone et le
// mot de passe ici aurait donné deux vérités sur la même personne, et la
// suspension d'un côté n'aurait rien fermé de l'autre.
//
// ⚠️ ET CE N'EST PAS DÉCORATIF. Le périmètre est écrit dans le JETON et
// vérifié par `middleware.RequireScope` dans chaque verticale. Un « périmètre »
// affiché sur une fiche mais qu'aucune route ne contrôle donne à
// l'exploitation la certitude d'avoir restreint quelqu'un qui ne l'est pas —
// pire que de n'avoir rien restreint, parce que plus personne ne surveille.
package staff

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// Collection is the MongoDB collection backing staff records.
const Collection = "staff_members"

// Fonctions. Ce que la personne FAIT, pour que la console sache à qui
// s'adresser — distinct du PÉRIMÈTRE, qui dit jusqu'où elle peut aller.
//
// ⚠️ Les deux ne se confondent pas : un responsable financier couvre les deux
// métiers, un chargé de support peut n'en couvrir qu'un. Les fondre en une
// seule liste aurait obligé à inventer « support-vtc » et « support-food »,
// puis « support-vtc-et-food », et la liste aurait grandi à chaque embauche.
const (
	FunctionOps     = "ops"     // exploitation : flotte, courses, livraisons
	FunctionSupport = "support" // tickets, litiges, signalements
	FunctionFinance = "finance" // portefeuilles, paiements, réconciliation
	FunctionContent = "content" // catalogue, promotions, bannières, feed
	FunctionAdmin   = "admin"   // direction : tout, y compris le staff
)

// Functions est la liste EXPORTÉE des fonctions connues, pour que la
// validation et la console lisent la même — deux listes divergeraient.
var Functions = []string{FunctionOps, FunctionSupport, FunctionFinance, FunctionContent, FunctionAdmin}

// Scopes reprend les portées de `pkg/auth`. Elles y sont définies parce que
// c'est le jeton qui les porte et le middleware qui les vérifie ; les
// redéclarer ici ferait deux vocabulaires pour une seule notion.
var Scopes = []string{auth.ScopeCore, auth.ScopeFood, auth.ScopeVTC}

const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// Member attaches a function and a scope to an existing admin account.
type Member struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
	// UserID est le compte. UNIQUE : une personne n'a qu'une fiche de staff,
	// sans quoi deux fiches contradictoires décideraient de ses droits selon
	// celle qu'on lit en premier.
	UserID   primitive.ObjectID `bson:"user_id"`
	Function string             `bson:"function"`
	// Scopes : les verticales que cette personne administre.
	//
	// ⚠️ VIDE = TOUTES, comme dans le jeton. La règle est la même des deux
	// côtés à dessein : deux conventions inverses — vide=tout ici, vide=rien
	// là-bas — auraient produit un membre affiché « accès complet » et refusé
	// partout, sans que rien ne l'explique.
	Scopes []string `bson:"scopes,omitempty"`
	// Title est l'intitulé humain, celui de la fiche de paie. Libre : « Chargé
	// de support niveau 2 » n'entre dans aucune énumération, et forcer un
	// choix aurait rangé tout le monde sous « autre ».
	Title     string             `bson:"title,omitempty"`
	Status    string             `bson:"status"`
	CreatedBy primitive.ObjectID `bson:"created_by,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// EffectiveScopes rend les portées à appliquer, en traduisant « vide » en
// « toutes ».
//
// ⚠️ Une méthode plutôt qu'une convention rappelée en commentaire : le jeton,
// la console et la fiche lisent tous ce champ, et l'un des trois aurait fini
// par oublier la règle.
func (m *Member) EffectiveScopes() []string {
	if len(m.Scopes) == 0 {
		return append([]string(nil), Scopes...)
	}
	return append([]string(nil), m.Scopes...)
}

// Unrestricted dit si ce membre couvre toute la plateforme.
func (m *Member) Unrestricted() bool { return len(m.Scopes) == 0 }

var (
	errNotFound     = apperr.NotFound("staff_not_found", "staff member not found")
	errAlreadyStaff = apperr.Conflict("already_staff", "this account already has a staff record")
	errBadFunction  = apperr.Validation("unknown staff function")
	errBadScope     = apperr.Validation("unknown scope: expected core, food or vtc")
	errNotAdmin     = apperr.Validation("a staff member must be an account with the admin role")
)
