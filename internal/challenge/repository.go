package challenge

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

// Repository lit et écrit les objectifs et les avancements.
type Repository struct {
	col      *mongo.Collection
	progress *mongo.Collection
}

func NewRepository(m *db.Mongo) *Repository {
	return &Repository{
		col:      m.Collection(Collection),
		progress: m.Collection(CollectionProgress),
	}
}

// EnsureIndexes pose ce qu'il faut pour lire vite et écrire une seule fois.
func (r *Repository) EnsureIndexes(ctx context.Context) error {
	if _, err := r.col.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Les objectifs VIVANTS d'un public, dans un pays — la lecture que fait
		// chaque application à chaque ouverture.
		{Keys: bson.D{
			{Key: "country", Value: 1}, {Key: "audience", Value: 1},
			{Key: "status", Value: 1}, {Key: "window.to", Value: 1},
		}},
		{Keys: bson.D{{Key: "series_id", Value: 1}, {Key: "window.from", Value: 1}}},
	}); err != nil {
		return fmt.Errorf("challenge: indexes: %w", err)
	}
	// ⚠️ L'INDEX UNIQUE QUI TIENT « UNE SEULE LIGNE PAR PERSONNE ET PAR
	// OBJECTIF », et il est au cœur du dispositif : sans lui, deux appels
	// simultanés d'une verticale créent DEUX avancements, chacun à la moitié du
	// chemin, et l'objectif ne se gagne jamais. ⚠️ Non partiel, délibérément —
	// un index unique PARTIEL ne verrouille que les lignes qu'il couvre, et
	// c'est exactement le piège qui a laissé partir un rapport en double.
	if _, err := r.progress.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "challenge_id", Value: 1}, {Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return fmt.Errorf("challenge: progress index: %w", err)
	}
	return nil
}

// Insert écrit un objectif.
func (r *Repository) Insert(ctx context.Context, c *Challenge) error {
	res, err := r.col.InsertOne(ctx, c)
	if err != nil {
		return fmt.Errorf("challenge: insert: %w", err)
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		c.ID = oid
	}
	return nil
}

// ByID rend un objectif.
func (r *Repository) ByID(ctx context.Context, id primitive.ObjectID) (*Challenge, error) {
	var c Challenge
	err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("challenge: by id: %w", err)
	}
	return &c, nil
}

// Save remplace un objectif.
func (r *Repository) Save(ctx context.Context, c *Challenge) error {
	c.UpdatedAt = time.Now().UTC()
	_, err := r.col.ReplaceOne(ctx, bson.M{"_id": c.ID}, c)
	if err != nil {
		return fmt.Errorf("challenge: save: %w", err)
	}
	return nil
}

// Filter borne une liste d'objectifs.
type Filter struct {
	Audience string
	Status   string
	// Live : seulement ceux qui comptent MAINTENANT.
	Live bool
	At   time.Time
}

// listQuery construit le filtre d'une liste.
func listQuery(f Filter) bson.M {
	q := bson.M{}
	if f.Audience != "" {
		q["audience"] = f.Audience
	}
	switch {
	case f.Live:
		q["status"] = StatusLive
		q["window.from"] = bson.M{"$lte": f.At}
		q["window.to"] = bson.M{"$gt": f.At}
	case f.Status != "":
		q["status"] = f.Status
	}
	return q
}

