package user

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// registerAgent ouvre un compte d'agent et rend son identifiant.
func registerAgent(t *testing.T, svc *Service, phone, app string) string {
	t.Helper()
	resp, err := svc.Register(context.Background(), RegisterRequest{
		Phone: phone, Name: "Kossi", Password: "s3cret-password",
		Role: auth.RoleDriver, App: app,
	})
	require.NoError(t, err)
	return resp.User.ID
}

// ⚠️ LE MÉTIER SE DÉCLARE À L'INSCRIPTION, et c'est le seul moment où « le
// premier qui déclare » est légitime : un compte qui vient de naître n'a aucun
// profil, donc aucune réalité à contredire.
func TestANewCourierAccountBelongsToTheCourierApp(t *testing.T) {
	svc, repo, _ := newUserTestService()
	id := registerAgent(t, svc, "+22890000010", AgentAppCourier)

	u := repo.byPhoneForTest(t, "+22890000010")
	assert.Equal(t, AgentAppCourier, u.AgentApp)
	assert.Equal(t, auth.RoleDriver, u.Role, "un livreur a le rôle `driver` : la frontière nouvelle est le métier, pas le rôle")
	require.NotEmpty(t, id)
}

// ⚠️ SEUL LE RÔLE `driver` PORTE UNE APPARTENANCE. La poser sur un client
// l'enfermerait dehors de sa propre application à la connexion suivante.
func TestOnlyAgentAccountsGetAnAgentApp(t *testing.T) {
	assert.Equal(t, "", newAgentApp(auth.RoleClient, AgentAppCourier))
	assert.Equal(t, "", newAgentApp(auth.RoleMerchant, AgentAppDriver))
	assert.Equal(t, "", newAgentApp(auth.RoleDriver, "console"))
	assert.Equal(t, "", newAgentApp(auth.RoleDriver, ""))
	assert.Equal(t, AgentAppDriver, newAgentApp(auth.RoleDriver, AgentAppDriver))
}

// La réclamation est IDEMPOTENTE : les verticales l'appellent à chaque
// ouverture d'application, et réclamer ce qui est acquis n'écrit rien.
func TestClaimingTwiceIsFree(t *testing.T) {
	svc, _, _ := newUserTestService()
	id := registerAgent(t, svc, "+22890000011", "")

	got, err := svc.ClaimAgentApp(context.Background(), id, AgentAppCourier, false)
	require.NoError(t, err)
	assert.Equal(t, AgentAppCourier, got)

	got, err = svc.ClaimAgentApp(context.Background(), id, AgentAppCourier, true)
	require.NoError(t, err)
	assert.Equal(t, AgentAppCourier, got)
}

// ⚠️ LA PREMIÈRE VERTICALE GAGNE, LA SECONDE EST REFUSÉE. C'est ce qui empêche
// une personne d'être chauffeuse ET livreuse — et le refus NOMME l'application
// à laquelle le compte appartient, pour que l'écran puisse le dire.
func TestTheSecondVerticalIsRefusedAndToldWhere(t *testing.T) {
	svc, repo, _ := newUserTestService()
	id := registerAgent(t, svc, "+22890000012", "")

	_, err := svc.ClaimAgentApp(context.Background(), id, AgentAppCourier, true)
	require.NoError(t, err)

	held, err := svc.ClaimAgentApp(context.Background(), id, AgentAppDriver, false)
	require.Error(t, err)
	assert.Equal(t, "wrong_app", apperr.From(err).Code)
	assert.Equal(t, AgentAppCourier, apperr.From(err).Meta["reason"],
		"`reason` doit nommer l'application à ouvrir — c'est la seule clé que la réponse porte")
	assert.Equal(t, AgentAppCourier, held, "le refus rend aussi l'appartenance en vigueur, pour les journaux")

	// ⚠️ ET RIEN N'A ÉTÉ ÉCRASÉ. Une réclamation refusée qui poserait quand
	// même son mot ferait basculer le compte à chaque ouverture de l'une ou
	// l'autre application.
	assert.Equal(t, AgentAppCourier, repo.byPhoneForTest(t, "+22890000012").AgentApp)
}

