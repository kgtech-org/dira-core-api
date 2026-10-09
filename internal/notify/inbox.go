package notify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// CollectionInbox is the MongoDB collection backing the notification centre.
const CollectionInbox = "notifications"

// Categories, telles que l'écran de préférences les présente. Ce sont elles
// qu'un utilisateur coupe — jamais une clé de message : couper
// `order_delivered` sans couper `order_ready` n'aurait aucun sens pour lui.
const (
	CategoryOrderUpdates = "order_updates"
	CategoryChatMessages = "chat_messages"
	CategoryPromotions   = "promotions"
	CategoryTombola      = "tombola"
	CategoryDriverCall   = "driver_call"
	// CategoryStaff range les alertes d'exploitation poussées au staff. Pas
	// coupable : un poste de supervision qui se coupe ses propres alertes
	// n'en est plus un.
	CategoryStaff = "staff"
	// CategorySupport range ce que le guichet répond à une demande qu'on a
	// soi-même ouverte, et l'objet oublié qu'un chauffeur doit chercher.
	// Pas coupable non plus : on a posé la question, on reçoit la réponse.
	CategorySupport = "support"
	// CategorySecurity range ce qui arrive AU COMPTE lui-même : une session
	// ouverte sur un autre appareil.
	//
	// ⚠️ PAS COUPABLE, et c'est la raison de la catégorie. Rangée dans les
	// mises à jour de commande, elle aurait disparu avec elles — et un
	// chauffeur dont quelqu'un d'autre utilise le compte l'aurait appris en
	// constatant qu'il ne reçoit plus d'appels. C'est la seule notification
	// qu'on n'a pas le droit de laisser couper.
	CategorySecurity = "security"
)

// categoryOf range une clé de message dans sa catégorie.
//
// ⚠️ `driver_call` a sa propre catégorie ET n'est pas coupable : c'est le
// gagne-pain du livreur, et un réglage mal compris le rendrait invisible du
// dispatch sans qu'il sache pourquoi. La catégorie existe pour l'affichage,
// pas pour l'interrupteur.
func categoryOf(key string) string {
	switch key {
	case KeyChatMessage:
		return CategoryChatMessages
	case KeyDriverCall:
		return CategoryDriverCall
	case KeyStaffDispatchFailed, KeyStaffDocumentSubmitted, KeyStaffDriverPending,
		KeyStaffTicketOpened, KeyStaffLostItemAnswered, KeyStaffEquipOverdue, KeyStaffEquipRequested,
		KeyStaffFinanceAlert, KeyPlatformAlert, KeyPlatformAlertResolved,
		// ⚠️ LE SOS EST RANGÉ AVEC LE STAFF, DONC NON COUPABLE (voir
		// `Muteable`). C'est la raison d'être de cette catégorie : un poste de
		// supervision qui peut se couper l'alarme d'urgence n'en est plus un.
		KeyStaffSOS, KeyStaffSOSClosed:
		return CategoryStaff
	case KeyEquipmentProposed, KeyEquipmentHandedOver, KeyEquipmentDue, KeyEquipmentCharged,
		KeyEquipmentOverdue, KeyEquipmentBlocked, KeyEquipmentReturned:
		// Ce qu'on doit et ce qu'on a payé : pas coupable, comme le support.
		return CategorySupport
	case KeyLostItemReported, KeyLostItemFound, KeyLostItemNotFound, KeyTicketReply, KeyTicketResolved:
		return CategorySupport
	case KeyVehicleAssigned, KeyVehicleTakenBack:
		// ⚠️ RANGÉES DANS `support`, DONC NON COUPABLES. C'est l'OUTIL DE
		// TRAVAIL de quelqu'un qui change, et le geste vient d'un tiers : un
		// chauffeur qui aurait coupé les offres commerciales apprendrait qu'on
		// lui a repris sa voiture en ne recevant plus d'appels.
		return CategorySupport
	case KeyChallengeReached:
		// ⚠️ RANGÉE DANS `support`, DONC NON COUPABLE — et ce n'est pas du
		// rangement par défaut. Un objectif atteint annonce un GAIN : quelqu'un
		// qui a travaillé pour un bonus doit apprendre qu'il l'a gagné, même
		// s'il a coupé les offres commerciales. La ranger dans `promotions`
		// aurait laissé un chauffeur ne jamais savoir qu'on venait de le payer.
		return CategorySupport
	case KeyDocumentsMissing:
		// ⚠️ RANGÉE DANS `support`, DONC NON COUPABLE. Un livreur qui couperait
		// cette catégorie ne saurait jamais ce qu'on lui demande d'envoyer, et
		// resterait « dossier incomplet » sans comprendre pourquoi. C'est aussi
		// ce qui oblige la relance à être RARE : on ne peut pas l'éteindre, donc
		// elle ne doit pas devenir du bruit — voir `RemindEvery`.
		return CategorySupport
	case KeySessionSuperseded:
		return CategorySecurity
	case KeyMerchantNewOrder:
		// Rangée avec les mises à jour de commande : c'est ce que c'est pour
		// le marchand, et lui donner une catégorie à part lui offrirait un
		// interrupteur pour couper son gagne-pain.
		return CategoryOrderUpdates
	default:
		return CategoryOrderUpdates
	}
}

