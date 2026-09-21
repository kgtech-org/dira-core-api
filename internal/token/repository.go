package token

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// ErrDuplicateWallet is returned when a wallet already exists for an owner
// (unique index on owner_id). Fakes must reproduce this behaviour.
var ErrDuplicateWallet = errors.New("token: wallet already exists for owner")

// Journal reçoit chaque mouvement écrit, DANS la transaction qui l'écrit :
// l'écriture comptable et le mouvement vivent ou meurent ensemble. La
// comptabilité (`internal/finance`) l'implémente.
type Journal interface {
	Post(ctx context.Context, tx *Transaction, w *Wallet) error
}

// Repository implements Repo on MongoDB.
type Repository struct {
	mongo        *db.Mongo
	wallets      *mongo.Collection
	transactions *mongo.Collection
	operations   *mongo.Collection
	journal      Journal
}

// SetJournal branche la comptabilité. Sans elle, les mouvements s'écrivent
// sans écriture — le balayage d'intégrité le signale.
func (r *Repository) SetJournal(j Journal) { r.journal = j }

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{
		mongo:        m,
		wallets:      m.Collection(walletsCollection),
		transactions: m.Collection(transactionsCollection),
		operations:   m.Collection(operationsCollection),
	}
}

// CreateWallet inserts a new wallet. Returns ErrDuplicateWallet when a wallet
// already exists for the owner (unique index on owner_id).
func (r *Repository) CreateWallet(ctx context.Context, w *Wallet) error {
	res, err := r.wallets.InsertOne(ctx, w)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateWallet
		}
		return fmt.Errorf("token: create wallet: %w", err)
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		w.ID = id
	}
	return nil
}

// FindWalletByOwner returns the wallet for an owner, or (nil, nil) when none
// exists.
func (r *Repository) FindWalletByOwner(ctx context.Context, ownerID primitive.ObjectID) (*Wallet, error) {
	var w Wallet
	err := r.wallets.FindOne(ctx, bson.M{"owner_id": ownerID}).Decode(&w)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("token: find wallet by owner: %w", err)
	}
	return &w, nil
}

// ConsumeAtomic debits a wallet with a conditional update so the balance can
// never go negative. Returns false when the balance is insufficient
// (matchedCount == 0).
func (r *Repository) ConsumeAtomic(ctx context.Context, walletID primitive.ObjectID, amount int) (bool, error) {
	res, err := r.wallets.UpdateOne(ctx,
		bson.M{"_id": walletID, "balance": bson.M{"$gte": amount}},
		bson.M{
			"$inc": bson.M{"balance": -amount},
			"$set": bson.M{"updated_at": time.Now().UTC()},
		},
	)
	if err != nil {
		return false, fmt.Errorf("token: consume: %w", err)
	}
	return res.MatchedCount > 0, nil
}

