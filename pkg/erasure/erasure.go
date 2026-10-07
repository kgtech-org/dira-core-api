// Package erasure sert la porte par laquelle le SOCLE annonce à une verticale
// qu'un compte vient d'être effacé.
//
// ⚠️ C'EST UN FAIT QUI ARRIVE, PAS UNE PERMISSION QU'ON DEMANDE. Quand cet
// appel entre, l'identité est DÉJÀ partie des bases du socle : le nom est
// devenu « Compte supprimé », le téléphone est brouillé, le carnet d'adresses
// est effacé. La verticale n'a rien à valider et rien à refuser pour des
// raisons métier — elle jette ce qu'elle seule détient.
//
// ⚠️ ET ELLE N'A RIEN À ANONYMISER. Ses courses et ses commandes ne portent ni
// nom ni téléphone : elles portent un identifiant de compte et demandent
// l'identité au socle au moment d'afficher. Elles sont donc déjà anonymes, et
// elles RESTENT — ce sont des écritures comptables, avec leur prix, leur
// commission et leur date. Ce qu'on demande ici, c'est de jeter le TEXTE écrit
// par la personne, et les FICHIERS qu'elle a déposés : messages de
// conversation, fils de support, pièces de conformité. Le socle ne les connaît
// pas, et personne d'autre ne les retrouvera.
//
// ⚠️ CE PAQUET EST UNE BIBLIOTHÈQUE, PAS UN SERVICE, comme `pkg/chat` et
// `pkg/support` : les deux verticales ont la même porte et des modules
// différents derrière. Chacune nomme les siens à son câblage, et c'est son
// `main.go` qui dit — en une liste lisible — tout ce qu'un effacement emporte
// chez elle. Un oubli se voit là, pas trois mois plus tard dans un bucket.
package erasure

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Purge est UN module qui détient quelque chose de la personne, et ce qu'il
// faut en faire.
//
// `Name` apparaît dans le journal : « 3 messages, 1 ticket, 2 pièces » est une
// réponse à « qu'est-ce qui a vraiment été effacé ? ». Un total muet n'en est
// pas une.
type Purge struct {
	Name string
	Run  func(ctx context.Context, userID primitive.ObjectID) (int64, error)
}

// Handler sert `POST /internal/accounts/erased`.
type Handler struct{ purges []Purge }

// NewHandler branche les modules à purger, dans l'ordre où ils seront appelés.
func NewHandler(purges ...Purge) *Handler {
	kept := make([]Purge, 0, len(purges))
	for _, p := range purges {
		if p.Run != nil && p.Name != "" {
			kept = append(kept, p)
		}
	}
	return &Handler{purges: kept}
}

// Mount enregistre la route derrière le secret de SERVICE.
//
// ⚠️ DERRIÈRE LE SECRET, évidemment : une porte ouverte qui efface les messages
// d'un compte sur simple identifiant serait une arme. L'intergiciel est celui
// de la verticale, le même que pour les autres rappels du socle.
func (h *Handler) Mount(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(serviceMW)
		g.Post("/internal/accounts/erased", h.erased)
	})
}

// erased purge et rend 204.
//
// ⚠️ L'ERREUR REMONTE AU SOCLE, qui remet l'annonce en file et réessaiera. Un
// `204` sur un échec partiel laisserait pour toujours le texte d'une personne
// effacée, sans que rien ne le dise nulle part — et la file est le seul endroit
// où ce fait survit.
//
// ⚠️ ON CONTINUE APRÈS UN ÉCHEC, et on ne rend l'erreur qu'à la fin : si le
// support tombe, les messages de conversation doivent partir quand même. Sortir
// au premier échec rendrait l'ordre des modules — un détail de câblage —
// décisif pour ce qui est effacé.
func (h *Handler) erased(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id" validate:"required,len=24,hexadecimal"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	oid, err := primitive.ObjectIDFromHex(req.UserID)
	if err != nil {
		httpx.Error(w, r, apperr.Validation("user_id must be an object id").WithCause(err))
		return
	}
	if len(h.purges) == 0 {
		// ⚠️ UN CRI, PAS UN SILENCE. Une porte montée sans rien derrière
		// répondrait « c'est fait » à chaque effacement, et la file du socle
		// marquerait l'annonce comme livrée.
		slog.ErrorContext(r.Context(), "erasure: nothing wired to purge — this erased account keeps everything it wrote here",
			"user_id", req.UserID)
		httpx.Error(w, r, apperr.New("erasure_unavailable",
			"nothing is wired to purge an erased account here", http.StatusServiceUnavailable))
		return
	}

	fields := []any{"user_id", req.UserID}
	var failures []error
	for _, p := range h.purges {
		n, err := p.Run(r.Context(), oid)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", p.Name, err))
			continue
		}
		fields = append(fields, p.Name, n)
	}
	if len(failures) > 0 {
		slog.ErrorContext(r.Context(), "erasure: part of an erased account survived here, the core will retry",
			append(fields, "error", errors.Join(failures...))...)
		httpx.Error(w, r, apperr.Internal(errors.Join(failures...)))
		return
	}
	// ⚠️ JOURNALISÉ MÊME QUAND TOUT VA BIEN, et c'est le seul endroit où la
	// question « qu'avons-nous réellement effacé de cette personne ? » trouve
	// une réponse chiffrée — le compte, lui, n'existe plus pour la poser.
	slog.InfoContext(r.Context(), "erasure: erased account purged", fields...)
	w.WriteHeader(http.StatusNoContent)
}
