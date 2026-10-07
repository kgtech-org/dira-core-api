package user

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// LA SUPPRESSION D'UN COMPTE — ce que chaque test protège est écrit au-dessus
// de lui, parce que la plupart de ces règles sont des arbitrages, pas des
// évidences.

// fakeBalances : ce qu'un compte détient encore.
type fakeBalances struct{ what string }

func (b fakeBalances) NonZeroBalance(context.Context, string) (string, bool) {
	return b.what, b.what != ""
}

// fakeAnnouncer compte les annonces faites aux verticales.
type fakeAnnouncer struct {
	told   []string
	phones []string
}

func (a *fakeAnnouncer) AccountErased(_ context.Context, userID, phone string) {
	a.told = append(a.told, userID)
	a.phones = append(a.phones, phone)
}

// fakeFiles : le stockage d'objets, qui retient ce qu'on lui demande de jeter.
type fakeFiles struct{ removed []string }

func (f *fakeFiles) Remove(_ context.Context, url string) error {
	f.removed = append(f.removed, url)
	return nil
}

// fakeInbox et fakePushDevices : ce qui se purge au passage.
type fakeInbox struct{ purged []string }

func (i *fakeInbox) PurgeOf(_ context.Context, userID string) { i.purged = append(i.purged, userID) }

type fakePushDevices struct{ forgotten []string }

func (d *fakePushDevices) ForgetAll(_ context.Context, userID string) {
	d.forgotten = append(d.forgotten, userID)
}

// newErasureTestService rend un service dont la suppression est entièrement
// branchée, et les mouchards qui disent ce qui a été touché.
func newErasureTestService(t *testing.T) (*Service, *fakeUserRepo, *fakeAnnouncer, *fakeInbox, *fakePushDevices) {
	t.Helper()
	svc, repo, _ := newUserTestService()
	announcer := &fakeAnnouncer{}
	inbox := &fakeInbox{}
	devices := &fakePushDevices{}
	svc.SetBalances(fakeBalances{})
	svc.SetErasureAnnouncer(announcer)
	svc.SetInbox(inbox)
	svc.SetPushDevices(devices)
	return svc, repo, announcer, inbox, devices
}

// registerClient ouvre un compte client et rend son identifiant.
func registerClient(t *testing.T, svc *Service) string {
	t.Helper()
	resp, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	return resp.User.ID
}

// LE PREMIER TEMPS : le compte se ferme TOUT DE SUITE. Garder un jeton vivant
// trente jours sur un compte qu'on vient de fermer laisserait commander pendant
// tout le délai de grâce, et il n'y aurait plus personne pour en répondre.
func TestDeletingAnAccountClosesItImmediately(t *testing.T) {
	svc, repo, _, _, devices := newErasureTestService(t)
	id := registerClient(t, svc)
	session, err := svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "client",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))

	// La porte est fermée des trois côtés : mot de passe, session en cours,
	// et le code à usage unique (testé plus bas).
	_, err = svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "client",
	})
	assertUserCode(t, err, "account_closed")
	_, err = svc.Refresh(context.Background(), session.RefreshToken, "")
	require.Error(t, err, "la session en cours doit tomber avec la fermeture")
	assert.Equal(t, []string{id}, devices.forgotten,
		"les notifications poussées doivent cesser le jour de la demande, pas trente jours après")

	// Mais l'identité est ENCORE LÀ : c'est tout l'objet du délai de grâce.
	u := repo.mustFind(t, id)
	assert.Equal(t, StatusClosed, u.Status)
	assert.NotNil(t, u.DeletionRequestedAt)
	assert.Nil(t, u.AnonymisedAt, "rien n'est effacé avant l'échéance")
	assert.Equal(t, "+22890000000", u.Phone)
}

