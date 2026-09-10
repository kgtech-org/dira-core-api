package callback

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/kgtech-org/dira-core-api/pkg/jobs"
)

// Worker consomme les annonces de paiement à destination des verticales.
//
// ⚠️ POURQUOI UNE FILE ICI, et nulle part ailleurs dans le socle.
//
// Les vingt-trois autres échanges entre services sont des COMMANDES qui
// attendent une réponse — « débite ce portefeuille, ça a marché ? ». Une file
// ne leur apporte rien : il faudrait réinventer la requête/réponse au-dessus,
// c'est-à-dire refaire HTTP en moins bien.
//
// Celui-ci est le seul ÉVÉNEMENT : le socle constate un fait et n'attend rien
// en retour. Et il portait un couplage réel — le webhook du prestataire de
// paiement échouait tant que la verticale ne répondait pas. Pendant un
// redéploiement de la livraison, le paiement d'un client échouait, et c'est
// au prestataire qu'il revenait de réessayer. On faisait porter à l'acheteur
// la latence de nos mises en production.
type Worker struct {
	verticals *Registry
}

func NewWorker(verticals *Registry) *Worker { return &Worker{verticals: verticals} }

// HandleRefPaid rappelle la verticale.
//
// ⚠️ Les erreurs de CONFIGURATION rendent `SkipRetry` : une verticale non
// configurée le restera au vingtième essai, et accumuler des reprises vouées
// au même échec noie la file — celle qu'on regarde pour trouver les vraies
// pannes.
//
// Les erreurs de TRANSPORT, elles, reviennent : c'est exactement ce que la
// file est là pour absorber.
func (w *Worker) HandleRefPaid(ctx context.Context, t *asynq.Task) error {
	var p jobs.RefPaidPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		// Une charge illisible ne deviendra pas lisible.
		slog.ErrorContext(ctx, "callback: unreadable ref-paid payload, dropping",
			"error", err, "payload", string(t.Payload()))
		return fmt.Errorf("callback: unmarshal ref-paid: %w: %w", err, asynq.SkipRetry)
	}
	v := w.verticals.For(p.Purpose)
	if v == nil {
		slog.ErrorContext(ctx, "callback: no vertical configured for this purpose, dropping",
			"purpose", p.Purpose, "ref_id", p.RefID, "payment_id", p.PaymentID,
			"hint", "set FOOD_BASE_URL / VTC_BASE_URL and their callback tokens")
		return fmt.Errorf("callback: no vertical for purpose %q: %w", p.Purpose, asynq.SkipRetry)
	}
	if err := v.RefPaid(ctx, p.RefID, p.PaymentID); err != nil {
		// ⚠️ On JOURNALISE à chaque échec, avec le compte d'essais. Une file
		// qui réessaie en silence pendant vingt minutes ressemble, vue de
		// l'extérieur, à une plateforme qui a perdu un paiement.
		retried, _ := asynq.GetRetryCount(ctx)
		slog.WarnContext(ctx, "callback: vertical refused the payment confirmation, will retry",
			"purpose", p.Purpose, "ref_id", p.RefID, "payment_id", p.PaymentID,
			"attempt", retried+1, "error", err)
		return err
	}
	slog.InfoContext(ctx, "callback: vertical notified of the payment",
		"purpose", p.Purpose, "ref_id", p.RefID, "payment_id", p.PaymentID)
	return nil
}