// Muteable dit si une catégorie peut être coupée par son destinataire.
func Muteable(category string) bool {
	switch category {
	case CategoryDriverCall, CategoryAlerts, CategoryStaff, CategorySupport, CategorySecurity:
		return false
	default:
		return true
	}
}

// Inbox is one notification, kept so the user can read it again.
//
// La notification poussée ne laisse AUCUNE trace : un téléphone éteint, une
// bannière balayée, et le message n'existe plus nulle part. Le centre de
// notifications est ce qui le rend relisable — et ce qui donne un sens au
// badge de non-lus de l'écran Compte.
type Inbox struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	UserID   primitive.ObjectID `bson:"user_id"`
	Key      string             `bson:"key"`
	Category string             `bson:"category"`
	Title    string             `bson:"title"`
	Body     string             `bson:"body"`
	// Data est ce qui permet d'ouvrir la BONNE commande depuis la liste,
	// exactement comme depuis la bannière.
	Data      map[string]string `bson:"data,omitempty"`
	ReadAt    *time.Time        `bson:"read_at,omitempty"`
	CreatedAt time.Time         `bson:"created_at"`
}

// --- dépôt ---

// InsertInbox enregistre une notification.
func (r *Repository) InsertInbox(ctx context.Context, n *Inbox) error {
	if _, err := r.inbox.InsertOne(ctx, n); err != nil {
		return fmt.Errorf("notify: insert inbox: %w", err)
	}
	return nil
}

// ListInbox rend les notifications d'un compte, de la plus récente.
func (r *Repository) ListInbox(ctx context.Context, userID primitive.ObjectID, cursor primitive.ObjectID, limit int) ([]Inbox, error) {
	filter := bson.M{"user_id": userID}
	if !cursor.IsZero() {
		filter["_id"] = bson.M{"$lt": cursor}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))
	cur, err := r.inbox.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("notify: list inbox: %w", err)
	}
	var out []Inbox
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("notify: decode inbox: %w", err)
	}
	return out, nil
}

// CountUnread compte ce que le compte n'a pas lu — le badge de l'écran Compte.
func (r *Repository) CountUnread(ctx context.Context, userID primitive.ObjectID) (int64, error) {
	n, err := r.inbox.CountDocuments(ctx, bson.M{
		"user_id": userID, "read_at": bson.M{"$exists": false},
	})
	if err != nil {
		return 0, fmt.Errorf("notify: count unread: %w", err)
	}
	return n, nil
}