// Credit increments a wallet balance atomically.
func (r *Repository) Credit(ctx context.Context, walletID primitive.ObjectID, amount int) error {
	res, err := r.wallets.UpdateOne(ctx,
		bson.M{"_id": walletID},
		bson.M{
			"$inc": bson.M{"balance": amount},
			"$set": bson.M{"updated_at": time.Now().UTC()},
		},
	)
	if err != nil {
		return fmt.Errorf("token: credit: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("token: credit: wallet %s not found", walletID.Hex())
	}
	return nil
}

// CreditField adds to ONE named balance of a wallet.
//
// Le champ est choisi par l'appelant plutôt que déduit d'un booléen : le
// portefeuille porte deux compteurs — jetons et argent — et un troisième
// viendrait sans que la signature ait à changer.
//
// Le montant peut être NÉGATIF : la contrainte de non-négativité appartient au
// jeton, pas à l'argent — refuser un solde négatif ici empêcherait de corriger
// une écriture erronée.
func (r *Repository) CreditField(ctx context.Context, walletID primitive.ObjectID, field string, amount int) error {
	switch field {
	case "balance", "balance_xof", "promo_xof":
	default:
		return fmt.Errorf("token: unknown balance field %q", field)
	}
	res, err := r.wallets.UpdateOne(ctx,
		bson.M{"_id": walletID},
		bson.M{
			"$inc": bson.M{field: amount},
			"$set": bson.M{"updated_at": time.Now().UTC()},
		},
	)
	if err != nil {
		return fmt.Errorf("token: credit %s: %w", field, err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("token: credit %s: wallet %s not found", field, walletID.Hex())
	}
	return nil
}

// AdjustDebt déplace la DETTE d'un portefeuille — positif l'augmente,
// négatif la rembourse, jamais en dessous de zéro.
func (r *Repository) AdjustDebt(ctx context.Context, walletID primitive.ObjectID, delta int) error {
	filter := bson.M{"_id": walletID}
	if delta < 0 {
		filter["debt_xof"] = bson.M{"$gte": -delta}
	}
	res, err := r.wallets.UpdateOne(ctx, filter, bson.M{"$inc": bson.M{"debt_xof": delta}, "$set": bson.M{"updated_at": time.Now().UTC()}})
	if err != nil {
		return fmt.Errorf("token: adjust debt: %w", err)
	}
	if res.MatchedCount == 0 {
		return fmt.Errorf("token: adjust debt: wallet %s not found or debt below %d", walletID.Hex(), -delta)
	}
	return nil
}

// InsertTransaction appends a wallet transaction.
func (r *Repository) InsertTransaction(ctx context.Context, t *Transaction) error {
	res, err := r.transactions.InsertOne(ctx, t)
	if err != nil {
		return fmt.Errorf("token: insert transaction: %w", err)
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		t.ID = id
	}
	if r.journal != nil {
		// Le portefeuille dit QUI (client, marchand, agent) et OÙ (pays) :
		// c'est ce qui décide des comptes de l'écriture.
		var w Wallet
		if err := r.wallets.FindOne(ctx, bson.M{"_id": t.WalletID}).Decode(&w); err != nil {
			return fmt.Errorf("token: wallet of transaction: %w", err)
		}
		if err := r.journal.Post(ctx, t, &w); err != nil {
			return fmt.Errorf("token: journal: %w", err)
		}
	}
	return nil
}

// ListTransactions returns one page of transactions for a wallet, most recent
// ids first is not used: cursor pagination follows _id ascending order.
// ListTransactionsNewest pages le grand livre du PLUS RÉCENT au plus ancien
// — ce qu'une fiche lit : les derniers mouvements d'abord. `ListTransactions`
// reste ascendant pour les applications qui rejouent un relevé.
func (r *Repository) ListTransactionsNewest(ctx context.Context, walletID primitive.ObjectID, limit int, cursor string) ([]Transaction, string, error) {
	filter := bson.M{"wallet_id": walletID}
	if cursor != "" {
		cursorID, err := primitive.ObjectIDFromHex(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("token: invalid cursor: %w", err)
		}
		filter["_id"] = bson.M{"$lt": cursorID}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit + 1))
	cur, err := r.transactions.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", fmt.Errorf("token: list transactions: %w", err)
	}
	var items []Transaction
	if err := cur.All(ctx, &items); err != nil {
		return nil, "", fmt.Errorf("token: decode transactions: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID.Hex()
	}
	return items, next, nil
}

func (r *Repository) ListTransactions(ctx context.Context, walletID primitive.ObjectID, limit int, cursor string) ([]Transaction, string, error) {
	filter := bson.M{"wallet_id": walletID}
	if cursor != "" {
		cursorID, err := primitive.ObjectIDFromHex(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("token: invalid cursor: %w", err)
		}
		filter["_id"] = bson.M{"$gt": cursorID}
	}

	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit + 1))
	cur, err := r.transactions.Find(ctx, filter, opts)
	if err != nil {
		return nil, "", fmt.Errorf("token: list transactions: %w", err)
	}
	var items []Transaction
	if err := cur.All(ctx, &items); err != nil {
		return nil, "", fmt.Errorf("token: decode transactions: %w", err)
	}

	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID.Hex()
	}
	return items, next, nil
}

// WithTransaction runs fn inside a MongoDB transaction.
func (r *Repository) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	return r.mongo.WithTransaction(ctx, fn)
}

