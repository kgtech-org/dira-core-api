package user

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// fakeUserRepo reproduces the persistence contract in memory, including the
// duplicate-key semantics of the unique phone and token_hash indexes.
type fakeUserRepo struct {
	mu            sync.Mutex
	users         map[primitive.ObjectID]*User
	refreshTokens map[string]*RefreshToken // by token hash
	addresses     map[primitive.ObjectID]*Address
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		addresses:     make(map[primitive.ObjectID]*Address),
		users:         make(map[primitive.ObjectID]*User),
		refreshTokens: make(map[string]*RefreshToken),
	}
}

func (f *fakeUserRepo) CreateUser(_ context.Context, u *User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.users {
		if existing.Phone == u.Phone {
			return ErrDuplicatePhone
		}
	}
	u.ID = primitive.NewObjectID()
	clone := *u
	f.users[u.ID] = &clone
	return nil
}

func (f *fakeUserRepo) FindByPhone(_ context.Context, phone string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Phone == phone {
			clone := *u
			return &clone, nil
		}
	}
	return nil, nil
}

func (f *fakeUserRepo) FindByEmail(_ context.Context, email string) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Email != "" && u.Email == email {
			clone := *u
			return &clone, nil
		}
	}
	return nil, nil
}

func (f *fakeUserRepo) FindByID(_ context.Context, id primitive.ObjectID) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id]
	if !ok {
		return nil, nil
	}
	clone := *u
	return &clone, nil
}

func (f *fakeUserRepo) UpdateUser(_ context.Context, u *User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.users[u.ID]
	if !ok {
		return errors.New("user not found")
	}
	// ⚠️ MÊME liste de champs que le `$set` du vrai dépôt, et elle doit le
	// rester. C'est en s'écartant l'une de l'autre qu'elles ont laissé passer
	// `avatar_url` : le service posait la nouvelle photo, la réponse la
	// montrait, rien n'était écrit, et aucun test ne le voyait parce que le
	// faux ne l'écrivait pas non plus.
	existing.Name = u.Name
	existing.FirstName = u.FirstName
	existing.LastName = u.LastName
	existing.BirthDate = u.BirthDate
	existing.Gender = u.Gender
	existing.Email = u.Email
	existing.AvatarURL = u.AvatarURL
	existing.Preferences = u.Preferences
	existing.Status = u.Status
	existing.UpdatedAt = u.UpdatedAt
	return nil
}

func (f *fakeUserRepo) DeleteUser(_ context.Context, id primitive.ObjectID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, id)
	return nil
}

func (f *fakeUserRepo) InsertRefreshToken(_ context.Context, t *RefreshToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.refreshTokens[t.TokenHash]; exists {
		return errors.New("duplicate token hash")
	}
	t.ID = primitive.NewObjectID()
	clone := *t
	f.refreshTokens[t.TokenHash] = &clone
	return nil
}

func (f *fakeUserRepo) DeleteRefreshTokenByHash(_ context.Context, tokenHash string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.refreshTokens[tokenHash]; !ok {
		return false, nil
	}
	delete(f.refreshTokens, tokenHash)
	return true, nil
}

func (f *fakeUserRepo) userCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.users)
}

func (f *fakeUserRepo) setStatus(t *testing.T, phone, status string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.users {
		if u.Phone == phone {
			u.Status = status
			return
		}
	}
	t.Fatalf("user with phone %s not found", phone)
}

// fakeWalletCreator records CreateWallet calls and optionally fails.
type fakeWalletCreator struct {
	mu    sync.Mutex
	calls []struct{ ownerID, walletType string }
	err   error
}

func (f *fakeWalletCreator) CreateWallet(_ context.Context, ownerID, walletType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, struct{ ownerID, walletType string }{ownerID, walletType})
	return nil
}

func newUserTestService() (*Service, *fakeUserRepo, *fakeWalletCreator) {
	repo := newFakeUserRepo()
	wallets := &fakeWalletCreator{}
	manager := auth.NewManager("test-secret", 15*time.Minute, 7*24*time.Hour)
	return NewService(repo, manager, wallets), repo, wallets
}

func assertUserCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, code, apperr.From(err).Code)
}

func registerReq() RegisterRequest {
	return RegisterRequest{Phone: "+22890000000", Name: "Awa", Password: "s3cret-password"}
}