// List rend les objectifs d'un pays.
func (r *Repository) List(ctx context.Context, f Filter, limit int) ([]Challenge, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cur, err := r.col.Find(ctx, country.Restrict(ctx, listQuery(f)),
		options.Find().SetSort(bson.D{{Key: "window.from", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("challenge: list: %w", err)
	}
	defer cur.Close(ctx)
	out := make([]Challenge, 0, limit)
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("challenge: list decode: %w", err)
	}
	return out, nil
}

// AddProgress compte une référence NOUVELLE et rend l'avancement d'après.
//
// ⚠️ LE FILTRE PORTE `refs: {$ne: ref}` : c'est la DÉDUPLICATION, et elle est
// dans l'écriture plutôt que dans une lecture préalable. Une verticale qui
// réessaie après un délai d'attente compterait deux fois la même course — et un
// objectif à 20 se gagnerait à 10. Lire puis écrire aurait laissé la fenêtre
// ouverte entre les deux.
//
// Rend `nil` quand la référence était déjà comptée : ce n'est pas une erreur,
// c'est un doublon correctement ignoré.
func (r *Repository) AddProgress(ctx context.Context, challengeID, userID primitive.ObjectID, countryCode, ref string, delta int, now time.Time) (*Progress, error) {
	var p Progress
	err := r.progress.FindOneAndUpdate(ctx,
		bson.M{
			"challenge_id": challengeID,
			"user_id":      userID,
			"refs":         bson.M{"$ne": ref},
		},
		bson.M{
			"$inc": bson.M{"value": delta},
			"$push": bson.M{"refs": bson.M{
				"$each": []string{ref}, "$slice": -RefsKept,
			}},
			"$set":         bson.M{"updated_at": now},
			"$setOnInsert": bson.M{"country": countryCode},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&p)
	switch {
	case mongo.IsDuplicateKeyError(err):
		// L'index unique a mordu : la ligne existe et porte déjà cette
		// référence. Doublon, pas erreur.
		return nil, nil
	case errors.Is(err, mongo.ErrNoDocuments):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("challenge: add progress: %w", err)
	}
	return &p, nil
}

// ClaimSlot prend une place dans l'enveloppe, SI elle en a une.
//
// Rend `false` quand l'enveloppe est pleine, l'objectif arrêté, ou la fenêtre
// passée — trois raisons de ne pas payer, et aucune n'est une erreur.
func (r *Repository) ClaimSlot(ctx context.Context, c *Challenge, now time.Time) (bool, error) {
	res, err := r.col.UpdateOne(ctx,
		claimFilter(c.ID, c.Limits, c.RewardXOF, now),
		claimUpdate(c.RewardXOF, now))
	if err != nil {
		return false, fmt.Errorf("challenge: claim slot: %w", err)
	}
	return res.ModifiedCount == 1, nil
}

// SettleSlot déplace une place de « promis » à « dépensé ».
func (r *Repository) SettleSlot(ctx context.Context, id primitive.ObjectID, rewardXOF int, now time.Time) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, settleUpdate(rewardXOF, now))
	if err != nil {
		return fmt.Errorf("challenge: settle slot: %w", err)
	}
	return nil
}

// ReleaseSlot rend une place après un échec définitif de versement.
func (r *Repository) ReleaseSlot(ctx context.Context, id primitive.ObjectID, rewardXOF int, now time.Time) error {
	_, err := r.col.UpdateOne(ctx, bson.M{"_id": id}, releaseUpdate(rewardXOF, now))
	if err != nil {
		return fmt.Errorf("challenge: release slot: %w", err)
	}
	return nil
}

// MarkReached note le franchissement, UNE SEULE FOIS.
//
// ⚠️ LE FILTRE EXIGE `reached_at` ABSENT, et c'est ce qui rend le droit au
// bonus unique : deux courses terminées dans la même seconde franchiraient la
// cible deux fois, et la seconde ne modifie rien.
func (r *Repository) MarkReached(ctx context.Context, challengeID, userID primitive.ObjectID, now time.Time) (bool, error) {
	res, err := r.progress.UpdateOne(ctx,
		bson.M{
			"challenge_id": challengeID, "user_id": userID,
			"reached_at": bson.M{"$exists": false},
		},
		bson.M{"$set": bson.M{"reached_at": now, "updated_at": now}})
	if err != nil {
		return false, fmt.Errorf("challenge: mark reached: %w", err)
	}
	return res.ModifiedCount == 1, nil
}

// MarkPaid note le versement.
func (r *Repository) MarkPaid(ctx context.Context, challengeID, userID primitive.ObjectID, amountXOF int, now time.Time) error {
	_, err := r.progress.UpdateOne(ctx,
		bson.M{"challenge_id": challengeID, "user_id": userID},
		bson.M{"$set": bson.M{"paid_at": now, "paid_xof": amountXOF, "updated_at": now}})
	if err != nil {
		return fmt.Errorf("challenge: mark paid: %w", err)
	}
	return nil
}

// MarkMissed note qu'une cible a été franchie SANS enveloppe pour la payer.
//
// ⚠️ C'EST UN AVEU, ET IL DOIT ÊTRE LISIBLE. Si ce champ se remplit, une
// promesse a été faite et non tenue : l'exploitation doit le voir, décider de
// payer à la main ou d'augmenter l'enveloppe, et comprendre que son objectif
// était sous-doté.
func (r *Repository) MarkMissed(ctx context.Context, challengeID, userID primitive.ObjectID, now time.Time) error {
	_, err := r.progress.UpdateOne(ctx,
		bson.M{"challenge_id": challengeID, "user_id": userID},
		bson.M{"$set": bson.M{"missed": true, "updated_at": now}})
	if err != nil {
		return fmt.Errorf("challenge: mark missed: %w", err)
	}
	return nil
}

// ClaimOwed réclame les bonus DUS à une personne, et les marque payés.
//
// ⚠️ C'EST LE CHEMIN DES CHAUFFEURS VTC, et il est en TIRÉ plutôt qu'en poussé.
// Le socle ne peut pas appeler une verticale — les annonces vont dans l'autre
// sens —, et un chauffeur n'a pas de portefeuille ici : son argent vit dans le
// grand livre des courses. La verticale vient donc chercher ce qu'on lui doit,
// exactement comme elle le fait pour les cautions de matériel.
//
// ⚠️ ATOMIQUE, UN PAR UN. `FindOneAndUpdate` filtré sur `paid_at` absent est ce
// qui rend l'appel rejouable : une verticale qui réessaie après un délai
// d'attente ne reprend pas ce qu'elle a déjà encaissé. Lire la liste puis la
// marquer en deux temps aurait laissé la fenêtre ouverte entre les deux — et
// un bonus payé deux fois est de l'argent perdu que personne ne réclame.
func (r *Repository) ClaimOwed(ctx context.Context, userID primitive.ObjectID, now time.Time) ([]Progress, error) {
	out := make([]Progress, 0, 4)
	for {
		var p Progress
		err := r.progress.FindOneAndUpdate(ctx,
			bson.M{
				"user_id":    userID,
				"reached_at": bson.M{"$exists": true},
				"paid_at":    bson.M{"$exists": false},
				// ⚠️ LES MANQUÉS NE SONT PAS DUS : la cible a été franchie sans
				// enveloppe pour la payer, et c'est à l'exploitation de
				// trancher. Les servir ici ferait payer par la verticale un
				// bonus que le socle a refusé.
				"missed": bson.M{"$ne": true},
			},
			bson.M{"$set": bson.M{"paid_at": now, "updated_at": now}},
			options.FindOneAndUpdate().SetReturnDocument(options.Before),
		).Decode(&p)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return out, nil
		}
		if err != nil {
			// ⚠️ ON REND CE QU'ON A DÉJÀ RÉCLAMÉ. Ces lignes sont marquées
			// payées : les perdre ferait croire à la verticale qu'elle ne doit
			// rien, et le chauffeur ne verrait jamais son bonus.
			return out, fmt.Errorf("challenge: claim owed: %w", err)
		}
		out = append(out, p)
		if len(out) >= 20 {
			// Borne de sûreté : vingt bonus en attente pour une personne est
			// déjà anormal, et une boucle sans borne sur une base en vrac
			// tournerait indéfiniment.
			return out, nil
		}
	}
}

