package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// InitiateRequest is the provider-agnostic payload to start a payment.
type InitiateRequest struct {
	PaymentID string
	UserID    string
	Purpose   string
	Amount    int
	Currency  string
	Meta      map[string]any
}

// InitiateResult is what a provider returns after initiating a payment.
type InitiateResult struct {
	ProviderRef     string // provider-side identifier, stored on the payment
	InstructionsURL string // where the payer completes the mobile-money flow
}

// Event is a verified provider webhook notification.
type Event struct {
	ProviderRef string
	Status      string // "succeeded" | "failed"
}

// PaymentProvider abstracts a mobile-money provider. Concrete providers are
// registered in a registry map passed to NewService.
//
// The concrete provider(s) depend on the target countries and are still to be
// decided; only the abstraction and the mock implementation ship for now.
type PaymentProvider interface {
	Initiate(ctx context.Context, req InitiateRequest) (InitiateResult, error)
	VerifyWebhook(payload []byte, signature string) (Event, error)
}

// DefaultMockSecret is the webhook secret used when the mock provider is
// constructed with an empty secret.
const DefaultMockSecret = "mock-secret"

// MockProvider is a fake provider for development and tests: Initiate returns
// a random provider reference plus a fake payment-instructions URL, and
// VerifyWebhook checks a hex HMAC-SHA256 signature of the raw payload.
type MockProvider struct {
	secret []byte
}

// NewMockProvider builds the mock provider; an empty secret falls back to
// DefaultMockSecret.
func NewMockProvider(secret string) *MockProvider {
	if secret == "" {
		secret = DefaultMockSecret
	}
	return &MockProvider{secret: []byte(secret)}
}

// Initiate returns a fake provider reference and payment instructions URL.
func (p *MockProvider) Initiate(ctx context.Context, req InitiateRequest) (InitiateResult, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return InitiateResult{}, fmt.Errorf("payment: mock initiate: %w", err)
	}
	ref := "mock-" + hex.EncodeToString(buf)
	return InitiateResult{
		ProviderRef:     ref,
		InstructionsURL: "https://pay.mock.example/instructions/" + ref,
	}, nil
}

// Sign computes the hex HMAC-SHA256 signature of payload; exposed so tests
// and dev tooling can forge valid webhooks.
func (p *MockProvider) Sign(payload []byte) string {
	mac := hmac.New(sha256.New, p.secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhook checks the hex HMAC-SHA256 signature of the raw payload and
// decodes the event ({"provider_ref": "...", "status": "succeeded|failed"}).
func (p *MockProvider) VerifyWebhook(payload []byte, signature string) (Event, error) {
	expected := p.Sign(payload)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return Event{}, errors.New("payment: mock webhook signature mismatch")
	}
	var body struct {
		ProviderRef string `json:"provider_ref"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return Event{}, fmt.Errorf("payment: mock webhook payload: %w", err)
	}
	return Event{ProviderRef: body.ProviderRef, Status: body.Status}, nil
}
