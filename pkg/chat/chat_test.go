package chat

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

const (
	clientID = "6a9f000000000000000000c1"
	driverID = "6a9f000000000000000000d1"
	otherID  = "6a9f000000000000000000f1"
	orderID  = "6a9f0000000000000000000a"
)

func conv(driver string, status string, delivered *time.Time) *Conversation {
	return &Conversation{ClientID: clientID, DriverUserID: driver, Status: status, CompletedAt: delivered}
}

// --- qui est de quel côté ---------------------------------------------------

func TestRoleOfIdentifiesEachSideAndNobodyElse(t *testing.T) {
	c := conv(driverID, "delivering", nil)
	assert.Equal(t, SenderClient, roleOf(clientID, c))
	assert.Equal(t, SenderDriver, roleOf(driverID, c))
	assert.Empty(t, roleOf(otherID, c), "un tiers n'est d'aucun côté")
	// Un administrateur non plus : il LIT pour le support. Écrire en son nom
	// ferait passer un message de la plateforme pour celui d'un livreur.
	assert.Empty(t, roleOf(otherID, c))
}

func TestNobodyIsTheDriverBeforeSomeoneTakesTheCourse(t *testing.T) {
	c := conv("", "paid", nil)
	assert.Empty(t, roleOf(driverID, c), "la course n'est prise par personne")
	assert.Equal(t, SenderClient, roleOf(clientID, c), "le client, lui, existe déjà")
}

// --- jusqu'à quand on peut écrire ------------------------------------------

// Un message parti dans le vide serait pire qu'un refus : le client
// attendrait une réponse qui ne viendra jamais.
func TestNoWritingBeforeADriverHasTakenTheCourse(t *testing.T) {
	err := writable(conv("", "paid", nil))
	require.Error(t, err)
	assert.Equal(t, "no_driver_yet", apperr.From(err).Code)
}

func TestWritingIsOpenDuringTheCourse(t *testing.T) {
	require.NoError(t, writable(conv(driverID, "picking_up", nil)))
	require.NoError(t, writable(conv(driverID, "delivering", nil)))
}

// Ni fermée à la livraison — « vous avez laissé le sac chez le voisin » se dit
// dans la minute —, ni ouverte pour toujours.
func TestWritingStaysOpenAWhileAfterDeliveryThenCloses(t *testing.T) {
	justNow := time.Now().Add(-5 * time.Minute)
	require.NoError(t, writable(conv(driverID, "delivered", &justNow)))

	longAgo := time.Now().Add(-WriteWindowAfterCompletion - time.Minute)
	err := writable(conv(driverID, "delivered", &longAgo))
	require.Error(t, err)
	assert.Equal(t, "conversation_closed", apperr.From(err).Code)
}

func TestACancelledOrderClosesTheConversation(t *testing.T) {
	err := writable(conv(driverID, "cancelled", nil))
	require.Error(t, err)
	assert.Equal(t, "conversation_closed", apperr.From(err).Code)
}

// --- ce qu'un message laisse voir ------------------------------------------

// TOUTE la raison d'être de cette conversation est que les deux se parlent
// sans échanger leurs coordonnées. La réponse ne doit donc porter ni
// identifiant d'utilisateur, ni téléphone — seulement un côté.
func TestAMessageNeverRevealsWhoSentIt(t *testing.T) {
	sender, err := primitive.ObjectIDFromHex(driverID)
	require.NoError(t, err)
	m := Message{SenderID: sender, SenderRole: SenderDriver, Body: "je suis en bas", CreatedAt: time.Now()}
	resp := toResponse(m)

	assert.Equal(t, SenderDriver, resp.From)
	assert.Equal(t, "je suis en bas", resp.Body)
	// Le seul champ d'identité est le RÔLE.
	assert.NotContains(t, strings.ToLower(resp.ID+resp.From), driverID)
}

// --- bornes -----------------------------------------------------------------

type fakeParties struct{ c *Conversation }

func (f fakeParties) ConversationOf(context.Context, string) (*Conversation, error) { return f.c, nil }

func TestAThirdPartyCannotEvenReadTheConversation(t *testing.T) {
	svc := NewService(nil, RefOrder, fakeParties{c: conv(driverID, "delivering", nil)})
	_, _, err := svc.List(context.Background(), otherID, "client", orderID, "", 20)
	require.Error(t, err)
	assert.Equal(t, 403, apperr.From(err).HTTPStatus)
}

// --- le GENRE de la référence -----------------------------------------------

// ⚠️ Une instance ne sert QU'UNE verticale, et le genre est fixé à sa
// construction. Le passer à chaque envoi laisserait la possibilité d'écrire une
// course dans les conversations de commandes — et une ligne du grand livre des
// messages ne dirait plus de quoi elle parle.
func TestTheReferenceKindIsFixedAtConstruction(t *testing.T) {
	food := NewService(nil, RefOrder, fakeParties{c: conv(driverID, "delivering", nil)})
	vtc := NewService(nil, RefRide, fakeParties{c: conv(driverID, "onboard", nil)})

	assert.Equal(t, RefOrder, food.refKind)
	assert.Equal(t, RefRide, vtc.refKind)
	// Deux genres distincts : c'est ce qui permet aux deux verticales de
	// porter le même identifiant sans jamais se confondre.
	assert.NotEqual(t, food.refKind, vtc.refKind)
}

// La fenêtre d'écriture ne dépend PAS de la verticale : « vous avez laissé le
// sac chez le voisin » et « j'ai oublié mon téléphone sur la banquette » se
// disent dans la minute qui suit, et n'ont plus de sens un mois plus tard.
// C'est exactement le genre de règle qu'on ne veut pas écrire deux fois.
func TestTheWriteWindowClosesAfterCompletion(t *testing.T) {
	long := time.Now().Add(-WriteWindowAfterCompletion - time.Minute)
	recent := time.Now().Add(-time.Minute)

	require.NoError(t, writable(conv(driverID, "completed", &recent)))
	require.Error(t, writable(conv(driverID, "completed", &long)))
}
