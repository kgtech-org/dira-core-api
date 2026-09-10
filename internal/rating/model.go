// Package rating records what a client thought of a delivered order — of the
// driver who brought it, and of the shops that made it.
package rating

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection is the MongoDB collection backing the module.
const Collection = "ratings"

// Cibles d'une note.
//
// Le MARCHAND est noté au niveau du POINT DE VENTE, pas de l'enseigne : une
// commande peut traverser deux boutiques d'une même marque, et le client a
// vécu l'une et pas l'autre. Une note d'enseigne se calcule à partir de
// celles-ci ; l'inverse ne se démêle plus.
const (
	TargetDriver = "driver"
	TargetStore  = "store"
	// TargetDish note le PLAT lui-même, indépendamment de qui l'a fait et de
	// qui l'a apporté. Un plat raté chez un bon restaurant se dit, et c'est
	// ce que lit le client suivant devant la carte.
	TargetDish = "dish"
)

// Bornes de la note. Cinq crans, entiers : une demi-étoile n'ajoute pas
// d'information et complique tout affichage.
const (
	MinScore = 1
	MaxScore = 5
)

// Rating is one score left by a client on one target of one order.
//
// UNE note par (commande, cible) — l'unicité est portée par un index, pas par
// une lecture préalable : deux envois simultanés passeraient tous les deux le
// contrôle applicatif et compteraient double.
type Rating struct {
	ID         primitive.ObjectID `bson:"_id,omitempty"`
	OrderID    primitive.ObjectID `bson:"order_id"`
	ClientID   primitive.ObjectID `bson:"client_id"`
	TargetType string             `bson:"target_type"` // "driver" | "store"
	TargetID   primitive.ObjectID `bson:"target_id"`
	Score      int                `bson:"score"`
	Comment    string             `bson:"comment,omitempty"`
	CreatedAt  time.Time          `bson:"created_at"`
}
