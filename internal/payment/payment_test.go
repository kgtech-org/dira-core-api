package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// --- fakes ---

type fakeRepo struct {
	byID  map[primitive.ObjectID]*Payment
	byRef map[string]*Payment
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:  map[primitive.ObjectID]*Payment{},
		byRef: map[string]*Payment{},
	}
}

func (f *fakeRepo) Create(ctx context.Context, p *Payment) error {
	if p.ID.IsZero() {
		p.ID = primitive.NewObjectID()
	}
	if _, exists := f.byRef[p.ProviderRef]; exists {
		return apperr.Conflict("duplicate_provider_ref", "duplicate provider ref")
	}
	cp := *p
	f.byID[p.ID] = &cp
	f.byRef[p.ProviderRef] = &cp
	return nil
}

func (f *fakeRepo) FindByID(ctx context.Context, id primitive.ObjectID) (*Payment, error) {
	p, ok := f.byID[id]
	if !ok {
		return nil, errPaymentNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeRepo) FindByProviderRef(ctx context.Context, ref string) (*Payment, error) {
	p, ok := f.byRef[ref]
	if !ok {
		return nil, errPaymentNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeRepo) SetStatusIfPending(ctx context.Context, id primitive.ObjectID, status string) (bool, error) {
	p, ok := f.byID[id]
	if !ok || p.Status != StatusPending {
		return false, nil
	}
	p.Status = status
	return true, nil
}

type auditCall struct {
	action     string
	resourceID string
}

type fakeAudit struct {
	calls []auditCall
}

func (f *fakeAudit) Record(ctx context.Context, action, resourceType, resourceID string, before, after any) {
	f.calls = append(f.calls, auditCall{action: action, resourceID: resourceID})
}

// --- helpers ---

func newTestService(t *testing.T) (*Service, *fakeRepo, *fakeAudit, *MockProvider) {
	t.Helper()
	repo := newFakeRepo()
	rec := &fakeAudit{}
	mock := NewMockProvider("")
	svc := NewService(repo, map[string]PaymentProvider{"mock": mock}, rec)
	return svc, repo, rec, mock
}

func signedEvent(t *testing.T, mock *MockProvider, providerRef, status string) ([]byte, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"provider_ref": providerRef, "status": status})
	require.NoError(t, err)
	return payload, mock.Sign(payload)
}

// --- tests ---

func TestInitiate_Order(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	userID := primitive.NewObjectID().Hex()
	orderID := primitive.NewObjectID().Hex()

	resp, err := svc.Initiate(context.Background(), userID, InitiatePaymentRequest{
		Purpose: PurposeOrder,
		Amount:  4500,
		RefID:   orderID,
	})
	require.NoError(t, err)

	assert.Equal(t, StatusPending, resp.Payment.Status)
	assert.Equal(t, "mock", resp.Payment.Provider)
	assert.Equal(t, orderID, resp.Payment.RefID)
	assert.Equal(t, DefaultCurrency, resp.Payment.Currency)
	assert.NotEmpty(t, resp.Payment.ProviderRef)
	assert.NotEmpty(t, resp.PaymentURL)
	assert.Len(t, repo.byID, 1)
}

func TestInitiate_TokenPurchaseRequiresTokens(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	_, err := svc.Initiate(context.Background(), primitive.NewObjectID().Hex(), InitiatePaymentRequest{
		Purpose: PurposeTokenPurchase,
		Amount:  1000,
		RefID:   primitive.NewObjectID().Hex(),
	})
	require.Error(t, err)
	assert.Equal(t, "validation_failed", apperr.From(err).Code)
}

func TestInitiate_UnknownProvider(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	_, err := svc.Initiate(context.Background(), primitive.NewObjectID().Hex(), InitiatePaymentRequest{
		Purpose:  PurposeOrder,
		Amount:   1000,
		RefID:    primitive.NewObjectID().Hex(),
		Provider: "wave",
	})
	require.Error(t, err)
	assert.Equal(t, "unknown_provider", apperr.From(err).Code)
}