// ⚠️ L'ADMINISTRATION PASSE PARTOUT, et cette porte-ci compte autant que celle
// de la connexion : un opérateur ouvre l'application d'un chauffeur pour
// reproduire ce qu'on lui décrit. Lui POSER une appartenance le rendrait
// ensuite aveugle à l'autre application.
func TestAnAdminNeverGetsAnAgentApp(t *testing.T) {
	svc, repo, _ := newUserTestService()
	resp, err := svc.register(context.Background(), RegisterRequest{
		Phone: "+22890000013", Name: "Exploitation", Password: "s3cret-password",
	}, auth.RoleAdmin)
	require.NoError(t, err)

	got, err := svc.ClaimAgentApp(context.Background(), resp.User.ID, AgentAppDriver, false)
	require.NoError(t, err)
	assert.Equal(t, "", got)
	assert.Equal(t, "", repo.byPhoneForTest(t, "+22890000013").AgentApp)
}

// Un mot qui n'est pas une application d'agent est refusé en 422 : `client`
// n'est pas un métier, et l'accepter poserait sur un compte une appartenance
// que plus rien ne saurait satisfaire.
func TestOnlyTheTwoAgentAppsCanBeClaimed(t *testing.T) {
	svc, _, _ := newUserTestService()
	id := registerAgent(t, svc, "+22890000014", "")

	_, err := svc.ClaimAgentApp(context.Background(), id, "client", false)
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code)
}

// ⚠️ UN COMPTE CLIENT NE PEUT PAS ÊTRE RÉCLAMÉ, et le garde-fou n'est pas
// théorique : poser une appartenance d'agent sur un client lui refuserait
// ensuite l'entrée de sa PROPRE application, à la connexion, définitivement.
func TestAClientAccountCanNeverBeClaimedByAVertical(t *testing.T) {
	svc, repo, _ := newUserTestService()
	resp, err := svc.Register(context.Background(), RegisterRequest{
		Phone: "+22890000019", Name: "Ama", Password: "s3cret-password",
	})
	require.NoError(t, err)

	_, err = svc.ClaimAgentApp(context.Background(), resp.User.ID, AgentAppCourier, false)
	require.Error(t, err)
	assert.Equal(t, "wrong_app", apperr.From(err).Code)
	assert.Equal(t, "client", apperr.From(err).Meta["reason"])
	assert.Equal(t, "", repo.byPhoneForTest(t, "+22890000019").AgentApp,
		"rien n'est écrit : ce client doit pouvoir se reconnecter chez lui")

	// Et il entre toujours dans son application.
	_, err = svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000019", Password: "s3cret-password", App: "client",
	})
	require.NoError(t, err)
}

func TestClaimingForAnUnknownAccountIsNotFound(t *testing.T) {
	svc, _, _ := newUserTestService()
	_, err := svc.ClaimAgentApp(context.Background(), "68d1a0a0a0a0a0a0a0a0a0a0", AgentAppDriver, false)
	assertUserCode(t, err, "user_not_found")
}

