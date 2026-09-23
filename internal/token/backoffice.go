package token

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Ce fichier sert les LECTURES du back-office : la liste des portefeuilles et
// le grand livre des mouvements.
//
// ⚠️ Ces lignes sortent BRUTES — un `owner_id`, pas un nom. Le socle ne sait
// pas ce qu'est un point de vente ; il stocke un identifiant opaque. C'est la
// verticale qui possède l'objet désigné, et c'est donc elle qui le nomme.
// Faire autrement obligerait le socle à connaître les collections de chaque
// verticale, et l'ajout d'une verticale à modifier le socle.

// WalletRow is one wallet, as the back-office reads it.
type WalletRow struct {
	ID         string    `json:"id"`
	OwnerID    string    `json:"owner_id"`
	Type       string    `json:"type"`
	Country    string    `json:"country,omitempty"`
	Balance    int       `json:"balance"`
	BalanceXOF int       `json:"balance_xof"`
	PromoXOF   int       `json:"promo_xof"`
	DebtXOF    int       `json:"debt_xof"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// LedgerRow is one wallet movement — tokens or money.
type LedgerRow struct {
	ID       string `json:"id"`
	WalletID string `json:"wallet_id"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	Amount   int    `json:"amount"`
	// Unit dit ce que le mouvement déplace : `token` ou `xof`.
	Unit    string         `json:"unit"`
	RefID   string         `json:"ref_id,omitempty"`
	RefKind string         `json:"ref_kind,omitempty"`
	Ref     map[string]any `json:"ref,omitempty"`
	// Le portefeuille, quand la liste croise plusieurs portefeuilles.
	OwnerID   string    `json:"owner_id,omitempty"`
	OwnerType string    `json:"owner_type,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// LedgerFilter : ce que la console demande au grand livre des portefeuilles.
type LedgerFilter struct {
	WalletID  string
	OwnerType string
	Unit      string
	Reason    string
	RefKind   string
}

// ListLedgerFiltered pages les mouvements de TOUS les portefeuilles du pays,
// avec le portefeuille de chacun — la vue « Opérations » de la console.
func (r *Repository) ListLedgerFiltered(ctx context.Context, f LedgerFilter, cursor string, limit int) ([]LedgerRow, string, error) {
	limit = pageLimit(limit)
	match := bson.M{}
	if f.WalletID != "" {
		wid, err := primitive.ObjectIDFromHex(f.WalletID)
		if err != nil {
			return nil, "", apperr.Validation("invalid wallet id").WithCause(err)
		}
		match["wallet_id"] = wid
	}
	if f.Unit == UnitToken {
		match["$or"] = bson.A{bson.M{"unit": UnitToken}, bson.M{"unit": bson.M{"$exists": false}}}
	} else if f.Unit != "" {
		match["unit"] = f.Unit
	}
	if f.Reason != "" {
		match["reason"] = f.Reason
	}
	if f.RefKind != "" {
		match["ref_kind"] = f.RefKind
	}
	walletMatch := country.Restrict(ctx, bson.M{})
	if f.OwnerType != "" {
		walletMatch["type"] = f.OwnerType
	}
	wm := bson.M{}
	for k, v := range walletMatch {
		wm["wallet."+k] = v
	}
	cur, err := r.transactions.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: beforeCursor(match, cursor)}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: -1}}}},
		{{Key: "$lookup", Value: bson.M{"from": walletsCollection, "localField": "wallet_id", "foreignField": "_id", "as": "wallet"}}},
		{{Key: "$unwind", Value: "$wallet"}},
		{{Key: "$match", Value: wm}},
		{{Key: "$limit", Value: limit + 1}},
	})
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	var docs []struct {
		ID        primitive.ObjectID  `bson:"_id"`
		WalletID  primitive.ObjectID  `bson:"wallet_id"`
		Kind      string              `bson:"kind"`
		Reason    string              `bson:"reason"`
		Amount    int                 `bson:"amount"`
		Unit      string              `bson:"unit"`
		RefID     *primitive.ObjectID `bson:"ref_id,omitempty"`
		RefKind   string              `bson:"ref_kind,omitempty"`
		Ref       map[string]any      `bson:"ref,omitempty"`
		CreatedAt time.Time           `bson:"created_at"`
		Wallet    struct {
			OwnerID primitive.ObjectID `bson:"owner_id"`
			Type    string             `bson:"type"`
		} `bson:"wallet"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", apperr.Internal(err)
	}
	next := ""
	if len(docs) > limit {
		docs = docs[:limit]
		next = docs[limit-1].ID.Hex()
	}
	out := make([]LedgerRow, 0, len(docs))
	for _, d := range docs {
		unit := d.Unit
		if unit == "" {
			unit = UnitToken
		}
		// Toujours positif : `kind` dit le sens. Les jetons consommés sont
		// stockés en négatif, les francs débités en positif — la console
		// applique le signe elle-même et ne doit pas le recevoir deux fois.
		amount := d.Amount
		if amount < 0 {
			amount = -amount
		}
		row := LedgerRow{
			ID: d.ID.Hex(), WalletID: d.WalletID.Hex(), Kind: d.Kind, Reason: d.Reason, Amount: amount, Unit: unit,
			RefKind: d.RefKind, Ref: d.Ref, OwnerID: d.Wallet.OwnerID.Hex(), OwnerType: d.Wallet.Type, CreatedAt: d.CreatedAt,
		}
		if d.RefID != nil {
			row.RefID = d.RefID.Hex()
		}
		out = append(out, row)
	}
	return out, next, nil
}

