package equipment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

var (
	errItemNotFound     = apperr.NotFound("equipment_item_not_found", "equipment item not found")
	errContractNotFound = apperr.NotFound("equipment_contract_not_found", "equipment contract not found")
)

type Repository struct {
	items     *mongo.Collection
	contracts *mongo.Collection
	settings  *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{
		items:     m.Collection(collectionItems),
		contracts: m.Collection(collectionContracts),
		settings:  m.Collection(collectionSettings),
	}
}

// Indexes de ce module — posés par `internal/indexes`.
func Indexes() []db.Index { // recopiés dans internal/indexes (la table de vérité)
	return []db.Index{
		{Collection: collectionItems, Keys: db.K("country", 1, "active", 1, "_id", -1)},
		{Collection: collectionContracts, Keys: db.K("user_id", 1, "status", 1, "_id", 1)},
		{Collection: collectionContracts, Keys: db.K("country", 1, "status", 1, "_id", -1)},
		{Collection: collectionContracts, Keys: db.K("status", 1, "next_period_at", 1)},
		{Collection: collectionContracts, Keys: db.K("item_id", 1, "_id", -1)},
	}
}

// --- items ---

func (r *Repository) InsertItem(ctx context.Context, it *Item) error {
	it.ID = primitive.NewObjectID()
	now := time.Now().UTC()
	it.CreatedAt, it.UpdatedAt = now, now
	if _, err := r.items.InsertOne(ctx, it); err != nil {
		return fmt.Errorf("equipment: insert item: %w", err)
	}
	return nil
}

func (r *Repository) ItemByID(ctx context.Context, id primitive.ObjectID) (*Item, error) {
	var it Item
	if err := r.items.FindOne(ctx, country.Restrict(ctx, bson.M{"_id": id})).Decode(&it); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errItemNotFound
		}
		return nil, fmt.Errorf("equipment: find item: %w", err)
	}
	return &it, nil
}

// ListItems : le catalogue du pays ; `onlyActive` pour les applications.
func (r *Repository) ListItems(ctx context.Context, onlyActive bool) ([]Item, error) {
	filter := country.Restrict(ctx, bson.M{})
	if onlyActive {
		filter["active"] = true
	}
	cur, err := r.items.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "kind", Value: 1}, {Key: "name", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("equipment: list items: %w", err)
	}
	var out []Item
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("equipment: list items decode: %w", err)
	}
	return out, nil
}

func (r *Repository) SaveItem(ctx context.Context, it *Item) error {
	it.UpdatedAt = time.Now().UTC()
	res, err := r.items.ReplaceOne(ctx, bson.M{"_id": it.ID}, it)
	if err != nil {
		return fmt.Errorf("equipment: save item: %w", err)
	}
	if res.MatchedCount == 0 {
		return errItemNotFound
	}
	return nil
}

// AdjustStock décrémente (ou incrémente) le stock d'un article suivi, en
// refusant de passer sous zéro — une seule opération, condition et
// décrément ensemble.
func (r *Repository) AdjustStock(ctx context.Context, id primitive.ObjectID, delta int) error {
	filter := bson.M{"_id": id, "track_stock": true}
	if delta < 0 {
		filter["stock"] = bson.M{"$gte": -delta}
	}
	res, err := r.items.UpdateOne(ctx, filter, bson.M{"$inc": bson.M{"stock": delta}})
	if err != nil {
		return fmt.Errorf("equipment: adjust stock: %w", err)
	}
	if res.MatchedCount == 0 && delta < 0 {
		// Soit non suivi (rien à faire), soit épuisé : relire pour le dire.
		var it Item
		if err := r.items.FindOne(ctx, bson.M{"_id": id}).Decode(&it); err == nil && it.TrackStock {
			return apperr.Conflict("equipment_out_of_stock", "this item is out of stock")
		}
	}
	return nil
}

// --- contracts ---