// SpendMoney débite un portefeuille, le PROMOTIONNEL d'abord.
//
// Le promotionnel d'abord parce qu'un client qui paierait de sa poche en
// gardant un crédit offert finirait par ne jamais l'utiliser — et la
// plateforme aurait offert quelque chose que personne ne dépense.
//
// UN SEUL aller-retour, en pipeline atomique. Lire le solde puis écrire
// laisserait deux commandes simultanées passer toutes les deux : la condition
// et le débit doivent être la même opération, et c'est la base qui l'assure.
//
// Renvoie ce qui a été pris sur chaque poste : le grand livre doit distinguer
// ce que le client a payé de ce qu'on lui avait offert.
func (r *Repository) SpendMoney(ctx context.Context, walletID primitive.ObjectID, amount int) (fromPromo, fromCash int, err error) {
	if amount <= 0 {
		return 0, 0, fmt.Errorf("token: spend: amount must be positive")
	}
	var before Wallet
	err = r.wallets.FindOneAndUpdate(ctx,
		bson.M{
			"_id": walletID,
			// La condition porte sur la SOMME des deux postes : c'est ce dont
			// le client dispose réellement.
			"$expr": bson.M{"$gte": []any{
				bson.M{"$add": []any{
					bson.M{"$ifNull": []any{"$balance_xof", 0}},
					bson.M{"$ifNull": []any{"$promo_xof", 0}},
				}},
				amount,
			}},
		},
		mongo.Pipeline{
			{{Key: "$set", Value: bson.M{
				"__take_promo": bson.M{"$min": []any{
					bson.M{"$ifNull": []any{"$promo_xof", 0}}, amount,
				}},
			}}},
			{{Key: "$set", Value: bson.M{
				"promo_xof": bson.M{"$subtract": []any{
					bson.M{"$ifNull": []any{"$promo_xof", 0}}, "$__take_promo",
				}},
				"balance_xof": bson.M{"$subtract": []any{
					bson.M{"$ifNull": []any{"$balance_xof", 0}},
					bson.M{"$subtract": []any{amount, "$__take_promo"}},
				}},
				"updated_at": time.Now().UTC(),
			}}},
			{{Key: "$unset", Value: "__take_promo"}},
		},
		// AVANT modification : c'est le solde promotionnel d'avant qui dit
		// combien a été pris dessus.
		options.FindOneAndUpdate().SetReturnDocument(options.Before),
	).Decode(&before)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, 0, ErrInsufficientFunds
	}
	if err != nil {
		return 0, 0, fmt.Errorf("token: spend: %w", err)
	}
	fromPromo = before.PromoXOF
	if fromPromo > amount {
		fromPromo = amount
	}
	return fromPromo, amount - fromPromo, nil
}

// TakeMoney debits `balance_xof` by `amount`, or by what there is when
// `partial` — in ONE atomic operation, condition and debit together. Rend ce
// qui a été pris ; zéro quand le solde est vide (ou insuffisant sans partiel).
//
// L'argent réel seulement, jamais le promotionnel : une retenue de matériel
// n'est pas un achat sur la plateforme, et le crédit offert à un livreur
// n'est pas fait pour rembourser un gilet.
func (r *Repository) TakeMoney(ctx context.Context, walletID primitive.ObjectID, amount int, partial bool) (int, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("token: take: amount must be positive")
	}
	filter := bson.M{"_id": walletID}
	takeExpr := any(amount)
	if partial {
		filter["balance_xof"] = bson.M{"$gt": 0}
		takeExpr = bson.M{"$min": []any{bson.M{"$ifNull": []any{"$balance_xof", 0}}, amount}}
	} else {
		filter["balance_xof"] = bson.M{"$gte": amount}
	}
	var before Wallet
	err := r.wallets.FindOneAndUpdate(ctx, filter,
		mongo.Pipeline{
			{{Key: "$set", Value: bson.M{"__take": takeExpr}}},
			{{Key: "$set", Value: bson.M{
				"balance_xof": bson.M{"$subtract": []any{bson.M{"$ifNull": []any{"$balance_xof", 0}}, "$__take"}},
				"updated_at":  time.Now().UTC(),
			}}},
			{{Key: "$unset", Value: "__take"}},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.Before),
	).Decode(&before)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil // rien à prendre : pas un échec, un solde vide
	}
	if err != nil {
		return 0, fmt.Errorf("token: take money: %w", err)
	}
	if partial && before.BalanceXOF < amount {
		return before.BalanceXOF, nil
	}
	return amount, nil
}