// pageLimit borne ce qu'une verticale peut demander d'un coup. Une page non
// bornée transformerait une vue d'administration en export complet de la
// table des mouvements.
func pageLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}

// beforeCursor traduit un curseur opaque en borne sur `_id`. Un curseur
// ILLISIBLE est ignoré plutôt que refusé : il rend la première page, ce qui
// est le comportement attendu d'une pagination qu'on relance.
func beforeCursor(filter bson.M, cursor string) bson.M {
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			filter["_id"] = bson.M{"$lt": cid}
		}
	}
	return filter
}

// ListWallets pages wallets, newest first, filtered by type and/or owner.
//
// Le filtre par PROPRIÉTAIRE sert la fiche d'une personne : c'est un champ sur
// une requête existante plutôt qu'une route « solde de quelqu'un » de plus.
// Une surface de service se garde étroite en n'y ajoutant que ce qu'on ne peut
// pas exprimer avec ce qui existe.
func (r *Repository) ListWallets(ctx context.Context, walletType, ownerID, cursor string, limit int) ([]WalletRow, string, error) {
	limit = pageLimit(limit)
	filter := country.Restrict(ctx, bson.M{})
	if walletType != "" {
		filter["type"] = walletType
	}
	if ownerID != "" {
		oid, err := primitive.ObjectIDFromHex(ownerID)
		if err != nil {
			return nil, "", apperr.Validation("invalid owner id").WithCause(err)
		}
		filter["owner_id"] = oid
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit + 1))
	cur, err := r.wallets.Find(ctx, beforeCursor(filter, cursor), opts)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	var docs []Wallet
	if err := cur.All(ctx, &docs); err != nil {
		return nil, "", apperr.Internal(err)
	}
	next := ""
	if len(docs) > limit {
		docs = docs[:limit]
		next = docs[limit-1].ID.Hex()
	}
	out := make([]WalletRow, 0, len(docs))
	for _, d := range docs {
		out = append(out, WalletRow{
			ID: d.ID.Hex(), OwnerID: d.OwnerID.Hex(), Type: d.Type, Country: d.Country,
			Balance: d.Balance, BalanceXOF: d.BalanceXOF, PromoXOF: d.PromoXOF, DebtXOF: d.DebtXOF, UpdatedAt: d.UpdatedAt,
		})
	}
	return out, next, nil
}

// ListLedger pages token movements, newest first, optionally for one wallet.
//
// ⚠️ LE MOUVEMENT NE PORTE PAS DE PAYS, son PORTEFEUILLE si. Cette liste
// balayait donc les mouvements de toute la plateforme dès qu'on ne nommait
// pas un portefeuille — la surface de service que la livraison interroge
// pour son écran « jetons ». Elle passe par la même lecture que la console,
// qui joint le portefeuille et borne sur son pays.
func (r *Repository) ListLedger(ctx context.Context, walletID, cursor string, limit int) ([]LedgerRow, string, error) {
	return r.ListLedgerFiltered(ctx, LedgerFilter{WalletID: walletID}, cursor, limit)
}
