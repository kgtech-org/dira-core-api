// Package chat carries the conversation between a client and the driver who
// serves them — celui qui apporte leur commande, ou celui qui vient les
// chercher.
//
// Elle existe pour une raison précise : permettre à ces deux-là de se parler
// SANS échanger leurs numéros. « Je suis au portail bleu », « je monte dans
// cinq minutes » — des phrases qui n'ont de sens que pendant la course, et qui
// ne justifient pas de donner son téléphone à un inconnu.
//
// ⚠️ CE PAQUET EST UNE BIBLIOTHÈQUE, PAS UN SERVICE, et le choix est délibéré.
//
// Les deux verticales ont besoin du même COMPORTEMENT — qui peut écrire,
// jusqu'à quand, comment se marque un message lu — mais pas des mêmes DONNÉES :
// une conversation de commande et une conversation de course ne se lisent
// jamais ensemble. En faire un service aurait coûté un TROISIÈME socket dans
// les applications, et une synchronisation d'état entre services dont l'échec
// se serait vu comme « aucun chauffeur assigné » sans que personne comprenne
// pourquoi.
//
// Chaque verticale câble donc sa propre collection, ses propres participants et
// son propre canal temps réel. Ce qui est partagé, c'est la règle.
package chat

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Ce à quoi une conversation se rattache.
//
// Le COUPLE (genre, identifiant) fait l'identité, comme au grand livre du
// portefeuille : deux verticales peuvent porter le même identifiant sans se
// confondre — deux collections, deux compteurs d'ObjectID, aucune garantie
// d'unicité entre elles.
const (
	RefOrder = "order"
	RefRide  = "ride"
)

// Rôles d'un participant. FIGÉS à l'envoi : un livreur qui perdrait la course
// ne doit pas voir ses messages passés changer de camp.
const (
	SenderClient = "client"
	SenderDriver = "driver"
)

// MaxBody bounds one message. Au-delà, ce n'est plus un mot au livreur.
const MaxBody = 1000

// WriteWindowAfterCompletion is how long the conversation stays writable once
// the course is over — commande livrée, passager déposé.
//
// ⚠️ Réglage produit, à valider. Ni fermée à l'arrivée — « vous avez laissé le
// sac chez le voisin », « j'ai oublié mon téléphone sur la banquette » se
// disent dans la minute qui suit —, ni ouverte pour toujours : un mois plus
// tard, le chauffeur n'a plus ni contexte ni raison de répondre, et l'autre
// attendrait une réponse qui ne viendra pas.
const WriteWindowAfterCompletion = 2 * time.Hour

// Message is one line of a conversation.
type Message struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
	// RefID et RefKind disent SUR QUOI l'on parle : une commande de repas, une
	// course. Le genre est écrit même quand une collection n'en contient qu'un
	// seul — une ligne qui ne dit pas de quoi elle parle oblige à connaître sa
	// collection pour se lire.
	RefID   primitive.ObjectID `bson:"ref_id"`
	RefKind string             `bson:"ref_kind"`
	// SenderID est l'utilisateur qui écrit ; SenderRole dit de quel côté.
	SenderID   primitive.ObjectID `bson:"sender_id"`
	SenderRole string             `bson:"sender_role"`
	Body       string             `bson:"body"`
	CreatedAt  time.Time          `bson:"created_at"`
	// ReadAt est l'instant où le DESTINATAIRE l'a lu. Porté par le message et
	// non par un compteur sur la conversation : un compteur se désynchronise,
	// et personne ne sait plus lequel des deux dit vrai.
	ReadAt *time.Time `bson:"read_at,omitempty"`
}
