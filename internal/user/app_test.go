package user

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// ⚠️ CE QUI SE PASSAIT SANS CE CONTRÔLE : un client se connectait dans
// l'application chauffeur. Le mot de passe est bon, le jeton est émis — puis
// chaque écran répond 403, et la personne croit l'application cassée plutôt
// que de comprendre qu'elle s'est trompée d'application.
func TestAClientCannotSignInToTheDriverApp(t *testing.T) {
	err := allowedIn("driver", auth.RoleClient)
	require.Error(t, err)
	assert.Equal(t, "wrong_app", apperr.From(err).Code)
}

func TestADriverCannotSignInToTheClientApp(t *testing.T) {
	require.Error(t, allowedIn("client", auth.RoleDriver))
	require.Error(t, allowedIn("merchant", auth.RoleDriver))
}

// Le refus DIT OÙ ALLER : un refus qui ne nomme pas la bonne application
// n'aide personne, et c'est au support qu'il coûte.
func TestTheRefusalNamesTheRightApp(t *testing.T) {
	err := allowedIn("driver", auth.RoleMerchant)
	require.Error(t, err)
	meta := apperr.From(err).Meta
	assert.Equal(t, "merchant", meta["open_instead"])
	assert.Equal(t, "merchant", meta["account_role"])
	assert.Equal(t, "driver", meta["app"])
}

// Chacun chez soi : la règle ne doit pas refuser ce qui est légitime.
func TestEachRoleSignsInToItsOwnApp(t *testing.T) {
	for app, role := range appRole {
		assert.NoError(t, allowedIn(app, role), "%s doit entrer dans %s", role, app)
	}
}

// ⚠️ APPLICATION ABSENTE = AUCUNE VÉRIFICATION, et c'est le comportement
// d'avant. Une application pas encore mise à jour ne doit pas voir ses
// utilisateurs enfermés dehors du jour au lendemain.
func TestAnAppThatDoesNotSayWhoItIsStillWorks(t *testing.T) {
	for _, role := range []string{auth.RoleClient, auth.RoleDriver, auth.RoleMerchant, auth.RoleAdmin} {
		assert.NoError(t, allowedIn("", role))
	}
}

// ⚠️ L'ADMINISTRATION PASSE PARTOUT. Un opérateur ouvre l'application d'un
// chauffeur pour reproduire ce qu'il décrit ; lui interdire la porte rendrait
// le support aveugle.
func TestAnAdminSignsInAnywhere(t *testing.T) {
	for app := range appRole {
		assert.NoError(t, allowedIn(app, auth.RoleAdmin), "l'administration doit entrer dans %s", app)
	}
}

// Une application inconnue ne bloque personne : refuser sur un mot qu'on ne
// comprend pas enfermerait dehors sur une faute de frappe.
func TestAnUnknownAppNameLetsEveryoneThrough(t *testing.T) {
	assert.NoError(t, allowedIn("kiosque", auth.RoleClient))
}