func TestInitiatePurchase_ProviderContract(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	userID := primitive.NewObjectID().Hex()
	ownerID := primitive.NewObjectID().Hex()

	paymentID, err := svc.InitiatePurchase(context.Background(), userID, ownerID, 10, 5000)
	require.NoError(t, err)
	require.NotEmpty(t, paymentID)

	oid, err := primitive.ObjectIDFromHex(paymentID)
	require.NoError(t, err)
	p, err := repo.FindByID(context.Background(), oid)
	require.NoError(t, err)
	assert.Equal(t, PurposeTokenPurchase, p.Purpose)
	assert.Equal(t, 5000, p.Amount)
	assert.Equal(t, ownerID, p.Meta[MetaWalletOwnerID])
	assert.Equal(t, 10, p.Meta[MetaTokens])
}

func TestHandleWebhook_InvalidSignature(t *testing.T) {
	svc, _, _, mock := newTestService(t)
	payload, _ := signedEvent(t, mock, "mock-ref", StatusSucceeded)

	err := svc.HandleWebhook(context.Background(), "mock", payload, "deadbeef")
	require.Error(t, err)
	appErr := apperr.From(err)
	assert.Equal(t, "invalid_signature", appErr.Code)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestHandleWebhook_SucceededOrder(t *testing.T) {
	svc, repo, rec, mock := newTestService(t)
	userID := primitive.NewObjectID().Hex()
	orderID := primitive.NewObjectID().Hex()

	var paidOrders []string
	svc.OnOrderPaid = func(ctx context.Context, id, _ string) error {
		paidOrders = append(paidOrders, id)
		return nil
	}

	resp, err := svc.Initiate(context.Background(), userID, InitiatePaymentRequest{
		Purpose: PurposeOrder, Amount: 4500, RefID: orderID,
	})
	require.NoError(t, err)

	payload, sig := signedEvent(t, mock, resp.Payment.ProviderRef, StatusSucceeded)
	require.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig))

	require.Equal(t, []string{orderID}, paidOrders)
	stored, err := repo.FindByProviderRef(context.Background(), resp.Payment.ProviderRef)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, stored.Status)
	require.Len(t, rec.calls, 1)
	assert.Equal(t, "payment.succeeded", rec.calls[0].action)
	assert.Equal(t, resp.Payment.ID, rec.calls[0].resourceID)
}

func TestHandleWebhook_DoubleDeliveryIsIdempotent(t *testing.T) {
	svc, _, rec, mock := newTestService(t)

	hookCalls := 0
	svc.OnOrderPaid = func(ctx context.Context, id, _ string) error {
		hookCalls++
		return nil
	}

	resp, err := svc.Initiate(context.Background(), primitive.NewObjectID().Hex(), InitiatePaymentRequest{
		Purpose: PurposeOrder, Amount: 2000, RefID: primitive.NewObjectID().Hex(),
	})
	require.NoError(t, err)

	payload, sig := signedEvent(t, mock, resp.Payment.ProviderRef, StatusSucceeded)
	require.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig))
	require.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig)) // duplicate -> 200 no-op

	assert.Equal(t, 1, hookCalls, "hook must be called exactly once")
	assert.Len(t, rec.calls, 1, "audit must be recorded exactly once")
}

func TestHandleWebhook_SucceededTokenPurchase(t *testing.T) {
	svc, _, rec, mock := newTestService(t)
	userID := primitive.NewObjectID().Hex()
	ownerID := primitive.NewObjectID().Hex()

	var gotOwner string
	var gotTokens int
	svc.OnTokensPurchased = func(ctx context.Context, walletOwnerID string, tokens int) error {
		gotOwner = walletOwnerID
		gotTokens = tokens
		return nil
	}

	resp, err := svc.Initiate(context.Background(), userID, InitiatePaymentRequest{
		Purpose: PurposeTokenPurchase, Amount: 5000, RefID: ownerID, Tokens: 10,
	})
	require.NoError(t, err)

	payload, sig := signedEvent(t, mock, resp.Payment.ProviderRef, StatusSucceeded)
	require.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig))

	assert.Equal(t, ownerID, gotOwner)
	assert.Equal(t, 10, gotTokens)
	require.Len(t, rec.calls, 1)
	assert.Equal(t, "payment.succeeded", rec.calls[0].action)
}