// ⚠️ LA SORTIE DU SUPPORT. Une personne change de métier ; sans ce geste, le
// support ouvrira un second compte à la même personne — avec un second
// téléphone, un second portefeuille et un second historique, c'est-à-dire
// exactement le désordre que la règle existe pour éviter.
func TestSupportCanReleaseAnAgentAppSoAPersonCanChangeTrade(t *testing.T) {
	svc, repo, _ := newUserTestService()
	id := registerAgent(t, svc, "+22890000015", AgentAppCourier)

	released, err := svc.ReleaseAgentApp(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, AgentAppCourier, released,
		"le geste NOMME ce qu'il a libéré : l'opérateur doit pouvoir le dire à la personne")
	assert.Equal(t, "", repo.byPhoneForTest(t, "+22890000015").AgentApp)

	// Libérée, l'appartenance se réclame de nouveau — dans l'AUTRE
	// application. C'est tout l'objet du geste : la personne a changé de
	// métier, pas de compte.
	got, err := svc.ClaimAgentApp(context.Background(), id, AgentAppDriver, false)
	require.NoError(t, err)
	assert.Equal(t, AgentAppDriver, got)

	// Et elle entre maintenant par la porte des courses, qui lui était fermée.
	_, err = svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000015", Password: "s3cret-password", App: AgentAppDriver,
	})
	require.NoError(t, err)
}

// Libérer ce qui est déjà libre RÉUSSIT sans rien écrire : un geste
// d'administration qui échoue parce qu'il a déjà été fait pousse à s'acharner,
// et c'est comme cela qu'on finit par supprimer un compte.
func TestReleasingNothingIsStillASuccess(t *testing.T) {
	svc, _, _ := newUserTestService()
	id := registerAgent(t, svc, "+22890000018", "")

	released, err := svc.ReleaseAgentApp(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "", released)
}

func TestReleasingAnUnknownAccountIsNotFound(t *testing.T) {
	svc, _, _ := newUserTestService()
	_, err := svc.ReleaseAgentApp(context.Background(), "68d1a0a0a0a0a0a0a0a0a0a0")
	assertUserCode(t, err, "user_not_found")
}

// ⚠️ PERMISSIF TANT QU'IL N'Y A PAS D'APPARTENANCE — exactement comme `app` et
// `device_id` avant elle. C'est ce qui laisse les 24 livreurs et les 33
// chauffeurs déjà en place se ranger tout seuls à leur première ouverture, au
// lieu d'être enfermés dehors par une règle qu'ils n'ont pas eu l'occasion de
// satisfaire.
func TestAnAccountWithNoMembershipEntersEitherAgentApp(t *testing.T) {
	u := &User{Role: auth.RoleDriver}
	assert.NoError(t, allowedInApp(AgentAppDriver, u))
	assert.NoError(t, allowedInApp(AgentAppCourier, u))
	assert.NoError(t, allowedInApp("", u))
}

// ⚠️ `courier` EST ACCEPTÉ PARTOUT OÙ `driver` L'EST. Un livreur a le rôle
// `driver` : la frontière nouvelle n'est pas le rôle, c'est le métier.
func TestTheCourierAppIsAnAgentAppLikeTheDriverApp(t *testing.T) {
	assert.Equal(t, auth.RoleDriver, appRole[AgentAppCourier])
	assert.NoError(t, allowedIn(AgentAppCourier, auth.RoleDriver))
	assert.NoError(t, allowedIn(AgentAppCourier, auth.RoleAdmin))
	// Et elle reste fermée aux autres rôles, comme l'application des courses.
	require.Error(t, allowedIn(AgentAppCourier, auth.RoleClient))
	require.Error(t, allowedIn(AgentAppCourier, auth.RoleMerchant))
}

// LE REFUS QUE LE PROPRIÉTAIRE A DEMANDÉ : un chauffeur qui ouvre la mauvaise
// application reçoit un refus, au lieu de rien.
func TestACourierIsRefusedAtTheDoorOfTheRideApp(t *testing.T) {
	u := &User{Role: auth.RoleDriver, AgentApp: AgentAppCourier}
	err := allowedInApp(AgentAppDriver, u)
	require.Error(t, err)
	assert.Equal(t, "wrong_app", apperr.From(err).Code)
	assert.Equal(t, AgentAppCourier, apperr.From(err).Meta["reason"])

	assert.NoError(t, allowedInApp(AgentAppCourier, u), "et il entre chez lui")
}

