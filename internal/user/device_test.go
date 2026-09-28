package user

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/session"
)

// fakeSessions reproduit le registre des appareils en mémoire, et retient ce
// qu'on lui a demandé : c'est ce qui permet de vérifier la RÉPARATION du
// registre au rafraîchissement, qu'aucune réponse HTTP ne montre.
type fakeSessions struct {
	mu       sync.Mutex
	bound    map[string]session.Device
	asserted []string
	released []string
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{bound: map[string]session.Device{}}
}

func (f *fakeSessions) Bind(_ context.Context, userID string, d session.Device) (session.Device, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	previous, had := f.bound[userID]
	f.bound[userID] = d
	return previous, had && previous.ID != d.ID
}

func (f *fakeSessions) Assert(_ context.Context, userID string, d session.Device) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bound[userID] = d
	f.asserted = append(f.asserted, userID)
}

func (f *fakeSessions) Release(_ context.Context, userID, deviceID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cur, ok := f.bound[userID]; ok && cur.ID == deviceID {
		delete(f.bound, userID)
	}
	f.released = append(f.released, userID)
}

func (f *fakeSessions) current(userID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bound[userID].ID
}

// fakeNotifier retient les notifications émises, sans rien envoyer.
type fakeNotifier struct {
	keys []string
	vars []map[string]string
}

func (f *fakeNotifier) Notify(_ context.Context, _, key string, vars, _ map[string]string) {
	f.keys = append(f.keys, key)
	f.vars = append(f.vars, vars)
}

// newDeviceTestService monte le service AVEC le registre des appareils et les
// notifications — le câblage réel du socle.
func newDeviceTestService() (*Service, *fakeSessions, *fakeNotifier) {
	svc, _, _ := newUserTestService()
	sessions := newFakeSessions()
	notifier := &fakeNotifier{}
	svc.SetSessions(sessions)
	svc.SetNotifier(notifier)
	return svc, sessions, notifier
}

// signUpDriver ouvre un compte de chauffeur et rend son identifiant.
func signUpDriver(t *testing.T, svc *Service) string {
	t.Helper()
	req := registerReq()
	req.Role = auth.RoleDriver
	resp, err := svc.Register(context.Background(), req)
	require.NoError(t, err)
	return resp.User.ID
}

func loginDriver(t *testing.T, svc *Service, deviceID, deviceName string) AuthResponse {
	t.Helper()
	resp, err := svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "driver",
		DeviceID: deviceID, DeviceName: deviceName,
	})
	require.NoError(t, err)
	return resp
}

// ⚠️ CE QUI SE PASSAIT SANS CETTE RÈGLE : un chauffeur connecté sur deux
// téléphones poussait DEUX flux de positions pour une seule voiture. Le vivier
// d'appel la voyait à deux endroits, l'appel partait vers le téléphone resté à
// la maison, et la course mourait d'un « personne n'a répondu » que rien
// n'expliquait.
func TestASecondDriverDeviceChasesTheFirst(t *testing.T) {
	svc, sessions, _ := newDeviceTestService()
	userID := signUpDriver(t, svc)

	first := loginDriver(t, svc, "install-A", "Tecno Spark 10")
	require.NotNil(t, first.Session)
	assert.True(t, first.Session.SingleDevice)
	assert.Empty(t, first.Session.SupersededDeviceID, "il n'y avait personne à chasser")

	second := loginDriver(t, svc, "install-B", "Itel A70")
	require.NotNil(t, second.Session)
	assert.Equal(t, "install-A", second.Session.SupersededDeviceID)
	assert.Equal(t, "Tecno Spark 10", second.Session.SupersededDeviceName,
		"le nouveau téléphone doit pouvoir DIRE lequel il a chassé — l'ancien est peut-être éteint")

	assert.Equal(t, "install-B", sessions.current(userID))

	// ⚠️ LE REFUS EST NOMMÉ. Avec `invalid_token`, l'écran affiche « session
	// expirée » : la personne se reconnecte, chasse l'autre téléphone à son
	// tour, et les deux appareils se renvoient la balle indéfiniment.
	_, err := svc.Refresh(context.Background(), first.RefreshToken)
	assertUserCode(t, err, "session_superseded")

	// Et le nouveau, lui, travaille.
	_, err = svc.Refresh(context.Background(), second.RefreshToken)
	require.NoError(t, err)
}

