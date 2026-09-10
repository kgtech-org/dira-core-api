package payment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// realProvider signe... rien : c'est tout l'intérêt. Il tient le rôle d'un
// vrai prestataire, dont la signature de webhook n'est pas forgeable.
type realProvider struct{}

func (realProvider) Initiate(context.Context, InitiateRequest) (InitiateResult, error) {
	return InitiateResult{ProviderRef: "real-ref"}, nil
}
func (realProvider) VerifyWebhook([]byte, string) (Event, error) { return Event{}, nil }

func purchaseInProgress(t *testing.T, svc *Service) (paymentID, ownerID string, tokens int) {
	t.Helper()
	ownerID = primitive.NewObjectID().Hex()
	id, err := svc.InitiatePurchase(context.Background(), primitive.NewObjectID().Hex(), ownerID, 30, 3000)
	require.NoError(t, err)
	return id, ownerID, 30
}

// Le parcours complet : un achat initié ne crédite rien, la confirmation
// déclenche le crédit — par le VRAI chemin webhook, signature comprise.
func TestConfirmSandboxCreditsTheWallet(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	var credited []string
	var creditedTokens int
	svc.OnTokensPurchased = func(_ context.Context, walletOwnerID string, tokens int) error {
		credited = append(credited, walletOwnerID)
		creditedTokens += tokens
		return nil
	}
	paymentID, ownerID, tokens := purchaseInProgress(t, svc)
	assert.Empty(t, credited, "un achat initié ne crédite pas encore")

	require.NoError(t, svc.ConfirmSandboxPayment(context.Background(), paymentID))

	assert.Equal(t, []string{ownerID}, credited, "le portefeuille visé est crédité")
	assert.Equal(t, tokens, creditedTokens)

	id, _ := primitive.ObjectIDFromHex(paymentID)
	p, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, p.Status)
}

// Rejouer une confirmation ne doit pas créditer deux fois : c'est de la
// monnaie, et un double clic ne doit pas la doubler.
func TestConfirmSandboxIsIdempotent(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	calls := 0
	svc.OnTokensPurchased = func(context.Context, string, int) error { calls++; return nil }
	paymentID, _, _ := purchaseInProgress(t, svc)

	require.NoError(t, svc.ConfirmSandboxPayment(context.Background(), paymentID))
	require.NoError(t, svc.ConfirmSandboxPayment(context.Background(), paymentID))
	require.NoError(t, svc.ConfirmSandboxPayment(context.Background(), paymentID))

	assert.Equal(t, 1, calls, "trois confirmations, un seul crédit")
}

// LE verrou de fond : un vrai prestataire ne peut pas être simulé. Si ce test
// tombe, c'est qu'on a rendu possible de fabriquer un encaissement fictif sur
// un paiement réel.
func TestConfirmSandboxRefusesANonForgeableProvider(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, map[string]PaymentProvider{"real": realProvider{}}, &fakeAudit{})
	credited := 0
	svc.OnTokensPurchased = func(context.Context, string, int) error { credited++; return nil }

	resp, err := svc.Initiate(context.Background(), primitive.NewObjectID().Hex(), InitiatePaymentRequest{
		Purpose:  PurposeTokenPurchase,
		Amount:   3000,
		RefID:    primitive.NewObjectID().Hex(),
		Tokens:   30,
		Provider: "real",
	})
	require.NoError(t, err)

	err = svc.ConfirmSandboxPayment(context.Background(), resp.Payment.ID)
	require.Error(t, err)
	assert.Equal(t, "provider_not_simulatable", apperr.From(err).Code)
	assert.Zero(t, credited, "aucun crédit ne doit sortir d'un prestataire réel")
}

func TestConfirmSandboxRejectsAnUnknownPayment(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	assert.Error(t, svc.ConfirmSandboxPayment(context.Background(), "pas-un-identifiant"))
	assert.Error(t, svc.ConfirmSandboxPayment(context.Background(), primitive.NewObjectID().Hex()))
}

// La confirmation manuelle ne doit PAS exister en production. Ce test monte le
// routeur dans les deux configurations : c'est le verrou d'exposition, celui
// qui tient même si quelqu'un appelle la méthode par erreur ailleurs.
func TestSandboxRouteIsAbsentInProduction(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	passthrough := func(next http.Handler) http.Handler { return next }

	mounted := func(allowSandbox bool) int {
		r := chi.NewRouter()
		NewHandler(svc).Mount(r, passthrough, allowSandbox)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
			"/admin/payments/"+primitive.NewObjectID().Hex()+"/confirm", nil))
		return rec.Code
	}

	assert.Equal(t, http.StatusNotFound, mounted(false),
		"en production, la route de confirmation ne doit même pas exister")
	assert.NotEqual(t, http.StatusNotFound, mounted(true),
		"hors production, la route doit répondre (le rôle et le paiement sont vérifiés ensuite)")
}