// ⚠️ LE SECOND TEMPS N'ARRIVE PAS AVANT L'HEURE. Un balayage qui effacerait
// dès le lendemain retirerait au délai de grâce sa seule raison d'être.
func TestTheGracePeriodIsActuallyWaitedOut(t *testing.T) {
	svc, repo, announcer, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))

	n, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)
	assert.Zero(t, n, "le jour de la demande, rien n'est dû")
	assert.Empty(t, announcer.told)
	assert.Nil(t, repo.mustFind(t, id).AnonymisedAt)
}

// LE SECOND TEMPS : l'identité part, LA LIGNE RESTE. C'est ce qui garde
// valides les milliers de courses, commandes et écritures qui la désignent —
// et ce qui les anonymise toutes d'un coup, puisque les verticales ne
// stockent ni nom ni téléphone.
func TestWhenTheGraceIsOverTheIdentityGoesAndTheRowStays(t *testing.T) {
	svc, repo, announcer, inbox, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	_, err := svc.SaveAddress(context.Background(), id, "", AddressRequest{
		Label: "Maison", Address: "Rue des Palmiers, Lomé", Geo: [2]float64{1.22, 6.13},
	})
	require.NoError(t, err)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))
	repo.backdateClosure(t, id, DefaultErasureGrace+time.Hour)

	n, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	u := repo.mustFind(t, id)
	require.NotNil(t, u, "⚠️ LA LIGNE RESTE : la supprimer casserait toute course qui la désigne")
	assert.Equal(t, AnonymousName, u.Name,
		"un libellé, pas une chaîne vide : sinon les écrans ne distinguent pas « effacé » de « manquant »")
	assert.NotEqual(t, "+22890000000", u.Phone, "le numéro doit avoir disparu de la ligne")
	assert.Empty(t, u.PasswordHash)
	assert.NotNil(t, u.AnonymisedAt)
	// Le carnet d'adresses — « Maison », « Bureau » — est la donnée la plus
	// personnelle que le socle détienne, et il n'est l'écriture comptable de
	// rien.
	addrs, err := svc.ListAddresses(context.Background(), id)
	require.NoError(t, err)
	assert.Empty(t, addrs)
	assert.Equal(t, []string{id}, inbox.purged)
	assert.Equal(t, []string{id}, announcer.told,
		"les verticales doivent être PRÉVENUES, une fois, pour purger ce qu'elles seules détiennent")
	// ⚠️ ET AVEC LE NUMÉRO TEL QU'IL ÉTAIT. Une verticale garde des traces
	// classées par NUMÉRO et non par compte — la conversation du robot
	// WhatsApp. Sans ce numéro, elles resteraient là pour toujours, et rien ne
	// dirait comment les retrouver.
	assert.Equal(t, []string{"+22890000000"}, announcer.phones,
		"l'annonce doit porter l'ancien numéro, pas celui brouillé")
}

// ⚠️ LA PHOTO DE PROFIL EST UN FICHIER, PAS UN CHAMP. Retirer `avatar_url` du
// document laisse le VISAGE de la personne dans le bucket, et plus rien ne le
// désigne : impossible à retrouver pour le supprimer, impossible à justifier si
// on le trouve. Un compte « effacé » dont la photo survit n'est pas effacé.
func TestTheProfilePhotoLeavesTheBucketToo(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	files := &fakeFiles{}
	svc.SetFiles(files)
	id := registerClient(t, svc)
	photo := "http://minio:9000/dira-media/avatars/awa.jpg"
	_, err := svc.UpdateProfile(context.Background(), id, UpdateMeRequest{AvatarURL: &photo})
	require.NoError(t, err)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))
	repo.backdateClosure(t, id, DefaultErasureGrace+time.Hour)

	_, err = svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, []string{photo}, files.removed)
	assert.Empty(t, repo.mustFind(t, id).AvatarURL)
}

