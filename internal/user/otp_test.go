package user

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// newOTPTestService rend un service dont la porte par code est ouverte, avec
// l'expéditeur `echo` — celui qui rend le code plutôt que de le remettre.
func newOTPTestService() (*Service, *fakeUserRepo) {
	svc, repo, _ := newUserTestService()
	svc.EnableOTP(EchoSender{}, "test-pepper", OTPPolicy{})
	return svc, repo
}

// requestCode demande un code et rend celui que l'écho a laissé passer.
func requestCode(t *testing.T, svc *Service, phone string) string {
	t.Helper()
	resp, err := svc.RequestOTP(context.Background(), OTPRequest{Phone: phone, App: appOfClients})
	require.NoError(t, err)
	require.True(t, resp.Sent)
	require.Equal(t, ChannelEcho, resp.Channel)
	require.Len(t, resp.DevCode, otpCodeDigits)
	return resp.DevCode
}

func TestOTPRegistersANewClient(t *testing.T) {
	svc, repo := newOTPTestService()
	code := requestCode(t, svc, "+22890000111")

	resp, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{
		Phone: "+22890000111", Code: code, App: appOfClients,
	})
	require.NoError(t, err)

	assert.True(t, resp.Created, "l'application n'a que ce signal pour demander le nom")
	assert.Equal(t, auth.RoleClient, resp.User.Role)
	assert.Equal(t, "+22890000111", resp.User.Phone)
	// Sans nom donné, le NUMÉRO fait office de nom d'affichage — la même
	// convention que `PATCH /me`, qui traite « nom égal au téléphone » comme
	// « nom non posé ».
	assert.Equal(t, "+22890000111", resp.User.Name)
	assert.NotEmpty(t, resp.AccessToken)
	assert.Equal(t, 1, repo.userCount())
}

func TestOTPSignsInAnExistingClientWithoutCreating(t *testing.T) {
	svc, repo := newOTPTestService()
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	phone := "+22890000000"

	code := requestCode(t, svc, phone)
	resp, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: code})
	require.NoError(t, err)

	assert.False(t, resp.Created)
	assert.Equal(t, 1, repo.userCount(), "aucun second compte pour le même numéro")
	assert.NotEmpty(t, resp.AccessToken)
}

// ⚠️ Le compte né par code N'A PAS DE MOT DE PASSE, et l'empreinte vide ne
// doit ouvrir aucune porte : sans ce test, une chaîne vide acceptée par
// `VerifyPassword` ouvrirait tous les comptes du pays.
func TestOTPAccountCannotSignInWithAnEmptyPassword(t *testing.T) {
	svc, _ := newOTPTestService()
	phone := "+22890000222"
	code := requestCode(t, svc, phone)
	_, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: code})
	require.NoError(t, err)

	// Le mot de passe vide est refusé par la validation du DTO avant même
	// d'arriver ici (`validate:"required"`) ; appelé en direct, le service
	// répond comme pour un mot de passe faux — et c'est ce qu'on veut : la
	// réponse ne doit pas distinguer « pas de mot de passe » de « mauvais mot
	// de passe », sous peine de dire lesquels des numéros essayés existent.
	for _, password := range []string{"", "anything"} {
		_, err = svc.Login(context.Background(), LoginRequest{Phone: phone, Password: password})
		assertUserCode(t, err, "invalid_credentials")
	}
}

func TestOTPNormalisesThePhoneAndKeepsOneAccount(t *testing.T) {
	svc, repo := newOTPTestService()
	code := requestCode(t, svc, "+228 90 00 03 33")
	resp, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{
		Phone: "+22890000333", Code: code,
	})
	require.NoError(t, err)
	assert.Equal(t, "+22890000333", resp.User.Phone)
	assert.Equal(t, 1, repo.userCount())
}

func TestOTPWrongCodeIsRefusedAndCounted(t *testing.T) {
	svc, _ := newOTPTestService()
	phone := "+22890000444"
	requestCode(t, svc, phone)

	_, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: "000000"})
	assertUserCode(t, err, "otp_invalid")

	rec, err := svc.repo.FindOTP(context.Background(), phone)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, 1, rec.Attempts)
}

func TestOTPDiesAfterTooManyWrongCodes(t *testing.T) {
	svc, _ := newOTPTestService()
	phone := "+22890000555"
	code := requestCode(t, svc, phone)

	for range otpDefaultAttempts {
		_, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: "000000"})
		require.Error(t, err)
	}
	// ⚠️ LE BON CODE NE VAUT PLUS RIEN. Laisser vivre le code après cinq
	// essais ratés ferait du plafond un décompte sans effet.
	_, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: code})
	assertUserCode(t, err, "otp_invalid")

	rec, err := svc.repo.FindOTP(context.Background(), phone)
	require.NoError(t, err)
	assert.Nil(t, rec, "le code est effacé, pas seulement refusé")
}

func TestOTPExpiredCodeSaysSo(t *testing.T) {
	svc, _ := newOTPTestService()
	svc.EnableOTP(EchoSender{}, "test-pepper", OTPPolicy{TTL: time.Nanosecond})
	phone := "+22890000666"
	code := requestCode(t, svc, phone)
	time.Sleep(2 * time.Millisecond)

	_, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: code})
	assertUserCode(t, err, "otp_expired")
}

func TestOTPCodeIsSingleUse(t *testing.T) {
	svc, _ := newOTPTestService()
	phone := "+22890000777"
	code := requestCode(t, svc, phone)

	_, err := svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: code})
	require.NoError(t, err)
	_, err = svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: phone, Code: code})
	assertUserCode(t, err, "otp_invalid")
}

