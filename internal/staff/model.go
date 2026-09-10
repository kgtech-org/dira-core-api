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
	"slices"
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
	// ⚠️ TOUJOURS EXPLICITE, et jamais vide sur une fiche active. Couvrir
	// toute la plateforme s'écrit en énumérant les trois portées, pas en
	// laissant la liste vide : une liste vide n'accorde rien, ici comme dans
	// le jeton, et c'est ce qui permet de suspendre quelqu'un en la vidant.
	//
	// Il n'existe pas de valeur « all » : elle aurait fait deux façons
	// d'exprimer la même chose, et le jour où une quatrième verticale
	// apparaît, « all » aurait continué de désigner les trois d'hier — sans
	// que rien ne le signale.
	Scopes []string `bson:"scopes"`
	// Title est l'intitulé humain, celui de la fiche de paie. Libre : « Chargé
	// de support niveau 2 » n'entre dans aucune énumération, et forcer un
	// choix aurait rangé tout le monde sous « autre ».
	Title     string             `bson:"title,omitempty"`
	Status    string             `bson:"status"`
	CreatedBy primitive.ObjectID `bson:"created_by,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at"`
}

// CoversEverything dit si ce membre couvre TOUTES les verticales.
//
// ⚠️ Calculé, jamais stocké. Un drapeau « accès complet » à côté de la liste
// aurait été une seconde vérité sur la même chose : le jour où une verticale
// s'ajoute, le drapeau resterait vrai pour des gens qui ne la couvrent pas.
func (m *Member) CoversEverything() bool {
	for _, want := range Scopes {
		if !slices.Contains(m.Scopes, want) {
			return false
		}
	}
	return len(Scopes) > 0
}

var (
	errNotFound     = apperr.NotFound("staff_not_found", "staff member not found")
	errAlreadyStaff = apperr.Conflict("already_staff", "this account already has a staff record")
	errBadFunction  = apperr.Validation("unknown staff function")
	errBadScope     = apperr.Validation("unknown scope: expected core, food or vtc")
	errNotAdmin     = apperr.Validation("a staff member must be an account with the admin role")
	errNoScope      = apperr.Validation("at least one scope is required: an empty scope list grants nothing")
	errNotDirection = apperr.Forbidden("staff_direction_required",
		"only a staff member with the admin function may change the team")
)