// ⚠️ LE NUMÉRO EST LIBÉRÉ PAR L'EFFACEMENT, et c'est un droit, pas un effet de
// bord : quelqu'un qui a supprimé son compte doit pouvoir revenir. Un
// téléphone laissé en place aurait rendu son propre numéro inutilisable à vie.
func TestAfterErasureTheNumberCanSignUpAgain(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))
	repo.backdateClosure(t, id, DefaultErasureGrace+time.Hour)
	_, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)

	fresh, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err, "le numéro doit être libre après l'effacement")
	assert.NotEqual(t, id, fresh.User.ID, "un compte NEUF, pas l'ancien ressuscité")
	assert.Equal(t, 2, repo.userCount(), "l'ancienne ligne reste, anonyme, pour ses courses")
}

// ⚠️ PENDANT LE DÉLAI, « CE NUMÉRO EST DÉJÀ ENREGISTRÉ » SERAIT UN MENSONGE.
// Quelqu'un qui a supprimé son compte hier et réessaie aujourd'hui lirait
// « déjà enregistré », chercherait son mot de passe, et conclurait que la
// suppression n'a pas marché.
func TestSigningUpAgainDuringTheGraceSaysTheAccountIsClosed(t *testing.T) {
	svc, _, _, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))

	_, err := svc.Register(context.Background(), registerReq())
	assertUserCode(t, err, "account_closed")
}

// ⚠️ UN EFFACEMENT DÉJÀ FAIT NE SE REFAIT PAS. Deux instances balaient en
// parallèle, et une annonce envoyée deux fois ferait purger deux fois.
func TestErasingTwiceDoesNothingTheSecondTime(t *testing.T) {
	svc, repo, announcer, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))
	repo.backdateClosure(t, id, DefaultErasureGrace+time.Hour)
	_, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)

	n, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Len(t, announcer.told, 1)
}

// ⚠️ IL FAUT PROUVER QUI ON EST. C'est irréversible depuis l'application : un
// téléphone déverrouillé posé sur une table suffirait sinon à faire
// disparaître le compte de quelqu'un.
func TestDeletingWithoutProvingWhoYouAreIsRefused(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)

	err := svc.RequestErasure(context.Background(), id, ErasureRequest{Password: "pas-le-bon"})
	assertUserCode(t, err, "invalid_credentials")
	assert.Equal(t, StatusActive, repo.mustFind(t, id).Status)
}

// Un compte né par code n'a PAS de mot de passe : il prouve son identité par
// la même porte qui l'a ouvert — un code à usage unique.
func TestAnAccountBornOfACodeProvesItselfWithACode(t *testing.T) {
	svc, repo := newOTPTestService()
	svc.SetBalances(fakeBalances{})
	phone := "+22890000333"
	code := requestCode(t, svc, phone)
	resp, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{
		Phone: phone, Code: code, App: appOfClients,
	})
	require.NoError(t, err)
	id := resp.User.ID

	// Sans code, le service dit QUOI FAIRE plutôt que de refuser sèchement.
	err = svc.RequestErasure(context.Background(), id, ErasureRequest{})
	require.Error(t, err)
	assert.Equal(t, StatusActive, repo.mustFind(t, id).Status)

	// Un code faux ne suffit pas non plus.
	fresh := requestCode(t, svc, phone)
	err = svc.RequestErasure(context.Background(), id, ErasureRequest{Code: "000000"})
	assertUserCode(t, err, "otp_invalid")

	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{Code: fresh}))
	assert.Equal(t, StatusClosed, repo.mustFind(t, id).Status)
}

// ⚠️ ET LA PORTE DU CODE SE REFERME SUR UN COMPTE FERMÉ. C'était le trou de la
// suppression : le mot de passe refusait, mais le numéro reçoit encore les
// codes pendant tout le délai de grâce.
func TestAClosedAccountCannotComeBackThroughTheCodeDoor(t *testing.T) {
	svc, _ := newOTPTestService()
	svc.SetBalances(fakeBalances{})
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))

	code := requestCode(t, svc, "+22890000000")
	_, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{
		Phone: "+22890000000", Code: code, App: appOfClients,
	})
	assertUserCode(t, err, "account_closed")
}