// Le carnet d'adresses, reproduit comme le vrai dépôt : le filtre porte sur
// l'utilisateur ET l'identifiant — une adresse d'autrui ne doit pas pouvoir
// être lue, même le temps d'un contrôle — et l'adresse par défaut sort en tête.
func (r *fakeUserRepo) ListAddresses(_ context.Context, userID primitive.ObjectID) ([]Address, error) {
	var out []Address
	for _, a := range r.addresses {
		if a.UserID == userID {
			out = append(out, *a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *fakeUserRepo) CountAddresses(_ context.Context, userID primitive.ObjectID) (int64, error) {
	var n int64
	for _, a := range r.addresses {
		if a.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (r *fakeUserRepo) FindAddress(_ context.Context, userID, id primitive.ObjectID) (*Address, error) {
	a, ok := r.addresses[id]
	if !ok || a.UserID != userID {
		return nil, nil
	}
	clone := *a
	return &clone, nil
}

func (r *fakeUserRepo) SaveAddress(_ context.Context, a *Address) error {
	if a.ID.IsZero() {
		a.ID = primitive.NewObjectID()
	}
	if r.addresses == nil {
		r.addresses = map[primitive.ObjectID]*Address{}
	}
	clone := *a
	r.addresses[a.ID] = &clone
	return nil
}

func (r *fakeUserRepo) DeleteAddress(_ context.Context, userID, id primitive.ObjectID) (bool, error) {
	a, ok := r.addresses[id]
	if !ok || a.UserID != userID {
		return false, nil
	}
	delete(r.addresses, id)
	return true, nil
}

func (r *fakeUserRepo) ClearDefaultAddress(_ context.Context, userID, except primitive.ObjectID) error {
	for id, a := range r.addresses {
		if a.UserID == userID && id != except {
			a.IsDefault = false
		}
	}
	return nil
}

func TestRegisterSuccess(t *testing.T) {
	svc, _, wallets := newUserTestService()

	resp, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	assert.Equal(t, "+22890000000", resp.User.Phone)
	assert.Equal(t, auth.RoleClient, resp.User.Role, "default role is client")
	assert.Equal(t, StatusActive, resp.User.Status)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEmpty(t, resp.RefreshToken)
	// Un client a désormais un portefeuille d'ARGENT — « Dira Cash ». Il ne
	// porte aucun jeton : ceux-ci sont le droit d'entrée d'un livreur et
	// l'outil de promotion d'un marchand.
	require.Len(t, wallets.calls, 1)
	assert.Equal(t, "client", wallets.calls[0].walletType)

	// The password must never leak into the response.
	assert.NotContains(t, resp.User.Name+resp.User.Phone+resp.User.Email, "s3cret-password")

	// The tokens must verify against the manager.
	manager := auth.NewManager("test-secret", 15*time.Minute, 7*24*time.Hour)
	claims, err := manager.Verify(resp.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, auth.TokenTypeAccess, claims.Type)
	assert.Equal(t, resp.User.ID, claims.UserID)
	// The refresh token is "<jwt>.<nonce>": its JWT part must verify.
	jwtPart, ok := splitRefreshToken(resp.RefreshToken)
	require.True(t, ok, "refresh token must have a nonce segment")
	claims, err = manager.Verify(jwtPart)
	require.NoError(t, err)
	assert.Equal(t, auth.TokenTypeRefresh, claims.Type)
}

func TestRegisterPhoneTaken(t *testing.T) {
	svc, _, _ := newUserTestService()

	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), registerReq())
	assertUserCode(t, err, "phone_taken")
	assert.Equal(t, 409, apperr.From(err).HTTPStatus)
}

func TestRegisterAdminRoleRejected(t *testing.T) {
	svc, repo, _ := newUserTestService()

	req := registerReq()
	req.Role = auth.RoleAdmin
	_, err := svc.Register(context.Background(), req)
	assertUserCode(t, err, "validation_failed")
	assert.Equal(t, 422, apperr.From(err).HTTPStatus)
	assert.Equal(t, 0, repo.userCount(), "no account created for rejected role")
}

func TestRegisterDriverCreatesWallet(t *testing.T) {
	svc, _, wallets := newUserTestService()

	req := registerReq()
	req.Role = auth.RoleDriver
	resp, err := svc.Register(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, wallets.calls, 1)
	assert.Equal(t, resp.User.ID, wallets.calls[0].ownerID)
	assert.Equal(t, "driver", wallets.calls[0].walletType)
}

func TestRegisterDriverWalletFailureDeletesUser(t *testing.T) {
	svc, repo, wallets := newUserTestService()
	wallets.err = errors.New("token service down")

	req := registerReq()
	req.Role = auth.RoleDriver
	_, err := svc.Register(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, 0, repo.userCount(), "no orphan driver account without a wallet")
}

func TestRegisterPasswordIsHashed(t *testing.T) {
	svc, repo, _ := newUserTestService()

	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	stored, err := repo.FindByPhone(context.Background(), "+22890000000")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.True(t, strings.HasPrefix(stored.PasswordHash, "$argon2id$"), "argon2id encoded hash")
	assert.NotContains(t, stored.PasswordHash, "s3cret-password")

	ok, err := VerifyPassword("s3cret-password", stored.PasswordHash)
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = VerifyPassword("wrong-password", stored.PasswordHash)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestLoginSuccess(t *testing.T) {
	svc, _, _ := newUserTestService()
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	resp, err := svc.Login(context.Background(), LoginRequest{Phone: "+22890000000", Password: "s3cret-password"})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEmpty(t, resp.RefreshToken)
	assert.Equal(t, "+22890000000", resp.User.Phone)
}

func TestLoginByEmail(t *testing.T) {
	svc, _, _ := newUserTestService()
	req := registerReq()
	req.Email = "Awa@Example.COM"
	_, err := svc.Register(context.Background(), req)
	require.NoError(t, err)

	// Email is matched case-insensitively (stored lowercase).
	resp, err := svc.Login(context.Background(), LoginRequest{Email: "awa@example.com", Password: "s3cret-password"})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)

	// Both or neither identifier → validation error.
	_, err = svc.Login(context.Background(), LoginRequest{Password: "s3cret-password"})
	assertUserCode(t, err, "validation_failed")
	_, err = svc.Login(context.Background(), LoginRequest{Phone: "+22890000000", Email: "awa@example.com", Password: "s3cret-password"})
	assertUserCode(t, err, "validation_failed")
}

func TestLoginWrongPassword(t *testing.T) {
	svc, _, _ := newUserTestService()
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	_, err = svc.Login(context.Background(), LoginRequest{Phone: "+22890000000", Password: "wrong-password"})
	assertUserCode(t, err, "invalid_credentials")
	assert.Equal(t, 401, apperr.From(err).HTTPStatus)

	// Unknown phone gets the same error, no account enumeration.
	_, err = svc.Login(context.Background(), LoginRequest{Phone: "+22899999999", Password: "s3cret-password"})
	assertUserCode(t, err, "invalid_credentials")
}

func TestLoginSuspended(t *testing.T) {
	svc, repo, _ := newUserTestService()
	_, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	repo.setStatus(t, "+22890000000", StatusSuspended)

	_, err = svc.Login(context.Background(), LoginRequest{Phone: "+22890000000", Password: "s3cret-password"})
	assertUserCode(t, err, "account_suspended")
	assert.Equal(t, 403, apperr.From(err).HTTPStatus)
}

func TestRefreshRotation(t *testing.T) {
	svc, _, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	pair, err := svc.Refresh(context.Background(), reg.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, pair.AccessToken)
	assert.NotEmpty(t, pair.RefreshToken)

	// The old refresh token was rotated out: reuse must fail.
	_, err = svc.Refresh(context.Background(), reg.RefreshToken)
	assertUserCode(t, err, "invalid_token")

	// The new one still works.
	_, err = svc.Refresh(context.Background(), pair.RefreshToken)
	require.NoError(t, err)
}

func TestRefreshRejectsAccessTokenAndGarbage(t *testing.T) {
	svc, _, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	_, err = svc.Refresh(context.Background(), reg.AccessToken)
	assertUserCode(t, err, "invalid_token")

	_, err = svc.Refresh(context.Background(), "not-a-jwt")
	assertUserCode(t, err, "invalid_token")
}

func TestRefreshSuspendedAccount(t *testing.T) {
	svc, repo, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)
	repo.setStatus(t, "+22890000000", StatusSuspended)

	_, err = svc.Refresh(context.Background(), reg.RefreshToken)
	assertUserCode(t, err, "account_suspended")
}

func TestLogoutInvalidatesRefreshToken(t *testing.T) {
	svc, _, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	require.NoError(t, svc.Logout(context.Background(), reg.RefreshToken))

	_, err = svc.Refresh(context.Background(), reg.RefreshToken)
	assertUserCode(t, err, "invalid_token")

	// Logout is idempotent.
	require.NoError(t, svc.Logout(context.Background(), reg.RefreshToken))
}

func TestMeAndUpdateProfile(t *testing.T) {
	svc, _, _ := newUserTestService()
	reg, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	me, err := svc.Me(context.Background(), reg.User.ID)
	require.NoError(t, err)
	assert.Equal(t, "Awa", me.Name)

	newName := "Awa D."
	newEmail := "awa@example.com"
	updated, err := svc.UpdateProfile(context.Background(), reg.User.ID, UpdateMeRequest{Name: &newName, Email: &newEmail})
	require.NoError(t, err)
	assert.Equal(t, "Awa D.", updated.Name)
	assert.Equal(t, "awa@example.com", updated.Email)

	me, err = svc.Me(context.Background(), reg.User.ID)
	require.NoError(t, err)
	assert.Equal(t, "Awa D.", me.Name)
	assert.Equal(t, "awa@example.com", me.Email)

	_, err = svc.Me(context.Background(), primitive.NewObjectID().Hex())
	assertUserCode(t, err, "user_not_found")
}

func (f *fakeUserRepo) FindNamesByIDs(_ context.Context, ids []primitive.ObjectID) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if u, ok := f.users[id]; ok {
			out[id.Hex()] = u.Name
		}
	}
	return out, nil
}

