package payment

import "time"

// InitiatePaymentRequest is the body of POST /payments/initiate.
type InitiatePaymentRequest struct {
	Purpose string `json:"purpose" validate:"required,oneof=order ride token_purchase wallet_topup"`
	Amount  int    `json:"amount" validate:"required,gt=0"`
	// RefID is the order id (purpose "order") or the wallet owner id
	// (purpose "token_purchase").
	RefID    string `json:"ref_id" validate:"required"`
	Provider string `json:"provider" validate:"omitempty"` // defaults to "mock"
	// Tokens is required (>0) for purpose "token_purchase".
	Tokens int `json:"tokens" validate:"omitempty,gt=0"`
}

// PaymentResponse is the public representation of a payment.
type PaymentResponse struct {
	ID          string         `json:"id"`
	UserID      string         `json:"user_id"`
	Purpose     string         `json:"purpose"`
	RefID       string         `json:"ref_id"`
	Provider    string         `json:"provider"`
	Amount      int            `json:"amount"`
	Currency    string         `json:"currency"`
	Status      string         `json:"status"`
	ProviderRef string         `json:"provider_ref"`
	Meta        map[string]any `json:"meta,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// InitiatePaymentResponse is returned by POST /payments/initiate.
type InitiatePaymentResponse struct {
	Payment PaymentResponse `json:"payment"`
	// PaymentURL is where the payer completes the mobile-money flow.
	PaymentURL string `json:"payment_url"`
}

// WebhookAck is the body returned to providers on accepted webhooks.
type WebhookAck struct {
	Received bool `json:"received"`
}

func toPaymentResponse(p *Payment) PaymentResponse {
	return PaymentResponse{
		ID:          p.ID.Hex(),
		UserID:      p.UserID.Hex(),
		Purpose:     p.Purpose,
		RefID:       p.RefID.Hex(),
		Provider:    p.Provider,
		Amount:      p.Amount,
		Currency:    p.Currency,
		Status:      p.Status,
		ProviderRef: p.ProviderRef,
		Meta:        p.Meta,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}