// ⚠️ ON NE DÉTRUIT PAS DE L'ARGENT EN SILENCE. Un solde de douze mille francs
// qui disparaît parce que quelqu'un a touché « supprimer mon compte » est une
// perte qu'il découvrira trop tard.
func TestAnAccountHoldingMoneyIsNotDeletedSilently(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	svc.SetBalances(fakeBalances{what: "money"})
	id := registerClient(t, svc)

	err := svc.RequestErasure(context.Background(), id, ErasureRequest{Password: "s3cret-password"})
	assertUserCode(t, err, "wallet_not_empty")
	assert.Equal(t, StatusActive, repo.mustFind(t, id).Status)
}

// ⚠️ ET UNE DETTE BLOQUE AUSSI, pour la raison inverse : si « supprimer mon
// compte » effaçait ce qu'on doit, ce bouton deviendrait la sortie de secours
// de toute commission en espèces impayée.
func TestAnAccountOwingMoneyIsNotDeletedEither(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	svc.SetBalances(fakeBalances{what: "debt"})
	id := registerClient(t, svc)

	assertUserCode(t,
		svc.RequestErasure(context.Background(), id, ErasureRequest{Password: "s3cret-password"}),
		"wallet_not_empty")
	assert.Equal(t, StatusActive, repo.mustFind(t, id).Status)
}

// ⚠️ RÉSERVÉE AUX CLIENTS. Un chauffeur, un livreur ou un marchand porte des
// versements et parfois une dette que le socle ne sait pas lire — ils vivent
// dans la verticale. Pour eux, la suppression passe par le support, qui a le
// grand livre sous les yeux.
func TestAnAgentDoesNotDeleteTheirOwnAccountFromTheApp(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	resp, err := svc.Register(context.Background(), RegisterRequest{
		Phone: "+22891000777", Name: "Kofi", Password: "s3cret-password",
		Role: auth.RoleDriver, App: "driver", DeviceID: "tel-1",
	})
	require.NoError(t, err)

	err = svc.RequestErasure(context.Background(), resp.User.ID, ErasureRequest{
		Password: "s3cret-password",
	})
	assertUserCode(t, err, "erasure_not_self_serve")
	assert.Equal(t, StatusActive, repo.mustFind(t, resp.User.ID).Status)
}

// Demander deux fois ne reprogramme pas : la seconde demande dirait « c'est
// déjà fait », et reporterait l'échéance si elle écrasait la date.
func TestAskingTwiceSaysItIsAlreadyScheduled(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))
	first := *repo.mustFind(t, id).DeletionRequestedAt

	err := svc.RequestErasure(context.Background(), id, ErasureRequest{Password: "s3cret-password"})
	assertUserCode(t, err, "deletion_already_requested")
	assert.Equal(t, first, *repo.mustFind(t, id).DeletionRequestedAt,
		"l'échéance ne se repousse pas à chaque appel")
}

// LE DÉLAI DE GRÂCE EST UN RECOURS, et le support doit pouvoir l'exercer.
//
// ⚠️ RÉACTIVER ANNULE L'EFFACEMENT. Sans cela, le compte redevenait actif en
// gardant sa date de demande : il se reconnectait, commandait, et le balayage
// effaçait son identité trois semaines plus tard sans que personne ne
// comprenne pourquoi un client actif venait de perdre son nom.
func TestSupportCanCallOffADeletionDuringTheGrace(t *testing.T) {
	svc, repo, announcer, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))

	_, err := svc.SetAccountStatus(context.Background(), id, StatusActive)
	require.NoError(t, err)
	assert.Nil(t, repo.mustFind(t, id).DeletionRequestedAt,
		"l'effacement programmé doit avoir été annulé, pas seulement masqué")

	n, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)
	assert.Zero(t, n, "un compte réactivé ne doit plus jamais être balayé")
	assert.Empty(t, announcer.told)

	_, err = svc.Login(context.Background(), LoginRequest{
		Phone: "+22890000000", Password: "s3cret-password", App: "client",
	})
	assert.NoError(t, err, "le compte doit se reconnecter normalement")
}

