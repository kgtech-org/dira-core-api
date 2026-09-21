package finance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/internal/token"
)

func wallet(kind string) *token.Wallet {
	return &token.Wallet{ID: primitive.NewObjectID(), OwnerID: primitive.NewObjectID(), Type: kind, Country: "TG"}
}

func tx(kind, reason, unit string, amount int, refKind string, ref map[string]any) *token.Transaction {
	oid := primitive.NewObjectID()
	return &token.Transaction{ID: primitive.NewObjectID(), Kind: kind, Reason: reason, Unit: unit, Amount: amount, RefKind: refKind, RefID: &oid, Ref: ref, CreatedAt: time.Now().UTC()}
}

// Chaque motif du grand livre des portefeuilles a SA traduction, équilibrée,
// sur les bons comptes. C'est cette table qui fait l'aperçu comptable.
func TestEveryReasonHasABalancedTranslation(t *testing.T) {
	cases := []struct {
		name   string
		tx     *token.Transaction
		w      *token.Wallet
		debit  string
		credit string
		amount int
	}{
		{"recharge client", tx(token.KindPurchase, token.ReasonWalletTopup, token.UnitXOF, 5000, "", nil), wallet("client"), AccMobileMoney, AccClientWallets, 5000},
		{"promo offerte", tx(token.KindPurchase, token.ReasonPromoCredit, token.UnitXOF, 1000, "", nil), wallet("client"), AccExpensePromo, AccPromoCredits, 1000},
		{"paiement commande (argent)", tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 3000, token.RefOrder, map[string]any{"source": "cash"}), wallet("client"), AccClientWallets, AccOrderClearing, 3000},
		{"paiement course (promo)", tx(token.KindConsume, token.ReasonPayment, token.UnitXOF, 800, token.RefRide, map[string]any{"source": "promo"}), wallet("client"), AccPromoCredits, AccRideClearing, 800},
		{"pourboire", tx(token.KindConsume, token.ReasonTip, token.UnitXOF, 500, token.RefTip, map[string]any{"source": "cash"}), wallet("client"), AccClientWallets, AccAgentPayables, 500},
		{"remboursement", tx(token.KindPurchase, token.ReasonRefund, token.UnitXOF, 3000, token.RefOrder, nil), wallet("client"), AccOrderClearing, AccClientWallets, 3000},
		{"produit marchand", tx(token.KindPurchase, token.ReasonOrderPayout, token.UnitXOF, 2500, token.RefOrder, nil), wallet("merchant"), AccOrderClearing, AccMerchantPayables, 2500},
		{"frais de livraison", tx(token.KindPurchase, token.ReasonDeliveryFee, token.UnitXOF, 500, token.RefOrder, nil), wallet("driver"), AccOrderClearing, AccAgentPayables, 500},
		{"matériel prélevé", tx(token.KindConsume, token.ReasonEquipment, token.UnitXOF, 300, token.RefEquipment, nil), wallet("driver"), AccAgentPayables, AccRevenueEquipment, 300},
		{"caution rendue", tx(token.KindPurchase, token.ReasonEquipment, token.UnitXOF, 5000, token.RefEquipment, nil), wallet("driver"), AccRevenueEquipment, AccAgentPayables, 5000},
		{"commission retenue", tx(token.KindConsume, token.ReasonCommission, token.UnitXOF, 150, token.RefOrder, nil), wallet("driver"), AccAgentPayables, AccRevenueCommission, 150},
		{"commission due (espèces)", tx(token.KindConsume, token.ReasonCommissionDue, token.UnitXOF, 150, token.RefOrder, map[string]any{"debt_delta": 150, "no_balance": true}), wallet("driver"), AccAgentReceivable, AccRevenueCommission, 150},
		{"paiement dû (client)", tx(token.KindConsume, token.ReasonPaymentDue, token.UnitXOF, 2000, token.RefRide, map[string]any{"debt_delta": 2000, "no_balance": true}), wallet("client"), AccAgentReceivable, AccRideClearing, 2000},
		{"dette remboursée sur gain", tx(token.KindConsume, token.ReasonDebtRepaid, token.UnitXOF, 150, token.RefOrder, map[string]any{"debt_delta": -150}), wallet("driver"), AccAgentPayables, AccAgentReceivable, 150},
		{"dette réglée à l'agence", tx(token.KindConsume, token.ReasonDebtRepaid, token.UnitXOF, 150, "", map[string]any{"debt_delta": -150, "no_balance": true, "settled": "agency"}), wallet("driver"), AccMobileMoney, AccAgentReceivable, 150},
		{"achat de jetons", tx(token.KindPurchase, token.ReasonTopup, "", 10, "", nil), wallet("driver"), AccMobileMoney, AccAgentTokens, 10 * 100},
		{"jetons offerts", tx(token.KindPurchase, token.ReasonOperatorCredit, token.UnitToken, 5, "", nil), wallet("driver"), AccExpenseCredits, AccAgentTokens, 500},
		{"jeton consommé", tx(token.KindConsume, token.ReasonOrderAccept, token.UnitToken, 2, token.RefOrder, nil), wallet("driver"), AccAgentTokens, AccRevenueTokens, 200},
		{"jeton rendu", tx(token.KindPurchase, "order_accept_refund", token.UnitToken, 2, token.RefOrder, nil), wallet("driver"), AccRevenueTokens, AccAgentTokens, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := EntryFor(c.tx, c.w, 100)
			require.NotNil(t, e)
			assert.True(t, e.Balanced(), "équilibrée")
			assert.Equal(t, c.amount, e.Amount)
			require.Len(t, e.Lines, 2)
			assert.Equal(t, c.debit, e.Lines[0].Account)
			assert.Equal(t, c.amount, e.Lines[0].Debit)
			assert.Equal(t, c.credit, e.Lines[1].Account)
			assert.Equal(t, c.amount, e.Lines[1].Credit)
			assert.Equal(t, "TG", e.Country)
			assert.Equal(t, c.tx.ID, *e.TransactionID)
		})
	}
	// Un motif inconnu va « à classer », pas à la poubelle.
	e := EntryFor(tx(token.KindConsume, "mystery", token.UnitXOF, 42, "", nil), wallet("client"), 100)
	require.NotNil(t, e)
	assert.Equal(t, AccSuspense, e.Lines[1].Account)
	assert.Nil(t, EntryFor(&token.Transaction{Amount: 0}, nil, 100))
}

