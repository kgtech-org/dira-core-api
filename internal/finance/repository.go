package finance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

type Repository struct {
	billing  *mongo.Collection
	entries  *mongo.Collection
	findings *mongo.Collection
	wallets  *mongo.Collection
	txs      *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{
		billing:  m.Collection(billingCollection),
		entries:  m.Collection(entriesCollection),
		findings: m.Collection(findingsCollection),
		// Lus en direct par le balayage : le grand livre des portefeuilles
		// est ce que l'on vérifie. Jamais écrits d'ici.
		wallets: m.Collection("token_wallets"),
		txs:     m.Collection("token_transactions"),
	}
}

// --- la facturation ---

func (r *Repository) Billing(ctx context.Context, country string) (*Billing, error) {
	var b Billing
	err := r.billing.FindOne(ctx, bson.M{"_id": country}).Decode(&b)
	if errors.Is(err, mongo.ErrNoDocuments) {
		d := DefaultBilling(country)
		return &d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finance: billing: %w", err)
	}
	return &b, nil
}

func (r *Repository) SaveBilling(ctx context.Context, b *Billing) error {
	_, err := r.billing.ReplaceOne(ctx, bson.M{"_id": b.Country}, b, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("finance: save billing: %w", err)
	}
	return nil
}

// --- le journal ---

var errUnbalanced = errors.New("finance: entry is not balanced")

func (r *Repository) InsertEntry(ctx context.Context, e *Entry) error {
	if !e.Balanced() {
		return errUnbalanced
	}
	if e.ID.IsZero() {
		e.ID = primitive.NewObjectID()
	}
	if _, err := r.entries.InsertOne(ctx, e); err != nil {
		return fmt.Errorf("finance: insert entry: %w", err)
	}
	return nil
}

func (r *Repository) InsertEntries(ctx context.Context, es []Entry) error {
	if len(es) == 0 {
		return nil
	}
	docs := make([]any, 0, len(es))
	for i := range es {
		if !es[i].Balanced() {
			return errUnbalanced
		}
		if es[i].ID.IsZero() {
			es[i].ID = primitive.NewObjectID()
		}
		docs = append(docs, es[i])
	}
	if _, err := r.entries.InsertMany(ctx, docs); err != nil {
		return fmt.Errorf("finance: insert entries: %w", err)
	}
	return nil
}

// EntryFilter : ce que la console demande au journal.
type EntryFilter struct {
	Country string
	Account string
	Reason  string
	Service string
	RefKind string
	RefID   string
	OwnerID string
	Unit    string
	From    time.Time
	To      time.Time
}

// query rend le filtre du journal, BORNÉ PAR LE PAYS de la requête.
//
// ⚠️ La borne est posée ici, par `country.Restrict`, et pas en recopiant
// `country.FromContext` dans le filtre : c'est le même geste que partout
// ailleurs sur la plateforme, et c'est celui que l'analyseur de frontière
// (`pkg/country/guard`) sait reconnaître. Un pays explicite — un rapport
// programmé qui compose le Sénégal depuis une tâche de fond — l'emporte.
func (f EntryFilter) query(ctx context.Context) bson.M {
	q := country.Restrict(ctx, bson.M{})
	if f.Country != "" {
		q[country.Field] = f.Country
	}
	if f.Account != "" {
		q["lines.account"] = f.Account
	}
	if f.Reason != "" {
		q["reason"] = f.Reason
	}
	if f.Service != "" {
		q["service"] = f.Service
	}
	if f.RefKind != "" {
		q["ref_kind"] = f.RefKind
	}
	if f.RefID != "" {
		q["ref_id"] = f.RefID
	}
	if f.OwnerID != "" {
		q["owner_id"] = f.OwnerID
	}
	if f.Unit != "" {
		q["unit"] = f.Unit
	}
	if !f.From.IsZero() || !f.To.IsZero() {
		at := bson.M{}
		if !f.From.IsZero() {
			at["$gte"] = f.From
		}
		if !f.To.IsZero() {
			at["$lt"] = f.To
		}
		q["at"] = at
	}
	return q
}

func (r *Repository) ListEntries(ctx context.Context, f EntryFilter, limit int, cursor string) ([]Entry, string, error) {
	q := f.query(ctx)
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			q["_id"] = bson.M{"$lt": cid}
		}
	}
	cur, err := r.entries.Find(ctx, q, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", fmt.Errorf("finance: list entries: %w", err)
	}
	var out []Entry
	if err := cur.All(ctx, &out); err != nil {
		return nil, "", fmt.Errorf("finance: list entries decode: %w", err)
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID.Hex()
	}
	if out == nil {
		out = []Entry{}
	}
	return out, next, nil
}

