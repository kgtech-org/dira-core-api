// Package token manages token wallets and their transactions: the economic
// core of the platform. Drivers spend tokens to accept orders; merchants
// spend store-wallet tokens to boost dishes, buy options or generate videos.
package token

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection names.
const (
	walletsCollection      = "token_wallets"
	transactionsCollection = "token_transactions"
)

// Wallet types.
const (
	WalletTypeDriver   = "driver"   // owner_id references a user (driver)
	WalletTypeMerchant = "merchant" // owner_id references a store (one wallet per store)
	// WalletTypeClient est le portefeuille D'ARGENT d'un client — « Dira
	// Cash ». Il ne porte AUCUN jeton : les jetons sont le droit d'entrée
	// d'un livreur et l'outil de promotion d'un marchand, un client n'en a
	// aucun usage. `Balance` y reste donc à zéro, et c'est `BalanceXOF` qui
	// vit.
	WalletTypeClient = "client"
)

// Transaction kinds.
const (
	KindPurchase = "purchase"
	KindConsume  = "consume"
)

// Transaction reasons.
const (
	ReasonOrderAccept = "order_accept"
	ReasonBoostDish   = "boost_dish"
	ReasonBuyOption   = "buy_option"
	ReasonAIVideo     = "ai_video"
	ReasonTopup       = "topup"
	// ReasonOperatorCredit marque un crédit accordé par un administrateur SANS
	// paiement (geste commercial, compensation, litige). Motif distinct de
	// `topup` exprès : ces mouvements doivent pouvoir être isolés du chiffre
	// d'affaires réel dans le grand livre.
	ReasonOperatorCredit = "operator_credit"

	// --- mouvements d'ARGENT (unité XOF) ---------------------------------
	//
	// Le grand livre porte désormais deux unités. Chaque mouvement dit
	// LAQUELLE il déplace : sans cela, un relevé additionnerait des jetons et
	// des francs, et personne ne saurait plus ce que vaut un solde.

	// ReasonOrderPayout crédite le marchand du produit de ses lignes, au
	// moment où le livreur retire la commande. Pas à la commande : tant que
	// rien n'est retiré, rien n'a été vendu.
	ReasonOrderPayout = "order_payout"
	// ReasonDeliveryFee verse au livreur ses frais de livraison, à la fin de
	// la course et pour une commande payée EN LIGNE seulement. En espèces il
	// les a déjà en main : la plateforme n'a rien à lui verser, et il ne lui
	// doit rien.
	ReasonDeliveryFee = "delivery_fee"
	// ReasonWalletTopup crédite le portefeuille d'un client après confirmation
	// d'un paiement mobile money.
	ReasonWalletTopup = "wallet_topup"
	// ReasonPayment débite le portefeuille d'un client. Ce qu'il paie est dit
	// par `ref_kind` : une commande de repas, une course.
	ReasonPayment = "payment"
	// ReasonPromoCredit crédite le SOLDE PROMOTIONNEL — un geste commercial,
	// dépensé avant l'argent réel. Motif distinct de `wallet_topup` exprès :
	// ce que la plateforme offre ne doit jamais se confondre, au grand livre,
	// avec ce qu'un client a payé.
	ReasonPromoCredit = "promo_credit"
	// ReasonRefund rend au portefeuille ce qu'une commande ou une course
	// annulée avait pris.
	ReasonRefund = "refund"
)

// Ce à quoi un mouvement se rattache.
const (
	RefOrder = "order"
	RefRide  = "ride"
)

// Unités du grand livre.
//
// UnitToken est la valeur par défaut, et l'absence du champ la vaut : tous les
// mouvements écrits avant l'arrivée de l'argent sont des jetons, et aucune
// migration n'est nécessaire.
const (
	UnitToken = "token"
	UnitXOF   = "xof"
)

// Wallet is a token wallet. Balance is always >= 0, enforced by conditional
// atomic updates (never read-then-write).
type Wallet struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	OwnerID primitive.ObjectID `bson:"owner_id"` // user id (driver) or store id (merchant)
	Type    string             `bson:"type"`     // "driver" | "merchant"
	Balance int                `bson:"balance"`  // JETONS
	// BalanceXOF est le solde en ARGENT : produit des ventes pour un marchand,
	// commissions pour un livreur.
	//
	// Un SECOND solde, et non le même entier : mélanger jetons et francs
	// rendrait tout relevé illisible et tout retrait ambigu — « solde 42 »
	// ne dirait plus si l'on peut propulser un plat ou retirer 42 F.
	//
	// `omitempty` : son absence vaut zéro, donc aucun portefeuille existant
	// n'a besoin d'être touché.
	BalanceXOF int `bson:"balance_xof,omitempty"`
	// PromoXOF est ce que la PLATEFORME a offert : geste commercial,
	// compensation, campagne. Dépensé AVANT l'argent réel — sinon un client
	// paierait de sa poche en gardant un crédit offert qu'il finirait par ne
	// jamais utiliser.
	//
	// Séparé de BalanceXOF et non additionné : les deux ne se remboursent pas
	// pareil. Rendre de l'argent qu'on n'a jamais reçu serait une perte
	// sèche, et l'écran doit pouvoir dire « dont X offerts ».
	PromoXOF  int       `bson:"promo_xof,omitempty"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Transaction is an append-only wallet movement. Amount is positive for
// purchases (credits) and negative for consumptions (debits).
type Transaction struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	WalletID primitive.ObjectID `bson:"wallet_id"`
	Kind     string             `bson:"kind"`   // "purchase" | "consume"
	Reason   string             `bson:"reason"` // voir les constantes Reason*
	Amount   int                `bson:"amount"`
	// Unit dit QUELLE unité ce mouvement déplace. Absent = jeton : c'est ce
	// que sont tous les mouvements antérieurs à l'argent.
	Unit string `bson:"unit,omitempty"`
	// RefID et RefKind disent À QUOI ce mouvement se rattache : une commande
	// de repas, une course, rien du tout.
	//
	// ⚠️ Le champ s'appelait `order_id`. La même mécanique sert désormais deux
	// verticales, et un nom qui désigne une seule des deux aurait obligé les
	// courses à s'écrire « commande » au grand livre — c'est-à-dire à mentir
	// sur ce que l'argent a payé.
	//
	// Le COUPLE fait l'identité : deux verticales peuvent porter le même
	// identifiant sans se confondre, et un rapport financier se ventile par
	// `ref_kind` sans avoir à deviner.
	RefID     *primitive.ObjectID `bson:"ref_id,omitempty"`
	RefKind   string              `bson:"ref_kind,omitempty"`
	Ref       map[string]any      `bson:"ref,omitempty"`
	CreatedAt time.Time           `bson:"created_at"`
}
