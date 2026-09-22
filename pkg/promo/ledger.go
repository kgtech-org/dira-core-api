package promo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// États d'un usage.
const (
	StateReserved = "reserved" // accordé, pas encore abouti
	StateSpent    = "spent"    // abouti : l'argent est sorti
	StateReleased = "released" // annulé : l'enveloppe est rendue
)

// Use est UN usage d'une promotion — la trace de ce qu'elle a coûté, et à
// l'occasion de quoi.
//
// ⚠️ `RefID` EST UNIQUE. C'est la course ou la commande, et c'est lui qui
// rend l'opération idempotente : un rappel de paiement rejoué, un
// redémarrage au mauvais moment, une double confirmation ne doivent pas
// consommer l'enveloppe deux fois. Sans cette contrainte, un budget se vide
// de moitié sur un incident réseau.
type Use struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	PromoID   string             `bson:"promo_id" json:"promo_id"`
	UserID    string             `bson:"user_id" json:"user_id"`
	RefID     string             `bson:"ref_id" json:"ref_id"`
	Country   string             `bson:"country,omitempty" json:"country,omitempty"`
	AmountXOF int                `bson:"amount_xof" json:"amount_xof"`
	State     string             `bson:"state" json:"state"`
	Title     string             `bson:"title,omitempty" json:"title,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	SettledAt *time.Time         `bson:"settled_at,omitempty" json:"settled_at,omitempty"`
}

// ErrAlreadyUsed : cet objet a déjà consommé une promotion. Ce n'est pas une
// panne — c'est la contrainte d'unicité qui fait son travail.
var ErrAlreadyUsed = errors.New("promo: this reference already used a promotion")

// Ledger tient le registre des usages ET les compteurs de l'enveloppe.
//
// ⚠️ LES DEUX ENSEMBLE, ET DANS CET ORDRE. Le registre est la VÉRITÉ — une
// ligne par usage, unique par référence ; les compteurs posés sur la
// promotion n'en sont qu'un résumé, pour que le devis n'ait pas à compter
// des milliers de lignes à chaque prix calculé. Écrire le résumé d'abord
// aurait laissé un compteur avancé sans usage derrière, le jour où
// l'insertion échoue.
type Ledger struct {
	uses   *mongo.Collection
	promos *mongo.Collection
}

// NewLedger branche le registre sur ses deux collections.
func NewLedger(uses, promos *mongo.Collection) *Ledger {
	return &Ledger{uses: uses, promos: promos}
}

// Reserve engage l'enveloppe pour une course ou une commande.
//
// Idempotent par `RefID` : rendre `ErrAlreadyUsed` signifie que l'usage
// existe déjà, et l'appelant n'a rien à faire de plus.
func (l *Ledger) Reserve(ctx context.Context, u Use) error {
	if l == nil || u.PromoID == "" || u.RefID == "" || u.AmountXOF <= 0 {
		return nil
	}
	u.State = StateReserved
	u.CreatedAt = time.Now().UTC()
	if _, err := l.uses.InsertOne(ctx, u); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrAlreadyUsed
		}
		return fmt.Errorf("promo: reserve: %w", err)
	}
	return l.bump(ctx, u.PromoID, bson.M{
		"uses_reserved": 1, "amount_reserved_xof": u.AmountXOF,
	})
}

// Settle acte la dépense : ce qui était promis est sorti.
func (l *Ledger) Settle(ctx context.Context, refID string) error {
	return l.close(ctx, refID, StateSpent, func(u Use) bson.M {
		return bson.M{
			"uses_reserved": -1, "amount_reserved_xof": -u.AmountXOF,
			"uses_spent": 1, "amount_spent_xof": u.AmountXOF,
		}
	})
}

// Release rend l'enveloppe : la course a été annulée, la commande refusée.
//
// ⚠️ L'ARGENT REVIENT DANS L'ENVELOPPE, et l'usage ne compte plus dans
// aucune limite. Une annulation qui garderait sa réservation ferait fondre
// un budget sans qu'un franc ne soit sorti — et personne ne comprendrait
// pourquoi l'offre s'est arrêtée.
func (l *Ledger) Release(ctx context.Context, refID string) error {
	return l.close(ctx, refID, StateReleased, func(u Use) bson.M {
		return bson.M{
			"uses_reserved": -1, "amount_reserved_xof": -u.AmountXOF,
			"uses_released": 1,
		}
	})
}

// close fait passer un usage RÉSERVÉ à son état final, une seule fois.
func (l *Ledger) close(ctx context.Context, refID, state string, delta func(Use) bson.M) error {
	if l == nil || refID == "" {
		return nil
	}
	now := time.Now().UTC()
	var u Use
	// Le filtre exige l'état `reserved` : un second appel ne trouve rien et
	// ne touche à aucun compteur. C'est ce qui rend l'opération sûre face à
	// un rappel rejoué.
	err := l.uses.FindOneAndUpdate(ctx,
		bson.M{"ref_id": refID, "state": StateReserved},
		bson.M{"$set": bson.M{"state": state, "settled_at": now}},
		options.FindOneAndUpdate().SetReturnDocument(options.Before)).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil // déjà réglé, ou aucune promotion sur cet objet
	}
	if err != nil {
		return fmt.Errorf("promo: close use: %w", err)
	}
	return l.bump(ctx, u.PromoID, delta(u))
}

// bump met à jour le RÉSUMÉ porté par la promotion.
func (l *Ledger) bump(ctx context.Context, promoID string, inc bson.M) error {
	oid, err := primitive.ObjectIDFromHex(promoID)
	if err != nil {
		return nil
	}
	if _, err := l.promos.UpdateByID(ctx, oid, bson.M{"$inc": inc}); err != nil {
		return fmt.Errorf("promo: counters: %w", err)
	}
	return nil
}

// UsesByUser compte ce qu'une personne a déjà tiré de CHAQUE promotion.
//
// ⚠️ UNE SEULE LECTURE POUR TOUTES LES PROMOTIONS, pas une par offre. Elle
// est faite une fois par devis, avant de tarifer les classes ; une lecture
// par promotion et par classe aurait multiplié la même requête sur le chemin
// le plus chaud du service.
//
// Les usages RENDUS ne comptent pas : une course annulée ne doit pas priver
// quelqu'un de son offre.
func (l *Ledger) UsesByUser(ctx context.Context, userID string) (map[string]int, error) {
	out := map[string]int{}
	if l == nil || userID == "" {
		return out, nil
	}
	cur, err := l.uses.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"user_id": userID,
			"state":   bson.M{"$in": bson.A{StateReserved, StateSpent}},
		}}},
		{{Key: "$group", Value: bson.M{"_id": "$promo_id", "n": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return nil, fmt.Errorf("promo: uses by user: %w", err)
	}
	var rows []struct {
		PromoID string `bson:"_id"`
		N       int    `bson:"n"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("promo: uses by user decode: %w", err)
	}
	for _, r := range rows {
		out[r.PromoID] = r.N
	}
	return out, nil
}

// History rend les derniers usages d'une promotion — ce qu'une console
// montre sous la barre d'enveloppe.
func (l *Ledger) History(ctx context.Context, promoID string, limit int) ([]Use, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cur, err := l.uses.Find(ctx, bson.M{"promo_id": promoID},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("promo: history: %w", err)
	}
	var out []Use
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("promo: history decode: %w", err)
	}
	return out, nil
}