// ProgressOf rend l'avancement d'une personne sur plusieurs objectifs.
func (r *Repository) ProgressOf(ctx context.Context, userID primitive.ObjectID, ids []primitive.ObjectID) (map[primitive.ObjectID]Progress, error) {
	out := map[primitive.ObjectID]Progress{}
	if len(ids) == 0 {
		return out, nil
	}
	cur, err := r.progress.Find(ctx, bson.M{
		"user_id": userID, "challenge_id": bson.M{"$in": ids},
	})
	if err != nil {
		return nil, fmt.Errorf("challenge: progress of: %w", err)
	}
	defer cur.Close(ctx)
	var rows []Progress
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("challenge: progress decode: %w", err)
	}
	for _, p := range rows {
		out[p.ChallengeID] = p
	}
	return out, nil
}

// Winners rend les avancements d'un objectif — ce que la console lit.
func (r *Repository) Winners(ctx context.Context, challengeID primitive.ObjectID, limit int) ([]Progress, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cur, err := r.progress.Find(ctx,
		bson.M{"challenge_id": challengeID, "reached_at": bson.M{"$exists": true}},
		options.Find().SetSort(bson.D{{Key: "reached_at", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("challenge: winners: %w", err)
	}
	defer cur.Close(ctx)
	out := make([]Progress, 0, limit)
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("challenge: winners decode: %w", err)
	}
	return out, nil
}
