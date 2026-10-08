package promocode

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
	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// Repository stores codes, their uses and the influencer records.
type Repository struct {
	codes       *mongo.Collection
	uses        *mongo.Collection
	influencers *mongo.Collection
	ledger      *promo.Ledger
}

func NewRepository(m *db.Mongo) *Repository {
	codes, uses := m.Collection(Collection), m.Collection(CollectionUses)
	return &Repository{
		codes: codes, uses: uses,
		influencers: m.Collection(CollectionInfluencers),
		// ⚠️ LE MÊME REGISTRE QUE LES PROMOTIONS DES VERTICALES, sur nos deux
		// collections : l'enveloppe, les deux temps et l'idempotence par
		// référence sont déjà écrits et déjà éprouvés. Un second moteur aurait
		// été un second endroit où de l'argent se compte.
		ledger: promo.NewLedger(uses, codes),
	}
}

// ErrDuplicateCode : ce code existe déjà.
//
// ⚠️ SUR TOUTE LA PLATEFORME, pas seulement dans ce pays. Deux pays qui
// posséderaient « DIRA10 » ne se gêneraient pas techniquement — mais une
// affiche, un SMS et une radio ne connaissent pas les frontières, et le
// support recevrait des appels qu'il ne saurait pas trancher.
var ErrDuplicateCode = errors.New("promocode: this code already exists")

// Ledger rend le registre des usages.
func (r *Repository) Ledger() *promo.Ledger { return r.ledger }

// Create writes a new code.
func (r *Repository) Create(ctx context.Context, c *Code) error {
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	res, err := r.codes.InsertOne(ctx, c)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateCode
		}
		return fmt.Errorf("promocode: create: %w", err)
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		c.ID = id
	}
	return nil
}

// ByCode lit un code par son mot, SANS borne de pays.
//
// ⚠️ SANS BORNE, ET C'EST VOULU. Un code est unique sur la plateforme : le
// chercher dans le pays de la requête ferait répondre « code inconnu » à
// quelqu'un qui a un vrai code d'un autre pays — alors que la bonne réponse
// est « ce code ne vaut pas ici ». La différence compte : la première envoie
// chercher une faute de frappe qui n'existe pas.
func (r *Repository) ByCode(ctx context.Context, code string) (*Code, error) {
	var c Code
	err := r.codes.FindOne(ctx, bson.M{"code": code}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("promocode: by code: %w", err)
	}
	return &c, nil
}

// ByID lit un code par son identifiant.
func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Code, error) {
	var c Code
	err := r.codes.FindOne(ctx, bson.M{"_id": id}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("promocode: by id: %w", err)
	}
	return &c, nil
}

// List rend les codes du pays de la requête, les plus récents d'abord.
func (r *Repository) List(ctx context.Context, kind string, ownerID *primitive.ObjectID, limit int) ([]Code, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	filter := country.Restrict(ctx, bson.M{})
	if kind != "" {
		filter["kind"] = kind
	}
	if ownerID != nil {
		filter["owner_id"] = *ownerID
	}
	cur, err := r.codes.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("promocode: list: %w", err)
	}
	var out []Code
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("promocode: decode list: %w", err)
	}
	return out, nil
}

// Update applique un champ à la fois — l'appelant compose son `$set`.
func (r *Repository) Update(ctx context.Context, id primitive.ObjectID, set bson.M) error {
	if len(set) == 0 {
		return nil
	}
	set["updated_at"] = time.Now().UTC()
	if _, err := r.codes.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set}); err != nil {
		return fmt.Errorf("promocode: update: %w", err)
	}
	return nil
}

// UsesOf rend les usages attachés à une référence — la course, la commande.
//
// ⚠️ LUE AVANT DE CLORE, parce qu'une ligne réglée ne dit plus quel code elle
// portait ni combien : c'est exactement ce qu'il faut pour payer un parrain.
func (r *Repository) UsesOf(ctx context.Context, refID string) ([]promo.Use, error) {
	cur, err := r.uses.Find(ctx, bson.M{"ref_id": refID})
	if err != nil {
		return nil, fmt.Errorf("promocode: uses of %s: %w", refID, err)
	}
	var out []promo.Use
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("promocode: decode uses: %w", err)
	}
	return out, nil
}

