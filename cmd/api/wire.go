package main

import (
	"context"

	"github.com/kgtech-org/dira-core-api/internal/payment"
	"github.com/kgtech-org/dira-core-api/internal/staff"
	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/internal/user"
)

// backOffice réunit les LECTURES financières de deux modules en une seule
// dépendance pour la surface de service.
//
// C'est une TRADUCTION, pas un module : elle ne décide de rien, ne stocke
// rien, et ne fait que router trois appels vers le dépôt qui les sert. Elle
// existe parce que les portefeuilles et les paiements sont deux modules, alors
// que le back-office les lit d'un même écran — et parce que `serviceapi` ne
// doit pas avoir à connaître deux dépôts pour servir trois listes.
//
// Aucun champ n'est recopié ici : les lignes traversent TELLES QUELLES. Un
// champ ajouté à un portefeuille arrive au back-office sans qu'on touche à ce
// fichier.
// staffEntitlements adapte le staff à ce que les comptes en attendent.
type staffEntitlements struct{ svc *staff.Service }

func (a staffEntitlements) EntitlementsOf(ctx context.Context, userID string) (user.Entitlements, error) {
	scopes, direction, err := a.svc.EntitlementsOf(ctx, userID)
	return user.Entitlements{Scopes: scopes, Direction: direction}, err
}

type backOffice struct {
	tokens   *token.Repository
	payments *payment.Repository
}

func (b backOffice) ListWallets(ctx context.Context, walletType, ownerID, cursor string, limit int) ([]token.WalletRow, string, error) {
	return b.tokens.ListWallets(ctx, walletType, ownerID, cursor, limit)
}

func (b backOffice) ListLedger(ctx context.Context, walletID, cursor string, limit int) ([]token.LedgerRow, string, error) {
	return b.tokens.ListLedger(ctx, walletID, cursor, limit)
}

func (b backOffice) ListPayments(ctx context.Context, status, purpose, cursor string, limit int) ([]payment.PaymentRow, string, error) {
	return b.payments.List(ctx, status, purpose, cursor, limit)
}

func (b backOffice) RefundOrderPayment(ctx context.Context, orderID string) (string, error) {
	return b.payments.RefundOrderPayment(ctx, orderID)
}