// Le refus DIT OÙ la session est ouverte. Sans ce nom, l'écran ne peut afficher
// qu'« erreur de connexion » — et la personne croit l'application cassée.
func TestTheRefusalNamesTheDeviceThatTookOver(t *testing.T) {
	svc, _, _ := newDeviceTestService()
	signUpDriver(t, svc)
	first := loginDriver(t, svc, "install-A", "Tecno Spark 10")
	loginDriver(t, svc, "install-B", "Itel A70")

	_, err := svc.Refresh(context.Background(), first.RefreshToken)
	require.Error(t, err)
	// ⚠️ `reason` est la SEULE clé de `Meta` qui atteint le réseau, avec
	// `fields` : une spec qui promettrait un autre champ promettrait du vide.
	assert.Equal(t, "Itel A70", apperrReason(t, err))
}

// ⚠️ NE CASSE RIEN POUR LES AUTRES PUBLICS. Un client a le droit d'être sur sa
// tablette ET son téléphone ; l'y interdire aurait déconnecté la moitié des
// clients de la plateforme pour résoudre un problème de chauffeurs.
func TestAClientKeepsEveryDevice(t *testing.T) {
	svc, sessions, _ := newDeviceTestService()
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	first, err := svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "client",
		DeviceID: "tablette", DeviceName: "iPad",
	})
	require.NoError(t, err)
	second, err := svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "client",
		DeviceID: "telephone", DeviceName: "Pixel 7",
	})
	require.NoError(t, err)

	assert.Nil(t, first.Session, "un client n'est borné à aucun appareil")
	assert.Nil(t, second.Session)
	assert.Empty(t, sessions.current(first.User.ID), "rien ne doit être inscrit au registre")

	// LES DEUX continuent de vivre : c'est tout ce qui compte.
	_, err = svc.Refresh(context.Background(), first.RefreshToken)
	require.NoError(t, err)
	_, err = svc.Refresh(context.Background(), second.RefreshToken)
	require.NoError(t, err)
}

// Le jeton d'un client ne nomme aucun appareil : sans cela, le garde de session
// des six services aurait quelque chose à comparer pour lui aussi.
func TestOnlyADriverTokenCarriesItsDevice(t *testing.T) {
	svc, _, _ := newDeviceTestService()
	signUpDriver(t, svc)
	driver := loginDriver(t, svc, "install-A", "Tecno Spark 10")

	manager := auth.NewManager("test-secret", 0, 0)
	claims, err := manager.Verify(driver.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "install-A", claims.Device)

	svc2, _, _ := newDeviceTestService()
	_, err = svc2.Register(context.Background(), registerReq())
	require.NoError(t, err)
	client, err := svc2.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "client",
		DeviceID: "tablette", DeviceName: "iPad",
	})
	require.NoError(t, err)
	claims, err = manager.Verify(client.AccessToken)
	require.NoError(t, err)
	assert.Empty(t, claims.Device)
}

// ⚠️ LA RÉINSTALLATION N'EST PAS UN SECOND APPAREIL, et le support doit pouvoir
// le voir : le même libellé avec un identifiant différent, c'est le même
// téléphone ; deux libellés différents, c'est quelqu'un d'autre. Sans cette
// trace, la seule réponse possible à « est-ce que quelqu'un utilise mon
// compte ? » était « on ne sait pas ».
func TestAReinstallLooksLikeTheSamePhoneInTheRecord(t *testing.T) {
	svc, _, _ := newDeviceTestService()
	userID := signUpDriver(t, svc)
	loginDriver(t, svc, "install-A", "Tecno Spark 10")
	resp := loginDriver(t, svc, "install-A-reinstalled", "Tecno Spark 10")

	require.NotNil(t, resp.Session)
	assert.Equal(t, "Tecno Spark 10", resp.Session.SupersededDeviceName)
	assert.Equal(t, resp.Session.DeviceName, resp.Session.SupersededDeviceName,
		"même libellé : c'est une réinstallation, pas un second téléphone")

	u := loadUser(t, svc, userID)
	require.NotNil(t, u.Device)
	assert.Equal(t, "install-A", u.Device.PreviousID)
	assert.NotNil(t, u.Device.SupersededAt)
}