// StatsOf rend ce qu'un code a donné — usages, personnes distinctes, coût.
//
// ⚠️ EN UNE SEULE AGRÉGATION, et pas en comptant des lignes en Go. Un code qui
// marche porte des dizaines de milliers d'usages : les rapatrier pour les
// compter aurait fonctionné sur le jeu de démonstration et serait tombé le
// premier jour où une campagne réussit.
func (r *Repository) StatsOf(ctx context.Context, code string) (Stats, error) {
	var out Stats
	cur, err := r.uses.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"promo_id": code}}},
		{{Key: "$group", Value: bson.M{
			"_id": "$state",
			"n":   bson.M{"$sum": 1},
			"amt": bson.M{"$sum": "$amount_xof"},
			// ⚠️ LES PERSONNES DISTINCTES, pas le nombre de lignes. L'écart
			// entre les deux est exactement ce qu'on veut voir : cent usages
			// par trois personnes n'est pas une campagne, c'est une fuite.
			"people": bson.M{"$addToSet": "$user_id"},
		}}},
	})
	if err != nil {
		return out, fmt.Errorf("promocode: stats: %w", err)
	}
	var rows []struct {
		State  string   `bson:"_id"`
		N      int      `bson:"n"`
		Amt    int      `bson:"amt"`
		People []string `bson:"people"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return out, fmt.Errorf("promocode: stats decode: %w", err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		switch row.State {
		case promo.StateSpent:
			out.Uses += row.N
			out.DiscountXOF += row.Amt
		case promo.StateReserved:
			out.Uses += row.N
			out.Reserved += row.N
			// ⚠️ LE RÉSERVÉ COMPTE DANS LE COÛT : c'est de l'argent ENGAGÉ.
			// Ne compter que le dépensé ferait croire qu'une campagne en
			// cours ne coûte rien.
			out.DiscountXOF += row.Amt
		case promo.StateReleased:
			out.Released += row.N
		}
		// Les personnes de TOUS les états, rendus compris : quelqu'un qui a
		// essayé puis annulé reste quelqu'un que le code a touché.
		for _, p := range row.People {
			seen[p] = true
		}
	}
	out.People = len(seen)
	return out, nil
}

// --- LES INFLUENCEURS -----------------------------------------------------

// ErrDuplicateHandle : ce pseudonyme est déjà pris.
var ErrDuplicateHandle = errors.New("promocode: this handle already exists")

func (r *Repository) CreateInfluencer(ctx context.Context, i *Influencer) error {
	now := time.Now().UTC()
	i.CreatedAt, i.UpdatedAt = now, now
	res, err := r.influencers.InsertOne(ctx, i)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrDuplicateHandle
		}
		return fmt.Errorf("promocode: create influencer: %w", err)
	}
	if id, ok := res.InsertedID.(primitive.ObjectID); ok {
		i.ID = id
	}
	return nil
}

func (r *Repository) InfluencerByID(ctx context.Context, id primitive.ObjectID) (*Influencer, error) {
	var i Influencer
	err := r.influencers.FindOne(ctx, bson.M{"_id": id}).Decode(&i)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("promocode: influencer by id: %w", err)
	}
	return &i, nil
}

// InfluencerByUser lit la fiche attachée à un compte — ce qui permet de dire
// « cette personne est déjà influenceuse » plutôt que d'en créer une seconde.
func (r *Repository) InfluencerByUser(ctx context.Context, userID primitive.ObjectID) (*Influencer, error) {
	var i Influencer
	err := r.influencers.FindOne(ctx, bson.M{"user_id": userID}).Decode(&i)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("promocode: influencer by user: %w", err)
	}
	return &i, nil
}

func (r *Repository) ListInfluencers(ctx context.Context, limit int) ([]Influencer, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	cur, err := r.influencers.Find(ctx, country.Restrict(ctx, bson.M{}),
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("promocode: list influencers: %w", err)
	}
	var out []Influencer
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("promocode: decode influencers: %w", err)
	}
	return out, nil
}

func (r *Repository) UpdateInfluencer(ctx context.Context, id primitive.ObjectID, set bson.M) error {
	if len(set) == 0 {
		return nil
	}
	set["updated_at"] = time.Now().UTC()
	if _, err := r.influencers.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set}); err != nil {
		return fmt.Errorf("promocode: update influencer: %w", err)
	}
	return nil
}
