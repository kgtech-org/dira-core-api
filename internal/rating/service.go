package rating

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
)

var (
	errNotDelivered = apperr.Conflict("order_not_delivered", "an order can only be rated once delivered")
	errForbidden    = apperr.Forbidden("forbidden", "this order is not yours")
	errNoDriver     = apperr.Validation("this order had no driver to rate")
	errNotInOrder   = apperr.Validation("this store or dish is not part of the order")
)

// ⚠️ Les interfaces RatedOrder, DriverOfOrder, AgentRatings, StoreRatings et
// DishRatings ont été RETIRÉES en déplaçant ce module au socle.
//
// Elles disaient au module ce qu'est une commande livrée, qui l'a portée, et
// où replier une moyenne. Le socle ne sait rien de tout cela, et les lui faire
// demander à la verticale aurait INVERSÉ la dépendance : le socle appelant la
// verticale pour servir la verticale.
//
// Le partage est donc : la verticale VALIDE et REPLIE, le socle STOCKE et
// AGRÈGE. `Record` reçoit des notes déjà légitimes et rend le total à jour,
// pour que la verticale replie sans recompter.

type Service struct {
	repo    *Repository
	auditor *audit.Recorder
}

// NewService builds the core rating service.
//
// ⚠️ Aucun collaborateur : le socle STOCKE et AGRÈGE, il ne valide pas. Savoir
// qu'une commande est livrée, que ce livreur l'a portée, que ce plat y
// figurait — c'est la verticale qui le sait, et c'est elle qui garde la
// responsabilité de ne pas laisser noter n'importe quoi.
//
// La version précédente prenait CINQ services de la livraison. Les lui donner
// depuis le socle aurait inversé la dépendance : le socle aurait appelé la
// verticale pour servir la verticale.
func NewService(repo *Repository, auditor *audit.Recorder) *Service {
	return &Service{repo: repo, auditor: auditor}
}

// ListForTarget returns the latest scores left on a driver or a store.
func (s *Service) ListForTarget(ctx context.Context, targetType, targetID string, limit int) ([]RatingResponse, error) {
	oid, err := primitive.ObjectIDFromHex(targetID)
	if err != nil {
		return nil, apperr.Validation("invalid target id").WithCause(err)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	items, err := s.repo.ListByTarget(ctx, targetType, oid, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RatingResponse, 0, len(items))
	for _, r := range items {
		out = append(out, RatingResponse{
			TargetType: r.TargetType, TargetID: r.TargetID.Hex(),
			Score: r.Score, Comment: r.Comment, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}
