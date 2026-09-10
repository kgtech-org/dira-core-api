package chat

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

var (
	errNotParty = apperr.Forbidden("forbidden", "this conversation is not yours")
	// errNoDriver : tant que personne n'a pris la course, il n'y a personne à
	// qui écrire. Un message parti dans le vide serait pire qu'un refus — le
	// client attendrait une réponse.
	// ⚠️ Le message ne dit ni « commande » ni « course » : ce paquet sert les
	// deux, et un texte qui nommerait l'un afficherait « aucun livreur n'a pris
	// cette commande » à un passager qui attend une voiture.
	errNoDriver = apperr.Conflict("no_driver_yet", "nobody has taken this yet")
	errClosed   = apperr.Conflict("conversation_closed", "this conversation is closed")
	errEmpty    = apperr.Validation("message body is required")
)

// Conversation is who may speak on an order, and until when.
//
// Un seul appel rend les trois faits : sans cela, le service enchaînerait des
// lectures dont chacune peut échouer à moitié, pour une question qui n'en est
// qu'une — « puis-je écrire ici ? ».
type Conversation struct {
	ClientID string
	// DriverUserID est vide tant que personne n'a pris la course.
	DriverUserID string
	Status       string
	// CompletedAt est l'instant où la course s'est terminée — commande livrée,
	// passager déposé. Nil tant qu'elle est en cours.
	CompletedAt *time.Time
}

// Parties resolves a conversation. Implemented at wiring time from the order
// and delivery modules — ce module n'importe ni l'un ni l'autre.
type Parties interface {
	ConversationOf(ctx context.Context, refID string) (*Conversation, error)
}

// Notifier pushes a message to whoever is listening. Facultatif : sans lui, la
// conversation fonctionne, elle se rafraîchit simplement à la lecture.
type Notifier interface {
	NotifyMessage(ctx context.Context, refID, clientID, driverUserID, senderRole, body string)
}

// Pusher atteint le DESTINATAIRE quand son application est fermée. Le socket
// ne porte que jusqu'à un écran allumé, et « je suis en bas » n'a de valeur
// que reçu tout de suite.
//
// ⚠️ Il reçoit le côté et le corps, JAMAIS une identité : la notification ne
// doit pas nommer sur l'écran verrouillé ce que la conversation protège.
type Pusher interface {
	NotifyChatMessage(ctx context.Context, recipientUserID, refID, body string)
}

type Service struct {
	repo    *Repository
	parties Parties
	// refKind est écrit sur chaque message : « order » ou « ride ». Il tient
	// dans le service et non dans l'appel, parce qu'une instance ne sert
	// qu'une verticale — le passer à chaque envoi laisserait la possibilité
	// d'écrire une course dans les conversations de commandes.
	refKind  string
	notifier Notifier
	pusher   Pusher
}

// NewService builds the conversation service of ONE vertical.
//
// `refKind` dit de quoi l'on parle ici — `RefOrder` ou `RefRide`.
func NewService(repo *Repository, refKind string, parties Parties) *Service {
	return &Service{repo: repo, refKind: refKind, parties: parties}
}

// SetNotifier branche la diffusion temps réel (câblage).
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// SetPusher branche les notifications poussées (câblage).
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// roleOf tells which side of the conversation a caller is on, or "" when they
// are on neither.
//
// L'ADMIN est un cas à part : il LIT pour le support, il n'écrit pas. Écrire
// en son nom ferait passer un message de la plateforme pour celui d'un
// livreur, et c'est exactement ce qu'un client ne doit pas avoir à démêler.
func roleOf(userID string, c *Conversation) string {
	switch {
	case c.ClientID == userID:
		return SenderClient
	case c.DriverUserID != "" && c.DriverUserID == userID:
		return SenderDriver
	default:
		return ""
	}
}