func TestOTPResendWindowHoldsTheSecondRequest(t *testing.T) {
	svc, _ := newOTPTestService()
	phone := "+22890000888"
	requestCode(t, svc, phone)

	_, err := svc.RequestOTP(context.Background(), OTPRequest{Phone: phone})
	assertUserCode(t, err, "otp_too_soon")
}

func TestOTPHourlyCapStopsTheFlood(t *testing.T) {
	svc, _ := newOTPTestService()
	// Sans fenêtre de renvoi, c'est le PLAFOND HORAIRE qu'on éprouve.
	svc.EnableOTP(EchoSender{}, "test-pepper", OTPPolicy{Resend: time.Nanosecond, MaxPerWindow: 3})
	phone := "+22890000999"

	for i := range 3 {
		_, err := svc.RequestOTP(context.Background(), OTPRequest{Phone: phone})
		require.NoError(t, err, "demande %d", i+1)
	}
	_, err := svc.RequestOTP(context.Background(), OTPRequest{Phone: phone})
	assertUserCode(t, err, "otp_too_many_requests")
}

// ⚠️ LA RÈGLE DE SÉCURITÉ DU MODULE : six chiffres ne remplacent un mot de
// passe que pour un CLIENT. Un chauffeur, un marchand ou un administrateur
// dont on connaît le numéro — et un numéro se lit sur une plaque — ne doit
// même pas recevoir de code.
func TestOTPRefusesNonClientAccounts(t *testing.T) {
	for _, role := range []string{auth.RoleDriver, auth.RoleMerchant, auth.RoleAdmin} {
		t.Run(role, func(t *testing.T) {
			svc, repo := newOTPTestService()
			req := registerReq()
			req.Phone = "+2289000" + role[:4]
			_, err := svc.Register(context.Background(), RegisterRequest{
				Phone: "+22891000001", Name: "X", Password: "s3cret-password", Role: auth.RoleDriver,
			})
			require.NoError(t, err)
			if role != auth.RoleDriver {
				// Les rôles que l'inscription publique refuse sont posés
				// directement, comme le provisionnement le fait.
				u, _ := repo.FindByPhone(context.Background(), "+22891000001")
				u.Role = role
				require.NoError(t, repo.UpdateUser(context.Background(), u))
			}

			_, err = svc.RequestOTP(context.Background(), OTPRequest{Phone: "+22891000001"})
			assertUserCode(t, err, "otp_not_available")
		})
	}
}

func TestOTPRefusesNonClientApps(t *testing.T) {
	svc, _ := newOTPTestService()
	for _, app := range []string{"driver", "courier", "merchant", "console"} {
		_, err := svc.RequestOTP(context.Background(), OTPRequest{Phone: "+22892000001", App: app})
		assertUserCode(t, err, "otp_not_available")
	}
}

func TestOTPRefusesASuspendedAccount(t *testing.T) {
	svc, repo := newOTPTestService()
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	repo.setStatus(t, "+22890000000", StatusSuspended)

	code := requestCode(t, svc, "+22890000000")
	_, err = svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: "+22890000000", Code: code})
	assertUserCode(t, err, "account_suspended")
}

// Sans expéditeur branché, la porte se TAIT plutôt que d'envoyer dans le vide.
func TestOTPIsClosedWithoutASender(t *testing.T) {
	svc, _, _ := newUserTestService()
	_, err := svc.RequestOTP(context.Background(), OTPRequest{Phone: "+22893000001"})
	assertUserCode(t, err, "otp_not_available")
	_, err = svc.VerifyOTP(context.Background(), OTPVerifyRequest{Phone: "+22893000001", Code: "123456"})
	assertUserCode(t, err, "otp_not_available")
}

func TestOTPCodeIsSixUniformDigits(t *testing.T) {
	seen := map[string]int{}
	for range 500 {
		code, err := newOTPCode()
		require.NoError(t, err)
		require.Len(t, code, otpCodeDigits)
		for _, r := range code {
			require.True(t, r >= '0' && r <= '9', "chiffres seulement")
		}
		seen[code]++
	}
	// Un générateur figé — horloge non ensemencée, constante — se voit ici.
	assert.Greater(t, len(seen), 400, "les codes doivent varier")
}

// Le code n'est jamais rangé en clair : ce qui vit en base est une empreinte,
// et elle dépend du POIVRE, qui ne vit pas dans la base.
func TestOTPIsStoredHashedAndPeppered(t *testing.T) {
	svc, _ := newOTPTestService()
	phone := "+22894000001"
	code := requestCode(t, svc, phone)

	rec, err := svc.repo.FindOTP(context.Background(), phone)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.NotContains(t, rec.Hash, code)
	assert.Equal(t, svc.hashOTP(phone, code), rec.Hash)

	other, _, _ := newUserTestService()
	other.EnableOTP(EchoSender{}, "autre-poivre", OTPPolicy{})
	assert.NotEqual(t, other.hashOTP(phone, code), rec.Hash, "le poivre doit changer l'empreinte")
}

func TestOTPDefaultChannelIsWhatsApp(t *testing.T) {
	assert.Equal(t, ChannelWhatsApp, normaliseChannel(""))
	assert.Equal(t, ChannelWhatsApp, normaliseChannel("whatsapp"))
	assert.Equal(t, ChannelSMS, normaliseChannel("SMS"))
}

func TestMaskPhoneKeepsOnlyTheLastDigits(t *testing.T) {
	assert.Equal(t, "****0001", maskPhone("+22890000001"))
	assert.Equal(t, "****", maskPhone("+228"))
}