// AccountTotal : ce qu'un compte a reçu au débit et au crédit sur une période.
type AccountTotal struct {
	Account string `bson:"_id" json:"account"`
	Debit   int    `bson:"debit" json:"debit"`
	Credit  int    `bson:"credit" json:"credit"`
	Entries int    `bson:"entries" json:"entries"`
}

// Totals : la balance des comptes sur une période, par unité.
func (r *Repository) Totals(ctx context.Context, f EntryFilter) ([]AccountTotal, error) {
	cur, err := r.entries.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: f.query(ctx)}},
		{{Key: "$unwind", Value: "$lines"}},
		{{Key: "$group", Value: bson.M{
			"_id":     "$lines.account",
			"debit":   bson.M{"$sum": "$lines.debit"},
			"credit":  bson.M{"$sum": "$lines.credit"},
			"entries": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("finance: totals: %w", err)
	}
	var out []AccountTotal
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("finance: totals decode: %w", err)
	}
	return out, nil
}

// ReasonTotal : les montants par motif sur une période.
type ReasonTotal struct {
	Reason  string `bson:"_id" json:"reason"`
	Amount  int    `bson:"amount" json:"amount"`
	Entries int    `bson:"entries" json:"entries"`
}

func (r *Repository) ByReason(ctx context.Context, f EntryFilter) ([]ReasonTotal, error) {
	cur, err := r.entries.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: f.query(ctx)}},
		{{Key: "$group", Value: bson.M{"_id": "$reason", "amount": bson.M{"$sum": "$amount"}, "entries": bson.M{"$sum": 1}}}},
		{{Key: "$sort", Value: bson.D{{Key: "amount", Value: -1}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("finance: by reason: %w", err)
	}
	var out []ReasonTotal
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("finance: by reason decode: %w", err)
	}
	return out, nil
}

// DayTotal : une journée du journal (produits, charges, encaissé).
type DayTotal struct {
	Day     string `bson:"_id" json:"day"`
	Revenue int    `bson:"revenue" json:"revenue"`
	Expense int    `bson:"expense" json:"expense"`
	Entries int    `bson:"entries" json:"entries"`
}

func (r *Repository) ByDay(ctx context.Context, f EntryFilter) ([]DayTotal, error) {
	revenue := bson.A{AccRevenueCommission, AccRevenueTokens, AccRevenueEquipment, AccRevenueFees}
	expense := bson.A{AccExpensePromo, AccExpenseCredits, AccExpenseWriteoff}
	cur, err := r.entries.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: f.query(ctx)}},
		{{Key: "$unwind", Value: "$lines"}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{"$dateToString": bson.M{"format": "%Y-%m-%d", "date": "$at"}},
			"revenue": bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$in": bson.A{"$lines.account", revenue}},
				bson.M{"$subtract": bson.A{"$lines.credit", "$lines.debit"}}, 0}}},
			"expense": bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$in": bson.A{"$lines.account", expense}},
				bson.M{"$subtract": bson.A{"$lines.debit", "$lines.credit"}}, 0}}},
			"entries": bson.M{"$sum": 1},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("finance: by day: %w", err)
	}
	var out []DayTotal
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("finance: by day decode: %w", err)
	}
	return out, nil
}

// --- ce que le balayage lit ---

// WalletSnapshot : un portefeuille tel qu'il est stocké.
type WalletSnapshot struct {
	ID         primitive.ObjectID `bson:"_id"`
	OwnerID    primitive.ObjectID `bson:"owner_id"`
	Type       string             `bson:"type"`
	Country    string             `bson:"country"`
	Balance    int                `bson:"balance"`
	BalanceXOF int                `bson:"balance_xof"`
	PromoXOF   int                `bson:"promo_xof"`
	DebtXOF    int                `bson:"debt_xof"`
}

func (r *Repository) Wallets(ctx context.Context, fn func(w WalletSnapshot) error) error {
	cur, err := r.wallets.Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("finance: wallets: %w", err)
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		var w WalletSnapshot
		if err := cur.Decode(&w); err != nil {
			return err
		}
		if err := fn(w); err != nil {
			return err
		}
	}
	return cur.Err()
}

// WalletSums : ce que les mouvements d'un portefeuille disent de ses soldes.
type WalletSums struct {
	WalletID primitive.ObjectID `bson:"_id"`
	Tokens   int                `bson:"tokens"`
	XOF      int                `bson:"xof"`
	Promo    int                `bson:"promo"`
	Debt     int                `bson:"debt"`
	Count    int                `bson:"count"`
	LastAt   time.Time          `bson:"last_at"`
}