// SentSince dit si ce compte a DÉJÀ reçu ce message depuis `since`.
//
// ⚠️ ELLE LIT LA BOÎTE, ET C'EST TOUT L'INTÉRÊT. Savoir « quand a-t-on relancé
// cette personne ? » demandait sinon une table de relances à tenir, à purger et
// à borner par pays — alors que la réponse est déjà écrite là, puisque toute
// notification poussée laisse une entrée. Une seconde source aurait pu mentir ;
// celle-ci est la même que ce que la personne voit à l'écran.
func (r *Repository) SentSince(ctx context.Context, userID primitive.ObjectID, key string, since time.Time) (bool, error) {
	n, err := r.inbox.CountDocuments(ctx, bson.M{
		"user_id": userID, "key": key, "created_at": bson.M{"$gte": since},
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("notify: sent since: %w", err)
	}
	return n > 0, nil
}

// MarkInboxRead marque une notification lue, ou TOUTES quand id est nul.
func (r *Repository) MarkInboxRead(ctx context.Context, userID, id primitive.ObjectID, at time.Time) (int64, error) {
	filter := bson.M{"user_id": userID, "read_at": bson.M{"$exists": false}}
	if !id.IsZero() {
		filter["_id"] = id
	}
	res, err := r.inbox.UpdateMany(ctx, filter, bson.M{"$set": bson.M{"read_at": at}})
	if err != nil {
		return 0, fmt.Errorf("notify: mark inbox read: %w", err)
	}
	return res.ModifiedCount, nil
}

// --- service ---

// ListInbox rend le centre de notifications d'un compte, avec le non-lu.
func (s *Service) ListInbox(ctx context.Context, userID, cursor string, limit int) ([]InboxResponse, int, string, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, 0, "", apperr.Validation("invalid user id").WithCause(err)
	}
	after := primitive.NilObjectID
	if cursor != "" {
		if after, err = primitive.ObjectIDFromHex(cursor); err != nil {
			return nil, 0, "", apperr.Validation("invalid cursor").WithCause(err)
		}
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	items, err := s.repo.ListInbox(ctx, uid, after, limit)
	if err != nil {
		return nil, 0, "", err
	}
	unread, err := s.repo.CountUnread(ctx, uid)
	if err != nil {
		// Un compteur perdu ne doit pas emporter la liste elle-même.
		unread = 0
	}
	out := make([]InboxResponse, 0, len(items))
	for i := range items {
		out = append(out, toInboxResponse(items[i]))
	}
	next := ""
	if len(out) == limit && len(items) > 0 {
		next = items[len(items)-1].ID.Hex()
	}
	return out, int(unread), next, nil
}

// MarkRead marque une notification lue, ou toutes quand notificationID est vide.
func (s *Service) MarkRead(ctx context.Context, userID, notificationID string) (int, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return 0, apperr.Validation("invalid user id").WithCause(err)
	}
	id := primitive.NilObjectID
	if notificationID != "" {
		if id, err = primitive.ObjectIDFromHex(notificationID); err != nil {
			return 0, apperr.NotFound("notification_not_found", "notification not found").WithCause(err)
		}
	}
	n, err := s.repo.MarkInboxRead(ctx, uid, id, time.Now().UTC())
	return int(n), err
}

// NotifyOnce envoie un message SEULEMENT s'il n'est pas déjà parti récemment,
// et dit s'il est parti.
//
// ⚠️ C'EST LE GARDE-FOU D'UNE RELANCE, et il manquait. Une campagne qui redit la
// même chose chaque jour ne se lit plus : la personne coupe la catégorie — ou,
// quand elle n'est pas coupable, apprend à balayer la bannière sans la lire. Et
// c'est alors la relance suivante, celle qui compte, qui ne sera pas vue.
//
// ⚠️ LA FENÊTRE EST UN ARGUMENT, pas une constante : « on ne redit pas un
// document manquant avant trois jours » et « on ne redit pas une panne avant
// dix minutes » sont deux décisions différentes, et c'est à l'appelant de les
// prendre.
//
// ⚠️ ET UNE LECTURE RATÉE ENVOIE QUAND MÊME. Ne pas savoir si on a déjà
// prévenu quelqu'un ne doit pas empêcher de le prévenir : un doublon est un
// désagrément, un silence est une personne qui ne sait pas qu'il lui manque une
// pièce.
func (s *Service) NotifyOnce(ctx context.Context, userID, key string, within time.Duration, vars, data map[string]string) bool {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return false
	}
	if within > 0 {
		sent, err := s.repo.SentSince(ctx, uid, key, time.Now().UTC().Add(-within))
		if err != nil {
			slog.WarnContext(ctx, "notify: could not check recent sends, notifying anyway",
				"key", key, "error", err)
		} else if sent {
			return false
		}
	}
	s.Notify(ctx, userID, key, vars, data)
	return true
}