func (r *Repository) InsertContract(ctx context.Context, c *Contract) error {
	c.ID = primitive.NewObjectID()
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	if c.Schedule == nil {
		c.Schedule = []Line{}
	}
	if c.Payments == nil {
		c.Payments = []Payment{}
	}
	if _, err := r.contracts.InsertOne(ctx, c); err != nil {
		return fmt.Errorf("equipment: insert contract: %w", err)
	}
	return nil
}

func (r *Repository) ContractByID(ctx context.Context, id primitive.ObjectID) (*Contract, error) {
	var c Contract
	if err := r.contracts.FindOne(ctx, country.Restrict(ctx, bson.M{"_id": id})).Decode(&c); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errContractNotFound
		}
		return nil, fmt.Errorf("equipment: find contract: %w", err)
	}
	return &c, nil
}

// SaveContract replaces the document — the contract is small and its
// schedule and payments are one unit of consistency.
func (r *Repository) SaveContract(ctx context.Context, c *Contract) error {
	c.UpdatedAt = time.Now().UTC()
	res, err := r.contracts.ReplaceOne(ctx, bson.M{"_id": c.ID}, c)
	if err != nil {
		return fmt.Errorf("equipment: save contract: %w", err)
	}
	if res.MatchedCount == 0 {
		return errContractNotFound
	}
	return nil
}

// ContractFilter narrows a listing.
type ContractFilter struct {
	UserID   *primitive.ObjectID
	Status   string
	Vertical string
	ItemID   *primitive.ObjectID
}

func (r *Repository) ListContracts(ctx context.Context, f ContractFilter, limit int, cursor string) ([]Contract, string, error) {
	filter := country.Restrict(ctx, bson.M{})
	if f.UserID != nil {
		filter["user_id"] = *f.UserID
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.Vertical != "" {
		filter["vertical"] = f.Vertical
	}
	if f.ItemID != nil {
		filter["item_id"] = *f.ItemID
	}
	if cursor != "" {
		if cid, err := primitive.ObjectIDFromHex(cursor); err == nil {
			filter["_id"] = bson.M{"$lt": cid}
		}
	}
	cur, err := r.contracts.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit+1)))
	if err != nil {
		return nil, "", fmt.Errorf("equipment: list contracts: %w", err)
	}
	var items []Contract
	if err := cur.All(ctx, &items); err != nil {
		return nil, "", fmt.Errorf("equipment: list contracts decode: %w", err)
	}
	if len(items) <= limit {
		return items, "", nil
	}
	items = items[:limit]
	return items, items[limit-1].ID.Hex(), nil
}

// ContractsOfUser : tous les contrats d'une personne, du plus ancien au
// plus récent — l'ordre dans lequel les retenues s'appliquent. SANS borne
// pays : l'agent lit les siens d'où qu'il appelle.
func (r *Repository) ContractsOfUser(ctx context.Context, userID primitive.ObjectID, onlyActive bool) ([]Contract, error) {
	filter := bson.M{"user_id": userID}
	if onlyActive {
		filter["status"] = StatusActive
	}
	cur, err := r.contracts.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("equipment: contracts of user: %w", err)
	}
	var out []Contract
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("equipment: contracts of user decode: %w", err)
	}
	return out, nil
}

// ActiveContracts : tout ce qui vit, tous pays — le balayage des échéances.
func (r *Repository) ActiveContracts(ctx context.Context) ([]Contract, error) {
	cur, err := r.contracts.Find(ctx, bson.M{"status": StatusActive})
	if err != nil {
		return nil, fmt.Errorf("equipment: active contracts: %w", err)
	}
	var out []Contract
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("equipment: active contracts decode: %w", err)
	}
	return out, nil
}

// --- settings ---

func (r *Repository) Settings(ctx context.Context, code string) (*Settings, error) {
	var s Settings
	err := r.settings.FindOne(ctx, bson.M{"_id": code}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		d := DefaultSettings(code)
		return &d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("equipment: settings: %w", err)
	}
	return &s, nil
}

func (r *Repository) SaveSettings(ctx context.Context, s *Settings) error {
	s.UpdatedAt = time.Now().UTC()
	_, err := r.settings.ReplaceOne(ctx, bson.M{"_id": s.Country}, s, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("equipment: save settings: %w", err)
	}
	return nil
}
