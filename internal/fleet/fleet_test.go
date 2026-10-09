package fleet

import (
	"github.com/go-chi/chi/v5"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

func bp(v int) *int { return &v }

// ⚠️ LE TEST QUI COMPTE. `nil` et `0` ne veulent pas dire la même chose : nil
// signifie « cette flotte suit le taux de la plateforme », zéro signifie « une
// gratuité a été négociée ». Les confondre ferait travailler gratuitement
// toute flotte enregistrée sans taux — une perte sèche que rien ne signale,
// parce qu'un zéro est une valeur parfaitement valide.
func TestCommissionDistinguishesUnsetFromZero(t *testing.T) {
	assert.NoError(t, checkCommission(nil), "non renseigné : la flotte suivra le taux de la plateforme")
	assert.NoError(t, checkCommission(bp(0)), "zéro est une gratuité NÉGOCIÉE, pas une absence")
	assert.NoError(t, checkCommission(bp(10000)))

	// Et le pointeur le PORTE jusqu'à la réponse : un `int` nu aurait rendu
	// les deux cas identiques à la lecture.
	unset := toResponse(&Fleet{})
	assert.Nil(t, unset.CommissionBp, "non renseigné doit rester distinguable à la lecture")
	free := toResponse(&Fleet{CommissionBp: bp(0)})
	require.NotNil(t, free.CommissionBp)
	assert.Equal(t, 0, *free.CommissionBp)
}

// Un taux hors bornes est un taux qui ne veut rien dire. 12 000 dix-millièmes
// prélèveraient 120 % d'une course.
func TestCommissionIsBounded(t *testing.T) {
	for _, v := range []int{-1, 10001, 999999} {
		err := checkCommission(bp(v))
		require.Error(t, err, "%d devrait être refusé", v)
		assert.Equal(t, "validation_failed", apperr.From(err).Code)
	}
}

// ⚠️ La recherche est une entrée NON FIABLE. Sans échappement, un nom
// contenant une parenthèse fait échouer la requête, et `.*` parcourt la
// collection entière.
func TestSearchIsEscaped(t *testing.T) {
	assert.Equal(t, `Sodigaz \(Lomé\)`, regexEscape("Sodigaz (Lomé)"))
	assert.Equal(t, `\.\*`, regexEscape(".*"))
	assert.Equal(t, `Transports Kodjo`, regexEscape("Transports Kodjo"), "un nom ordinaire n'est pas altéré")
}

// La réponse porte l'identifiant du gérant quand il existe, et RIEN quand il
// n'existe pas — une chaîne vide dans un champ d'identifiant se lit comme un
// compte introuvable, alors que la flotte n'en a simplement pas.
func TestOwnerIsOmittedWhenAbsent(t *testing.T) {
	assert.Empty(t, toResponse(&Fleet{}).OwnerUserID)
}

// --- LA SURFACE PARTENAIRE ----------------------------------------------

// ⚠️⚠️ LE TEST QUI PORTE TOUT L'ISOLEMENT : LA SURFACE PARTENAIRE N'A AUCUN
// IDENTIFIANT DANS SES CHEMINS.
//
// C'est structurel, pas documentaire. Un `GET /partner/fleets/{id}` aurait
// suffi à un partenaire curieux pour lire le parc d'un concurrent en changeant
// un chiffre dans l'URL — et un garde oublié une seule fois, sur une seule
// route, suffit. En n'offrant aucune route paramétrée, il n'y a rien à essayer.
func TestThePartnerSurfaceHasNoIdentifierToTamperWith(t *testing.T) {
	routes := mountedPartnerRoutes(t)
	require.NotEmpty(t, routes, "la surface partenaire existe")
	for _, r := range routes {
		assert.NotContains(t, r, "{", "aucun paramètre de chemin : %s", r)
	}
	assert.Equal(t, []string{"GET /partner/me"}, routes)
}

// mountedPartnerRoutes liste les routes `/partner/...` servies par ce module.
func mountedPartnerRoutes(t *testing.T) []string {
	t.Helper()
	r := chi.NewRouter()
	NewHandler(&Service{}).Mount(r, func(next http.Handler) http.Handler { return next })
	var out []string
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/partner/") {
			out = append(out, method+" "+strings.TrimSuffix(route, "/"))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// --- L'ACCÈS DU PARTENAIRE À SA CONSOLE (access.go) ---

// ⚠️ ON NE PROVISIONNE PAS UN ACCÈS SANS SAVOIR À QUI. Le téléphone identifie
// le compte sur la plateforme, l'adresse est ce avec quoi on se connecte : sans
// l'adresse, on aurait créé un compte incapable d'ouvrir sa propre console.
func TestOpeningAnAccessNeedsBothContactFields(t *testing.T) {
	_, err := accessPlan(&Fleet{Name: "Sodigaz", ContactPhone: "+22890000000"})
	require.Error(t, err, "sans adresse")
	_, err = accessPlan(&Fleet{Name: "Sodigaz", ContactEmail: "kodjo@sodigaz.tg"})
	require.Error(t, err, "sans téléphone")

	p, err := accessPlan(&Fleet{Name: "Sodigaz",
		ContactPhone: " +22890000000 ", ContactEmail: " Kodjo@Sodigaz.TG "})
	require.NoError(t, err)
	assert.Equal(t, "+22890000000", p.Phone)
	// L'adresse est RANGÉE en minuscules : c'est l'identifiant de connexion, et
	// « Kodjo@ » puis « kodjo@ » auraient fait deux comptes pour une personne.
	assert.Equal(t, "kodjo@sodigaz.tg", p.Email)
	// Le nom de la SOCIÉTÉ à défaut du gérant : un compte sans nom s'affiche
	// vide partout où on le croise.
	assert.Equal(t, "Sodigaz", p.Name)
}

// ⚠️⚠️ LE PIÈGE QUI COMPTE : LE GÉRANT D'UNE FLOTTE EST SOUVENT DÉJÀ UN
// PASSAGER DIRA. `EnsureAccount` rend alors son compte `client` tel quel. Le
// rattacher aurait affiché « accès ouvert » sur la fiche pendant que la
// connexion lui répond `403 wrong_app` — et personne n'aurait su pourquoi.
func TestAnAccountOfAnotherRoleIsNotLinkedToTheFleet(t *testing.T) {
	require.NoError(t, ownerRoleOK(auth.RolePartner))
	for _, role := range []string{auth.RoleClient, auth.RoleDriver, auth.RoleMerchant, auth.RoleAdmin, ""} {
		err := ownerRoleOK(role)
		require.Error(t, err, "rôle %q", role)
		assert.Contains(t, err.Error(), "phone_belongs_to_another_role")
	}
}

// --- LE PERSONNEL D'UNE FLOTTE (members.go) ---

// ⚠️⚠️ LA RÈGLE QUI TIENT TOUTES LES AUTRES : `owner` NE SE DONNE PAS. Le
// laisser passer aurait permis de fabriquer un second propriétaire — et le
// propriétaire est précisément celui qui décide qui entre. Un `manager` promu
// propriétaire pourrait mettre le signataire du contrat dehors.
func TestOwnerIsSignedNotGranted(t *testing.T) {
	require.NoError(t, CheckFleetRole(RoleManager))
	require.NoError(t, CheckFleetRole(RoleViewer))
	for _, role := range []string{RoleOwner, "admin", "", "propriétaire"} {
		err := CheckFleetRole(role)
		require.Error(t, err, "rôle %q", role)
		assert.Contains(t, err.Error(), "fleet_role_unknown")
	}
}

// ⚠️ UN COMPTABLE N'A AUCUNE RAISON DE POUVOIR COUPER LE TRAVAIL DE QUELQU'UN.
// `CanWrite` est la seule fonction qui en décide, et elle vit au socle : trois
// modules qui trancheraient chacun « est-ce que `viewer` peut ? » finiraient par
// répondre trois choses, et c'est celui qu'on regarde le moins qui laisserait
// passer.
func TestOnlyTheOwnerAndTheManagerMayAct(t *testing.T) {
	assert.True(t, CanWrite(RoleOwner))
	assert.True(t, CanWrite(RoleManager))
	assert.False(t, CanWrite(RoleViewer), "un lecteur LIT")
	// ⚠️ ET UN RÔLE INCONNU NE PEUT RIEN : si une verticale recevait un rôle
	// qu'elle ne connaît pas (jeton d'une version plus ancienne, données
	// importées), le défaut doit être « regarder », jamais « agir ».
	assert.False(t, CanWrite(""))
	assert.False(t, CanWrite("invention_future"))
}

// ⚠️ LE PROPRIÉTAIRE D'ABORD : s'il figurait AUSSI dans `members` — données
// anciennes, import — c'est son titre de propriétaire qui doit gagner. Sinon un
// signataire se retrouverait `viewer` sur sa propre société, et ne pourrait plus
// faire entrer personne.
func TestTheOwnerKeepsHisTitleEvenIfAlsoListed(t *testing.T) {
	owner := primitive.NewObjectID()
	f := &Fleet{OwnerUserID: &owner, Members: []Member{{UserID: owner, Role: RoleViewer}}}
	assert.Equal(t, RoleOwner, roleIn(f, owner))
}

func TestRoleInNamesEachMember(t *testing.T) {
	owner, manager, viewer, stranger := primitive.NewObjectID(), primitive.NewObjectID(),
		primitive.NewObjectID(), primitive.NewObjectID()
	f := &Fleet{OwnerUserID: &owner, Members: []Member{
		{UserID: manager, Role: RoleManager},
		{UserID: viewer, Role: RoleViewer},
	}}
	assert.Equal(t, RoleOwner, roleIn(f, owner))
	assert.Equal(t, RoleManager, roleIn(f, manager))
	assert.Equal(t, RoleViewer, roleIn(f, viewer))
	// ⚠️ UN INCONNU N'A PAS DE RÔLE — chaîne VIDE, et non un rôle par défaut :
	// `CanWrite("")` est faux, donc l'absence de rôle ne donne aucun pouvoir.
	assert.Empty(t, roleIn(f, stranger))
}

// ⚠️ UNE SEULE LECTURE POUR LES DEUX CAS (`$or`) : propriétaire OU membre. Si
// la clause des membres disparaissait, un répartiteur parfaitement légitime
// recevrait « votre contrat n'est pas enregistré » — un refus qui dit le
// contraire de la vérité.
func TestTheFleetIsFoundByOwnerOrByStaff(t *testing.T) {
	uid := primitive.NewObjectID()
	f := memberFilter(uid)
	clauses, ok := f["$or"].([]bson.M)
	require.True(t, ok, "le filtre doit chercher les deux")
	require.Len(t, clauses, 2)
	assert.Equal(t, uid, clauses[0]["owner_user_id"])
	assert.Equal(t, uid, clauses[1]["members.user_id"])
}