// SumTransactions recalcule les trois soldes de CHAQUE portefeuille depuis
// le grand livre — la définition du solde ; le champ stocké n'en est que le
// cache.
func (r *Repository) SumTransactions(ctx context.Context) (map[primitive.ObjectID]WalletSums, error) {
	// Le SENS vient de `kind`, jamais du signe stocké : les jetons consommés
	// sont écrits en négatif, les francs débités en positif. Prendre
	// `-amount` d'un débit de −2 jetons donnait +2 — et un constat
	// `wallet_drift` sur chaque livreur qui avait accepté une commande.
	abs := bson.M{"$abs": "$amount"}
	signed := bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{"$kind", "purchase"}}, abs, bson.M{"$multiply": bson.A{abs, -1}}}}
	isToken := bson.M{"$or": bson.A{bson.M{"$eq": bson.A{"$unit", "token"}}, bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$unit", ""}}, ""}}}}
	isPromo := bson.M{"$or": bson.A{
		bson.M{"$eq": bson.A{"$reason", "promo_credit"}},
		bson.M{"$eq": bson.A{"$ref.source", "promo"}},
	}}
	// Un mouvement de DETTE porte `ref.debt_delta` ; `ref.no_balance` dit
	// qu'il n'a pas touché le solde (commission portée à la dette, règlement
	// à l'agence).
	touchesBalance := bson.M{"$ne": bson.A{"$ref.no_balance", true}}
	cur, err := r.txs.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.M{
			"_id":     "$wallet_id",
			"tokens":  bson.M{"$sum": bson.M{"$cond": bson.A{isToken, signed, 0}}},
			"promo":   bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$and": bson.A{bson.M{"$not": isToken}, isPromo}}, signed, 0}}},
			"xof":     bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$and": bson.A{bson.M{"$not": isToken}, bson.M{"$not": isPromo}, touchesBalance}}, signed, 0}}},
			"debt":    bson.M{"$sum": bson.M{"$ifNull": bson.A{"$ref.debt_delta", 0}}},
			"count":   bson.M{"$sum": 1},
			"last_at": bson.M{"$max": "$created_at"},
		}}},
	})
	if err != nil {
		return nil, fmt.Errorf("finance: sum transactions: %w", err)
	}
	out := map[primitive.ObjectID]WalletSums{}
	for cur.Next(ctx) {
		var s WalletSums
		if err := cur.Decode(&s); err != nil {
			return nil, err
		}
		out[s.WalletID] = s
	}
	return out, cur.Err()
}