// ⚠️ ET UN SECOND VERROU TIENT MÊME SI LA BASE MENT. Ce balayage tourne toutes
// les heures sur la collection des comptes : le jour où un chemin oubliera de
// retirer la date de demande d'un compte réactivé, la faute doit être un
// journal à lire — pas un client actif qui perd son nom trois semaines plus
// tard.
func TestAnActiveAccountIsNeverErasedEvenWithAStaleRequestDate(t *testing.T) {
	svc, repo, announcer, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	repo.forceClosureDate(t, id, time.Now().UTC().Add(-DefaultErasureGrace-time.Hour))

	n, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, announcer.told)
	u := repo.mustFind(t, id)
	assert.Equal(t, "Awa", u.Name)
	assert.Nil(t, u.AnonymisedAt)
}

// ⚠️ MAIS UN COMPTE DÉJÀ EFFACÉ NE SE ROUVRE PAS : il n'a plus de nom, plus de
// téléphone, plus de mot de passe. Répondre « réactivé » rendrait une coquille
// vide, et laisserait croire que la suppression s'annule toujours.
func TestAnErasedAccountCannotBeReactivated(t *testing.T) {
	svc, repo, _, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))
	repo.backdateClosure(t, id, DefaultErasureGrace+time.Hour)
	_, err := svc.EraseDue(context.Background(), 100)
	require.NoError(t, err)

	_, err = svc.SetAccountStatus(context.Background(), id, StatusActive)
	assertUserCode(t, err, "account_already_erased")
	assert.Equal(t, StatusClosed, repo.mustFind(t, id).Status)
}

// La voie de l'exploitation : effacer SANS attendre, pour une demande légale
// ou un compte frauduleux. Elle ferme et efface d'un geste.
func TestSupportCanEraseWithoutWaiting(t *testing.T) {
	svc, repo, announcer, _, _ := newErasureTestService(t)
	id := registerClient(t, svc)

	require.NoError(t, svc.EraseNow(context.Background(), id))
	u := repo.mustFind(t, id)
	assert.Equal(t, AnonymousName, u.Name)
	assert.NotNil(t, u.AnonymisedAt)
	assert.Equal(t, []string{id}, announcer.told)
}

// ⚠️ SANS PORTEFEUILLE BRANCHÉ, LA SUPPRESSION PASSE QUAND MÊME. Mieux vaut
// une suppression non vérifiée qu'un module absent qui rend le droit à
// l'effacement indisponible — et le journal le crie.
func TestWithoutAWalletModuleTheDeletionStillGoesThrough(t *testing.T) {
	svc, repo, _ := newUserTestService()
	id := registerClient(t, svc)

	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))
	assert.Equal(t, StatusClosed, repo.mustFind(t, id).Status)
}

// Ce que l'application affiche après la demande : « fermé », et la date à
// laquelle les données partent. « Compte supprimé » suivi d'un historique
// encore lisible chez le support ne se comprend pas.
func TestTheAppIsToldWhenTheDataWillGo(t *testing.T) {
	svc, _, _, _, _ := newErasureTestService(t)
	svc.SetErasureGrace(48 * time.Hour)
	id := registerClient(t, svc)
	require.NoError(t, svc.RequestErasure(context.Background(), id, ErasureRequest{
		Password: "s3cret-password",
	}))

	out := svc.ErasureStatus(context.Background(), id)
	assert.Equal(t, StatusClosed, out.Status)
	assert.WithinDuration(t, time.Now().UTC().Add(48*time.Hour), out.EraseAt, time.Minute)
}