// Se reconnecter sur le MÊME téléphone ne chasse personne — et ne doit donc
// rien annoncer. Sans cette distinction, l'application afficherait « votre
// session sur X a été fermée » à chaque ouverture, sur un seul appareil.
func TestRelogOnTheSameDeviceAnnouncesNothing(t *testing.T) {
	svc, _, notifier := newDeviceTestService()
	signUpDriver(t, svc)
	loginDriver(t, svc, "install-A", "Tecno Spark 10")
	again := loginDriver(t, svc, "install-A", "Tecno Spark 10")

	require.NotNil(t, again.Session)
	assert.Empty(t, again.Session.SupersededDeviceID)
	assert.Empty(t, notifier.keys, "personne n'a été chassé : il n'y a rien à annoncer")
}

// ⚠️ MAIS LA SESSION PRÉCÉDENTE DU MÊME TÉLÉPHONE EST BIEN REMPLACÉE. Une
// connexion remplace la session, elle ne s'ajoute pas à elle : deux jetons de
// rafraîchissement vivants pour un seul appareil, c'est deux sessions dont une
// que personne ne surveille.
func TestRelogOnTheSameDeviceReplacesTheSession(t *testing.T) {
	svc, _, _ := newDeviceTestService()
	signUpDriver(t, svc)
	first := loginDriver(t, svc, "install-A", "Tecno Spark 10")
	loginDriver(t, svc, "install-A", "Tecno Spark 10")

	_, err := svc.Refresh(context.Background(), first.RefreshToken)
	assertUserCode(t, err, "invalid_token")
}

// ⚠️ UNE APPLICATION PAS ENCORE MISE À JOUR CONTINUE DE FONCTIONNER. Le
// déploiement du serveur précède toujours celui des applications, de plusieurs
// semaines quand un magasin est lent à valider : sans cette tolérance, la
// plateforme aurait enfermé dehors tous ses chauffeurs le jour de la mise en
// service.
func TestAnAppThatSendsNoDeviceIsNotLockedOut(t *testing.T) {
	svc, sessions, _ := newDeviceTestService()
	userID := signUpDriver(t, svc)
	withDevice := loginDriver(t, svc, "install-A", "Tecno Spark 10")

	old, err := svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "driver",
	})
	require.NoError(t, err)
	assert.Nil(t, old.Session, "rien n'a été déclaré : il n'y a pas de session d'appareil à décrire")
	assert.Equal(t, "install-A", sessions.current(userID),
		"⚠️ l'appareil enregistré NE DOIT PAS être effacé par une application muette : "+
			"il serait alors de nouveau chassable par n'importe quoi")

	// ⚠️ ET LES DEUX TRAVAILLENT — C'EST LE PRIX EXACT DE LA COMPATIBILITÉ, et
	// il faut le connaître : pendant la fenêtre où une application déclare son
	// appareil et l'autre pas, la règle ne s'applique pas et les deux sessions
	// vivent. Elle ne commence à mordre que quand les deux applications
	// envoient leur identifiant — c'est ce que les specs demandent aux équipes
	// mobiles, et c'est pour cela que la livraison de ce mécanisme n'est pas
	// terminée sans elles.
	_, err = svc.Refresh(context.Background(), old.RefreshToken)
	require.NoError(t, err)
	_, err = svc.Refresh(context.Background(), withDevice.RefreshToken)
	require.NoError(t, err)
}

