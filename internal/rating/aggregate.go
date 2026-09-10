package rating

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Aggregate is a target's score, computed FROM THE RATINGS THEMSELVES.
//
// ⚠️ C'est ce qui permet au socle de porter les notes sans connaître les
// verticales. Le repli dénormalisé — `rating_avg` sur un plat, un point de
// vente, un livreur — reste chez celui qui possède ces documents : le socle
// n'écrit que dans SA collection.
//
// La moyenne ne se lit jamais sans son compte : 5,0 sur un avis et 4,6 sur
// deux cents ne disent pas la même chose.
type Aggregate struct {
	TargetType string  `json:"target_type"`
	TargetID   string  `json:"target_id"`
	Count      int     `json:"count"`
	Average    float64 `json:"average"`
}

// AggregateOf computes one target's score from the stored ratings.
//
// Calculé À LA LECTURE plutôt que replié à l'écriture : le socle ne sait pas
// où une verticale range ses moyennes, et deux replis concurrents divergeraient
// sans que rien ne le signale.
func (r *Repository) AggregateOf(ctx context.Context, targetType string, targetID primitive.ObjectID) (*Aggregate, error) {
	pipeline := []bson.M{
		{"$match": bson.M{"target_type": targetType, "target_id": targetID}},
		{"$group": bson.M{"_id": nil, "count": bson.M{"$sum": 1}, "sum": bson.M{"$sum": "$score"}}},
	}
	cur, err := r.col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("rating: aggregate: %w", err)
	}
	var rows []struct {
		Count int `bson:"count"`
		Sum   int `bson:"sum"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("rating: decode aggregate: %w", err)
	}
	out := &Aggregate{TargetType: targetType, TargetID: targetID.Hex()}
	if len(rows) == 1 && rows[0].Count > 0 {
		out.Count = rows[0].Count
		out.Average = float64(rows[0].Sum) / float64(rows[0].Count)
	}
	return out, nil
}

// RecordTarget is one validated score a vertical asks the core to store.
type RecordTarget struct {
	TargetType string `json:"target_type" validate:"required,oneof=driver store dish"`
	TargetID   string `json:"target_id" validate:"required,len=24,hexadecimal"`
	Score      int    `json:"score" validate:"required,min=1,max=5"`
	Comment    string `json:"comment" validate:"omitempty,max=1000"`
}

// Record stores scores a VERTICAL has already validated.
//
// ⚠️ Le socle ne vérifie PAS qu'une note est légitime — que la commande est
// livrée, que ce livreur l'a portée, que ce plat y figurait. Il ne peut pas :
// il ne sait pas ce qu'est une commande. C'est la verticale qui possède cette
// connaissance, et c'est elle qui garde la responsabilité de ne pas laisser
// noter n'importe quoi.
//
// Ce qu'il garantit, en revanche, et qu'aucune verticale ne pourrait garantir
// seule : UNE note par (commande, cible), portée par un index unique. Deux
// envois simultanés passeraient tous les deux un contrôle applicatif et
// compteraient double.
func (s *Service) Record(ctx context.Context, clientID, orderID string, targets []RecordTarget) ([]Aggregate, error) {
	cid, err := primitive.ObjectIDFromHex(clientID)
	if err != nil {
		return nil, apperr.Validation("invalid client id").WithCause(err)
	}
	oid, err := primitive.ObjectIDFromHex(orderID)
	if err != nil {
		return nil, apperr.Validation("invalid order id").WithCause(err)
	}
	out := make([]Aggregate, 0, len(targets))
	for _, t := range targets {
		tid, err := primitive.ObjectIDFromHex(t.TargetID)
		if err != nil {
			return nil, apperr.Validation("invalid target id").WithCause(err)
		}
		if err := s.repo.Insert(ctx, &Rating{
			OrderID: oid, ClientID: cid, TargetType: t.TargetType,
			TargetID: tid, Score: t.Score, Comment: t.Comment,
		}); err != nil {
			return nil, err
		}
		agg, err := s.repo.AggregateOf(ctx, t.TargetType, tid)
		if err != nil {
			return nil, err
		}
		// Le total est rendu à la verticale : c'est elle qui replie la moyenne
		// sur SES documents, et lui faire recalculer ce que le socle vient de
		// compter serait une lecture de plus pour le même chiffre.
		out = append(out, *agg)
	}
	return out, nil
}