// --- administration des comptes ---

// Le faux dépôt REPRODUIT le comportement attendu — filtres, tri croissant,
// curseur, état rendu avant modification — plutôt que de rendre des listes
// vides. Un faux qui ne fait rien fait passer au vert des tests qui ne
// vérifient rien.

func (f *fakeUserRepo) toRow(u *User) AccountRow {
	return AccountRow{
		ID: u.ID.Hex(), OID: u.ID, Role: u.Role, Phone: u.Phone, Name: u.Name,
		Email: u.Email, Status: u.Status, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

func (f *fakeUserRepo) ListAccounts(_ context.Context, flt AccountFilter, cursor string, limit int) ([]AccountRow, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []AccountRow
	for _, u := range f.users {
		if flt.Role != "" && u.Role != flt.Role {
			continue
		}
		if flt.Status != "" && u.Status != flt.Status {
			continue
		}
		if flt.Query != "" {
			q := strings.ToLower(flt.Query)
			if !strings.Contains(strings.ToLower(u.Name), q) && !strings.Contains(u.Phone, flt.Query) {
				continue
			}
		}
		rows = append(rows, f.toRow(u))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].OID.Hex() < rows[j].OID.Hex() })
	if cursor != "" {
		for i, r := range rows {
			if r.OID.Hex() > cursor {
				rows = rows[i:]
				break
			}
			if i == len(rows)-1 {
				rows = nil
			}
		}
	}
	if limit <= 0 {
		limit = 20
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[limit-1].OID.Hex()
	}
	return rows, next, nil
}