// MissingEntries : les mouvements écrits après `since` qui n'ont pas
// d'écriture au journal, par portefeuille.
func (r *Repository) MissingEntries(ctx context.Context, since time.Time) (map[primitive.ObjectID]int, error) {
	cur, err := r.txs.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"created_at": bson.M{"$gte": since}}}},
		{{Key: "$lookup", Value: bson.M{"from": entriesCollection, "localField": "_id", "foreignField": "transaction_id", "as": "entry"}}},
		{{Key: "$match", Value: bson.M{"entry": bson.M{"$size": 0}}}},
		{{Key: "$group", Value: bson.M{"_id": "$wallet_id", "n": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("finance: missing entries: %w", err)
	}
	out := map[primitive.ObjectID]int{}
	for cur.Next(ctx) {
		var row struct {
			ID primitive.ObjectID `bson:"_id"`
			N  int                `bson:"n"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}
		out[row.ID] = row.N
	}
	return out, cur.Err()
}

// FirstEntryAt : la date de la première écriture — la mise en service
// effective du journal sur cette base.
func (r *Repository) FirstEntryAt(ctx context.Context) (time.Time, bool) {
	var row struct {
		At time.Time `bson:"at"`
	}
	err := r.entries.FindOne(ctx, bson.M{}, options.FindOne().SetSort(bson.D{{Key: "at", Value: 1}}).SetProjection(bson.M{"at": 1})).Decode(&row)
	if err != nil {
		return time.Time{}, false
	}
	return row.At, true
}

// UnbalancedEntries : les écritures dont les débits ≠ crédits.
func (r *Repository) UnbalancedEntries(ctx context.Context) ([]primitive.ObjectID, int, error) {
	cur, err := r.entries.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$project", Value: bson.M{"d": bson.M{"$sum": "$lines.debit"}, "c": bson.M{"$sum": "$lines.credit"}}}},
		{{Key: "$match", Value: bson.M{"$expr": bson.M{"$ne": bson.A{"$d", "$c"}}}}},
		{{Key: "$limit", Value: 50}},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("finance: unbalanced: %w", err)
	}
	var ids []primitive.ObjectID
	for cur.Next(ctx) {
		var row struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		if err := cur.Decode(&row); err != nil {
			return nil, 0, err
		}
		ids = append(ids, row.ID)
	}
	total, err := r.entries.CountDocuments(ctx, bson.M{})
	return ids, int(total), err
}

// --- les constats ---

// Upsert : dédoublonne par empreinte. Rend vrai si le constat est NOUVEAU.
func (r *Repository) UpsertFinding(ctx context.Context, f *Finding, now time.Time) (bool, error) {
	var existing Finding
	err := r.findings.FindOne(ctx, bson.M{"fingerprint": f.Fingerprint, "status": bson.M{"$ne": FindingResolved}}).Decode(&existing)
	if errors.Is(err, mongo.ErrNoDocuments) {
		f.ID = primitive.NewObjectID()
		f.Status = FindingOpen
		f.FirstSeenAt, f.LastSeenAt, f.Occurrences = now, now, 1
		if _, err := r.findings.InsertOne(ctx, f); err != nil {
			return false, fmt.Errorf("finance: insert finding: %w", err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("finance: finding: %w", err)
	}
	_, err = r.findings.UpdateByID(ctx, existing.ID, bson.M{
		"$set": bson.M{"last_seen_at": now, "expected": f.Expected, "actual": f.Actual, "detail": f.Detail, "severity": f.Severity},
		"$inc": bson.M{"occurrences": 1},
	})
	f.ID = existing.ID
	return false, err
}

// ResolveUnseen ferme les constats qui n'ont pas été revus lors de ce
// passage : l'écart a disparu.
func (r *Repository) ResolveUnseen(ctx context.Context, seen map[string]bool, now time.Time) (int, error) {
	cur, err := r.findings.Find(ctx, bson.M{"status": bson.M{"$ne": FindingResolved}}, options.Find().SetProjection(bson.M{"fingerprint": 1}))
	if err != nil {
		return 0, fmt.Errorf("finance: findings: %w", err)
	}
	var stale []primitive.ObjectID
	for cur.Next(ctx) {
		var row struct {
			ID          primitive.ObjectID `bson:"_id"`
			Fingerprint string             `bson:"fingerprint"`
		}
		if err := cur.Decode(&row); err != nil {
			return 0, err
		}
		if !seen[row.Fingerprint] {
			stale = append(stale, row.ID)
		}
	}
	if len(stale) == 0 {
		return 0, nil
	}
	res, err := r.findings.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": stale}}, bson.M{"$set": bson.M{"status": FindingResolved, "last_seen_at": now}})
	if err != nil {
		return 0, err
	}
	return int(res.ModifiedCount), nil
}

func (r *Repository) ListFindings(ctx context.Context, status, country string, limit int) ([]Finding, error) {
	q := bson.M{}
	if status != "" {
		q["status"] = status
	} else {
		q["status"] = bson.M{"$ne": FindingResolved}
	}
	if country != "" {
		q["$or"] = bson.A{bson.M{"country": country}, bson.M{"country": ""}}
	}
	cur, err := r.findings.Find(ctx, q, options.Find().SetSort(bson.D{{Key: "last_seen_at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("finance: list findings: %w", err)
	}
	out := []Finding{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) AckFinding(ctx context.Context, id primitive.ObjectID, by, note string, now time.Time) (*Finding, error) {
	var f Finding
	err := r.findings.FindOneAndUpdate(ctx, bson.M{"_id": id},
		bson.M{"$set": bson.M{"status": FindingAcknowledged, "acked_by": by, "acked_at": now, "note": note}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&f)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, errFindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("finance: ack finding: %w", err)
	}
	return &f, nil
}

// EnsureIndexes : les index du module.
func (r *Repository) EnsureIndexes(ctx context.Context) error {
	_, err := r.entries.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "country", Value: 1}, {Key: "at", Value: -1}}},
		{Keys: bson.D{{Key: "lines.account", Value: 1}, {Key: "at", Value: -1}}},
		{Keys: bson.D{{Key: "transaction_id", Value: 1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "ref_kind", Value: 1}, {Key: "ref_id", Value: 1}}},
		{Keys: bson.D{{Key: "owner_id", Value: 1}, {Key: "at", Value: -1}}},
	})
	if err != nil {
		return err
	}
	_, err = r.findings.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "fingerprint", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "last_seen_at", Value: -1}}},
	})
	return err
}