func TestVerticalEventsSplitARide(t *testing.T) {
	now := time.Now().UTC()
	online := EntriesForEvent(Event{Kind: "ride_settled", Country: "TG", Vertical: "vtc", RefKind: "ride", RefID: "r1", PaymentMethod: "wallet", FareXOF: 2000, CommissionXOF: 400, DriverXOF: 1600}, now)
	require.Len(t, online, 2)
	assert.Equal(t, AccRideClearing, online[0].Lines[0].Account)
	assert.Equal(t, AccAgentPayables, online[0].Lines[1].Account)
	assert.Equal(t, 1600, online[0].Amount)
	assert.Equal(t, AccRevenueCommission, online[1].Lines[1].Account)
	assert.Equal(t, 400, online[1].Amount)
	for _, e := range online {
		assert.True(t, e.Balanced())
		assert.Equal(t, "vtc", e.Service)
	}

	cash := EntriesForEvent(Event{Kind: "ride_settled", Country: "TG", Vertical: "vtc", PaymentMethod: "cash", FareXOF: 2000, CommissionXOF: 400, DriverXOF: 1600}, now)
	require.Len(t, cash, 1, "en espèces, seule la commission due est écrite")
	assert.Equal(t, AccAgentReceivable, cash[0].Lines[0].Account)
	assert.Equal(t, AccRevenueCommission, cash[0].Lines[1].Account)

	payout := EntriesForEvent(Event{Kind: "ledger_entry", Country: "TG", Vertical: "vtc", EntryKind: "payout", AmountXOF: -5000}, now)
	require.Len(t, payout, 1)
	assert.Equal(t, AccAgentPayables, payout[0].Lines[0].Account)
	assert.Equal(t, AccMobileMoney, payout[0].Lines[1].Account)
	assert.Equal(t, 5000, payout[0].Amount)
	assert.Empty(t, EntriesForEvent(Event{Kind: "ledger_entry", EntryKind: "tip", AmountXOF: 500}, now), "le pourboire est déjà au journal côté socle")
}

func TestBillingDefaultsAndValidation(t *testing.T) {
	d := DefaultBilling("TG")
	assert.Equal(t, ChargeCommission, d.Rides.AgentCharge)
	assert.Equal(t, ClientAtAccept, d.Rides.ClientChargeAt)
	assert.Equal(t, ChargeTokens, d.Deliveries.AgentCharge)
	assert.Equal(t, ClientAtRequest, d.Deliveries.ClientChargeAt)
	assert.NoError(t, validateVertical(VerticalBilling{AgentCharge: ChargeTokens, ClientChargeAt: ClientAtComplete, TokensPerRide: 2}, "rides"))
	assert.Error(t, validateVertical(VerticalBilling{AgentCharge: "gold", ClientChargeAt: ClientAtAccept}, "rides"))
	assert.Error(t, validateVertical(VerticalBilling{AgentCharge: ChargeCommission, CommissionPct: 120, ClientChargeAt: ClientAtAccept}, "rides"))
	assert.Error(t, validateVertical(VerticalBilling{AgentCharge: ChargeCommission, ClientChargeAt: "later"}, "deliveries"))
}