// ⚠️ SE DÉCONNECTER SUR L'ANCIEN TÉLÉPHONE NE DOIT PAS LIBÉRER LE NOUVEAU. Sans
// cette précaution, il suffisait de se déconnecter proprement sur l'appareil
// chassé pour effacer la session de son remplaçant, qui se retrouvait sans
// appareil enregistré — donc plus protégé du tout.
func TestLogoutFromAChasedDeviceKeepsTheLiveSession(t *testing.T) {
	svc, sessions, _ := newDeviceTestService()
	userID := signUpDriver(t, svc)
	first := loginDriver(t, svc, "install-A", "Tecno Spark 10")
	second := loginDriver(t, svc, "install-B", "Itel A70")

	require.NoError(t, svc.Logout(context.Background(), first.RefreshToken))
	assert.Equal(t, "install-B", sessions.current(userID))
	u := loadUser(t, svc, userID)
	require.NotNil(t, u.Device)
	assert.Equal(t, "install-B", u.Device.ID)

	// La déconnexion de l'appareil COURANT, elle, rend bien la session.
	require.NoError(t, svc.Logout(context.Background(), second.RefreshToken))
	assert.Empty(t, sessions.current(userID))
	u = loadUser(t, svc, userID)
	assert.Nil(t, u.Device)
}

// ⚠️ LA RÉPARATION DU REGISTRE. Redis n'est pas durable : un redémarrage, un
// `FLUSHDB` de maintenance, et le registre est vide — un téléphone chassé
// redevient alors accepté par le suivi. Chaque rafraîchissement le remet en
// place, donc au pire quinze minutes de dérive au lieu de semaines.
func TestRefreshRepairsTheRegistry(t *testing.T) {
	svc, sessions, _ := newDeviceTestService()
	userID := signUpDriver(t, svc)
	live := loginDriver(t, svc, "install-A", "Tecno Spark 10")

	sessions.mu.Lock()
	delete(sessions.bound, userID) // Redis a tout oublié
	sessions.mu.Unlock()

	_, err := svc.Refresh(context.Background(), live.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "install-A", sessions.current(userID))
	assert.Contains(t, sessions.asserted, userID)
}

// La chasse PRÉVIENT. C'est la seule alerte que reçoit un chauffeur dont
// quelqu'un d'autre utilise le compte : l'appareil chassé, lui, est peut-être
// éteint et n'apprendra rien avant trois jours.
func TestTheChaseNotifiesTheAccount(t *testing.T) {
	svc, _, notifier := newDeviceTestService()
	signUpDriver(t, svc)
	loginDriver(t, svc, "install-A", "Tecno Spark 10")
	loginDriver(t, svc, "install-B", "Itel A70")

	require.Len(t, notifier.keys, 1)
	assert.Equal(t, KeySessionSuperseded, notifier.keys[0])
	assert.Equal(t, "Itel A70", notifier.vars[0]["device"],
		"le message nomme l'appareil qui vient de PRENDRE la session : il part aussi sur celui-là")
}

// Un nom d'appareil démesuré est TRONQUÉ, jamais refusé.
//
// ⚠️ Ce texte est stocké puis recopié dans chaque message de refus : non borné,
// il suffit d'une application qui envoie dix kilo-octets pour les remplir. Mais
// refuser la CONNEXION d'un chauffeur pour un détail d'affichage serait hors de
// proportion — à la différence de l'identifiant, qui décide de qui travaille.
func TestAnOversizedDeviceNameIsTruncatedNotRefused(t *testing.T) {
	long := ""
	for range 400 {
		long += "x"
	}
	id, name := deviceFrom("driver", "install-A", long)
	assert.Equal(t, "install-A", id)
	assert.Len(t, name, maxDeviceName)

	// L'identifiant, lui, n'est pas tronqué : tronqué, deux installations
	// différentes finiraient par se confondre au moment précis où l'une
	// devrait chasser l'autre.
	longID := long
	id, _ = deviceFrom("driver", longID, "Tecno")
	assert.Empty(t, id, "un identifiant démesuré est ignoré, pas raccourci")
}

func loadUser(t *testing.T, svc *Service, userID string) *User {
	t.Helper()
	u, err := svc.findUser(context.Background(), userID)
	require.NoError(t, err)
	return u
}

// apperrReason rend `error.reason` — la seule clé de `Meta`, avec `fields`, que
// l'enveloppe d'erreur sert au réseau.
func apperrReason(t *testing.T, err error) string {
	t.Helper()
	reason, _ := apperr.From(err).Meta["reason"].(string)
	return reason
}