func TestHandleWebhook_Failed(t *testing.T) {
	svc, repo, rec, mock := newTestService(t)

	hookCalled := false
	svc.OnOrderPaid = func(ctx context.Context, id, _ string) error {
		hookCalled = true
		return nil
	}

	resp, err := svc.Initiate(context.Background(), primitive.NewObjectID().Hex(), InitiatePaymentRequest{
		Purpose: PurposeOrder, Amount: 3000, RefID: primitive.NewObjectID().Hex(),
	})
	require.NoError(t, err)

	payload, sig := signedEvent(t, mock, resp.Payment.ProviderRef, StatusFailed)
	require.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig))

	stored, err := repo.FindByProviderRef(context.Background(), resp.Payment.ProviderRef)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, stored.Status)
	assert.False(t, hookCalled, "failed payment must not trigger the paid hook")
	require.Len(t, rec.calls, 1)
	assert.Equal(t, "payment.failed", rec.calls[0].action)
}

func TestHandleWebhook_NilHooksAreSafe(t *testing.T) {
	svc, _, _, mock := newTestService(t)

	resp, err := svc.Initiate(context.Background(), primitive.NewObjectID().Hex(), InitiatePaymentRequest{
		Purpose: PurposeOrder, Amount: 3000, RefID: primitive.NewObjectID().Hex(),
	})
	require.NoError(t, err)

	payload, sig := signedEvent(t, mock, resp.Payment.ProviderRef, StatusSucceeded)
	assert.NoError(t, svc.HandleWebhook(context.Background(), "mock", payload, sig))
}

func TestHandleWebhook_UnknownProviderRef(t *testing.T) {
	svc, _, _, mock := newTestService(t)
	payload, sig := signedEvent(t, mock, "mock-unknown", StatusSucceeded)

	err := svc.HandleWebhook(context.Background(), "mock", payload, sig)
	require.Error(t, err)
	assert.Equal(t, "payment_not_found", apperr.From(err).Code)
}

func TestGet_Ownership(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ownerID := primitive.NewObjectID().Hex()

	resp, err := svc.Initiate(context.Background(), ownerID, InitiatePaymentRequest{
		Purpose: PurposeOrder, Amount: 1500, RefID: primitive.NewObjectID().Hex(),
	})
	require.NoError(t, err)

	// Owner reads it.
	got, err := svc.Get(context.Background(), ownerID, "client", resp.Payment.ID)
	require.NoError(t, err)
	assert.Equal(t, resp.Payment.ID, got.ID)

	// Admin reads it.
	_, err = svc.Get(context.Background(), primitive.NewObjectID().Hex(), "admin", resp.Payment.ID)
	require.NoError(t, err)

	// Stranger is rejected.
	_, err = svc.Get(context.Background(), primitive.NewObjectID().Hex(), "client", resp.Payment.ID)
	require.Error(t, err)
	assert.Equal(t, "forbidden", apperr.From(err).Code)
}

// La liste des opérateurs est le REGISTRE, pas un catalogue d'intentions :
// ce qui n'est pas branché n'apparaît pas, et l'application ne propose donc
// jamais un paiement qui échouera.
func TestProvidersListsOnlyWiredOnes(t *testing.T) {
	svc := NewService(newFakeRepo(), map[string]PaymentProvider{
		"mock":   NewMockProvider(""),
		"orange": NewMockProvider(""),
	}, nil)

	got := svc.Providers()
	require.Len(t, got, 2)
	assert.Equal(t, "mock", got[0].ID, "trié par identifiant")
	assert.Equal(t, "orange", got[1].ID)
	assert.Equal(t, "Orange Money", got[1].Label, "le libellé vient du SERVEUR")
}

// Un opérateur branché mais non nommé reste utilisable : l'identifiant fait
// office de libellé, plutôt que de le masquer d'une liste où il fonctionne.
func TestUnknownProviderKeepsItsID(t *testing.T) {
	svc := NewService(newFakeRepo(), map[string]PaymentProvider{"kkiapay": NewMockProvider("")}, nil)
	got := svc.Providers()
	require.Len(t, got, 1)
	assert.Equal(t, "kkiapay", got[0].Label)
}