// List returns the conversation of an order, and how many messages the caller
// has not read.
func (s *Service) List(ctx context.Context, userID, role, refID, after string, limit int) ([]MessageResponse, int, error) {
	oid, _, party, err := s.access(ctx, userID, role, refID)
	if err != nil {
		return nil, 0, err
	}
	var afterID *primitive.ObjectID
	if after != "" {
		parsed, err := primitive.ObjectIDFromHex(after)
		if err != nil {
			return nil, 0, apperr.Validation("invalid cursor").WithCause(err)
		}
		afterID = &parsed
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, err := s.repo.ListByRef(ctx, oid, afterID, limit)
	if err != nil {
		return nil, 0, err
	}
	unread := 0
	if party != "" {
		if unread, err = s.repo.CountUnread(ctx, oid, party); err != nil {
			// Au mieux : un compteur perdu ne doit pas emporter la
			// conversation elle-même.
			slog.WarnContext(ctx, "chat: unread count unavailable", "ref_id", refID, "error", err)
			unread = 0
		}
	}
	return toResponses(items), unread, nil
}

// Send posts a message from the caller.
func (s *Service) Send(ctx context.Context, userID, role, refID, body string) (*MessageResponse, error) {
	oid, conv, party, err := s.access(ctx, userID, role, refID)
	if err != nil {
		return nil, err
	}
	if party == "" {
		// Un administrateur lit, il n'écrit pas.
		return nil, errNotParty
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errEmpty
	}
	if len([]rune(body)) > MaxBody {
		return nil, apperr.Validation("message is too long")
	}
	if err := writable(conv); err != nil {
		return nil, err
	}

	senderOID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	m := &Message{
		RefID:      oid,
		RefKind:    s.refKind,
		SenderID:   senderOID,
		SenderRole: party,
		Body:       body,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.repo.Insert(ctx, m); err != nil {
		return nil, err
	}
	if s.notifier != nil {
		s.notifier.NotifyMessage(ctx, refID, conv.ClientID, conv.DriverUserID, party, body)
	}
	// L'AUTRE côté, et lui seul : s'envoyer une notification à soi-même est
	// le défaut classique de ce genre de branchement.
	if s.pusher != nil {
		recipient := conv.DriverUserID
		if party == SenderDriver {
			recipient = conv.ClientID
		}
		if recipient != "" {
			s.pusher.NotifyChatMessage(ctx, recipient, refID, body)
		}
	}
	resp := toResponse(*m)
	return &resp, nil
}

// SendAs posts a message ON BEHALF of one side, for the back-office
// emulators.
//
// Le message est attribué à la VRAIE partie — son identifiant d'utilisateur,
// son côté —, jamais à l'administrateur. C'est le même motif que les autres
// émulateurs de la console : ils agissent POUR un acteur, par la même méthode
// de service, et non à côté de lui.
//
// La règle qui interdit à un administrateur d'écrire EN SON NOM reste entière :
// ici il n'écrit pas en son nom, il rejoue ce que la partie aurait envoyé.
func (s *Service) SendAs(ctx context.Context, refID, as, body string) (*MessageResponse, error) {
	if as != SenderClient && as != SenderDriver {
		return nil, apperr.Validation("as must be client or driver")
	}
	conv, err := s.parties.ConversationOf(ctx, refID)
	if err != nil {
		return nil, err
	}
	userID := conv.ClientID
	if as == SenderDriver {
		userID = conv.DriverUserID
	}
	if userID == "" {
		return nil, errNoDriver
	}
	// On repasse par Send : mêmes bornes, mêmes refus, même diffusion. Une
	// seconde implémentation aurait fini par diverger de la première.
	return s.Send(ctx, userID, "", refID, body)
}

// MarkRead marks everything the other side sent as read.
func (s *Service) MarkRead(ctx context.Context, userID, role, refID string) (int, error) {
	oid, _, party, err := s.access(ctx, userID, role, refID)
	if err != nil {
		return 0, err
	}
	if party == "" {
		// Lire en tant qu'admin ne marque rien : le support n'est pas le
		// destinataire, et effacer le non-lu du client serait lui cacher un
		// message qu'il n'a jamais vu.
		return 0, nil
	}
	n, err := s.repo.MarkRead(ctx, oid, party, time.Now().UTC())
	return int(n), err
}

// access resolves the order, its conversation and the caller's side of it.
func (s *Service) access(ctx context.Context, userID, role, refID string) (primitive.ObjectID, *Conversation, string, error) {
	oid, err := primitive.ObjectIDFromHex(refID)
	if err != nil {
		return primitive.NilObjectID, nil, "", apperr.NotFound("not_found", "not found").WithCause(err)
	}
	conv, err := s.parties.ConversationOf(ctx, refID)
	if err != nil {
		return primitive.NilObjectID, nil, "", err
	}
	party := roleOf(userID, conv)
	if party == "" && role != auth.RoleAdmin {
		return primitive.NilObjectID, nil, "", errNotParty
	}
	return oid, conv, party, nil
}

// writable says whether the conversation still accepts messages.
func writable(c *Conversation) error {
	if c.DriverUserID == "" {
		return errNoDriver
	}
	if c.Status == "cancelled" {
		return errClosed.WithMeta(map[string]any{"reason": "cancelled"})
	}
	// Livrée : la conversation reste ouverte le temps de dire ce qui doit
	// l'être — puis se ferme, l'historique demeurant lisible.
	if c.CompletedAt != nil && time.Since(*c.CompletedAt) > WriteWindowAfterCompletion {
		return errClosed.WithMeta(map[string]any{"reason": "completed", "window": WriteWindowAfterCompletion.String()})
	}
	return nil
}