func TestADriverIsRefusedAtTheDoorOfTheDeliveryApp(t *testing.T) {
	u := &User{Role: auth.RoleDriver, AgentApp: AgentAppDriver}
	err := allowedInApp(AgentAppCourier, u)
	require.Error(t, err)
	assert.Equal(t, AgentAppDriver, apperr.From(err).Meta["reason"])
}

// ⚠️ L'APPARTENANCE EST PLUS PRÉCISE QUE LE RÔLE, et c'est pourquoi elle est
// consultée d'abord. Un livreur qui ouvre l'application CLIENT doit lire
// « ouvrez Dira Livreur » ; le rôle seul l'aurait envoyé vers l'application
// des chauffeurs — la mauvaise des deux, et un aller-retour de plus.
func TestTheRefusalNamesTheTradeAndNotJustTheRole(t *testing.T) {
	u := &User{Role: auth.RoleDriver, AgentApp: AgentAppCourier}
	err := allowedInApp("client", u)
	require.Error(t, err)
	assert.Equal(t, AgentAppCourier, apperr.From(err).Meta["reason"])
}

// ⚠️ L'ADMINISTRATION PASSE PARTOUT, jusque dans les deux applications
// d'agent : sans cela, le support serait aveugle à ce qu'on lui décrit.
func TestAnAdminEntersBothAgentApps(t *testing.T) {
	u := &User{Role: auth.RoleAdmin, AgentApp: AgentAppCourier}
	assert.NoError(t, allowedInApp(AgentAppDriver, u))
	assert.NoError(t, allowedInApp(AgentAppCourier, u))
	assert.NoError(t, allowedInApp("client", u))
}

// La connexion refuse pour de bon, et pas seulement la fonction qui juge :
// c'est à cette porte que tout se joue, AVANT qu'aucun profil ne puisse
// naître dans la verticale.
func TestLoginRefusesTheOtherAgentApp(t *testing.T) {
	svc, _, _ := newUserTestService()
	registerAgent(t, svc, "+22890000016", AgentAppCourier)

	_, err := svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000016", Password: "s3cret-password", App: AgentAppDriver,
	})
	require.Error(t, err)
	assert.Equal(t, "wrong_app", apperr.From(err).Code)
	assert.Equal(t, AgentAppCourier, apperr.From(err).Meta["reason"])

	// Chez lui, il entre.
	_, err = svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000016", Password: "s3cret-password", App: AgentAppCourier,
	})
	require.NoError(t, err)
}

// ⚠️ UNE APPLICATION PAS ENCORE MISE À JOUR CONTINUE DE FONCTIONNER. Les deux
// applications envoient `app: "driver"` aujourd'hui ; celle de la livraison ne
// deviendra `courier` qu'à sa prochaine publication, et le magasin met des
// semaines à valider. Un compte sans appartenance entre donc avec l'un ou
// l'autre mot, comme avant.
func TestLoginStaysPermissiveWhileTheAppsHaveNotShipped(t *testing.T) {
	svc, _, _ := newUserTestService()
	registerAgent(t, svc, "+22890000017", "")

	for _, app := range []string{"", AgentAppDriver, AgentAppCourier} {
		_, err := svc.Login(context.Background(), LoginRequest{
			Phone: "+22890000017", Password: "s3cret-password", App: app,
		})
		assert.NoError(t, err, "app=%q doit entrer tant qu'aucune appartenance n'est connue", app)
	}
}

// byPhoneForTest lit le compte TEL QU'IL EST EN BASE, et non tel que la
// réponse le montre : ce qui se vérifie ici est ce qui a été ÉCRIT.
func (f *fakeUserRepo) byPhoneForTest(t *testing.T, phone string) *User {
	t.Helper()
	u, err := f.FindByPhone(context.Background(), phone)
	require.NoError(t, err)
	require.NotNil(t, u, "compte %s introuvable", phone)
	return u
}