func (f *fakeUserRepo) AccountByID(_ context.Context, id string) (*AccountRow, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errAccountNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[oid]
	if !ok {
		return nil, errAccountNotFound
	}
	row := f.toRow(u)
	return &row, nil
}

func (f *fakeUserRepo) AccountsByIDs(_ context.Context, ids []string) ([]AccountRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []AccountRow
	for _, id := range ids {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			continue
		}
		if u, ok := f.users[oid]; ok {
			rows = append(rows, f.toRow(u))
		}
	}
	return rows, nil
}

func (f *fakeUserRepo) SetAccountStatus(_ context.Context, id, status string) (*AccountRow, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errAccountNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[oid]
	if !ok {
		return nil, errAccountNotFound
	}
	before := f.toRow(u)
	u.Status = status
	u.UpdatedAt = time.Now().UTC()
	return &before, nil
}

func TestSetAccountStatusReturnsThePreviousState(t *testing.T) {
	svc, _, _ := newUserTestService()

	created, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	before, err := svc.SetAccountStatus(context.Background(), created.User.ID, StatusSuspended)
	require.NoError(t, err)
	// L'état AVANT distingue « suspendre un compte actif » de « suspendre un
	// compte déjà suspendu ». Sans lui, la trace d'audit ne dit pas ce qui a
	// réellement bougé.
	assert.Equal(t, StatusActive, before.Status)

	again, err := svc.SetAccountStatus(context.Background(), created.User.ID, StatusSuspended)
	require.NoError(t, err)
	assert.Equal(t, StatusSuspended, again.Status)
}

func TestSetAccountStatusRefusesAnUnknownStatus(t *testing.T) {
	svc, repo, _ := newUserTestService()
	created, err := svc.Register(context.Background(), registerReq())
	require.NoError(t, err)

	// « deleted » n'est pas un état de compte. L'accepter écrirait dans la
	// base un état qu'aucun écran ne sait lire et qu'aucun filtre ne trouve.
	_, err = svc.SetAccountStatus(context.Background(), created.User.ID, "deleted")
	require.Error(t, err)

	// Et surtout : le refus doit avoir eu lieu AVANT l'écriture.
	row, err := repo.AccountByID(context.Background(), created.User.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, row.Status)
}

func TestAccountsByIDsRefusesAnUnboundedList(t *testing.T) {
	svc, _, _ := newUserTestService()
	ids := make([]string, 201)
	for i := range ids {
		ids[i] = primitive.NewObjectID().Hex()
	}
	// Une liste d'identifiants sans limite est une lecture de toute la table
	// déguisée en résolution de noms.
	_, err := svc.AccountsByIDs(context.Background(), ids)
	require.Error(t, err)
}
