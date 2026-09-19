package support

import (
	"context"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

const (
	riderID  = "6a9f000000000000000000c1"
	driverID = "6a9f000000000000000000d1"
	otherID  = "6a9f000000000000000000f1"
	adminID  = "6a9f000000000000000000a1"
	rideID   = "6a9f0000000000000000000a"
)

// --- doublures ---------------------------------------------------------------

type memStore struct {
	seq   int
	items map[primitive.ObjectID]*Ticket
}

func newMemStore() *memStore { return &memStore{items: map[primitive.ObjectID]*Ticket{}} }

func (m *memStore) NextReference(context.Context) (string, error) {
	m.seq++
	return "TCK-" + padded(m.seq), nil
}

func padded(n int) string {
	s := "000000" + itoa(n)
	return s[len(s)-6:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (m *memStore) Create(_ context.Context, t *Ticket) error {
	t.ID = primitive.NewObjectID()
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	cp := *t
	m.items[t.ID] = &cp
	return nil
}

func (m *memStore) ByID(_ context.Context, id primitive.ObjectID) (*Ticket, error) {
	t, ok := m.items[id]
	if !ok {
		return nil, errNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *memStore) List(_ context.Context, f Filter, limit int, _ string) ([]Ticket, string, error) {
	var out []Ticket
	for _, t := range m.items {
		if f.ParticipantID != nil && t.UserID != *f.ParticipantID &&
			(t.CounterpartID == nil || *t.CounterpartID != *f.ParticipantID) {
			continue
		}
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		if f.Category != "" && t.Category != f.Category {
			continue
		}
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Reference < out[j].Reference })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, "", nil
}

func (m *memStore) AppendMessage(_ context.Context, id primitive.ObjectID, msg Message) error {
	t, ok := m.items[id]
	if !ok {
		return errNotFound
	}
	t.Messages = append(t.Messages, msg)
	return nil
}

func (m *memStore) Update(_ context.Context, id primitive.ObjectID, upd Update) (*Ticket, error) {
	t, ok := m.items[id]
	if !ok {
		return nil, errNotFound
	}
	if upd.Status != nil {
		t.Status = *upd.Status
	}
	if upd.Priority != nil {
		t.Priority = *upd.Priority
	}
	if upd.AssignedTo != nil {
		t.AssignedTo = upd.AssignedTo
	}
	cp := *t
	return &cp, nil
}

func (m *memStore) AnswerLostItem(_ context.Context, id primitive.ObjectID, found bool, note, status string, at time.Time) (*Ticket, error) {
	t, ok := m.items[id]
	if !ok {
		return nil, errNotFound
	}
	li := *t.LostItem
	li.Found = &found
	li.AnsweredAt = &at
	li.Note = note
	t.LostItem = &li
	t.Status = status
	cp := *t
	return &cp, nil
}

type sent struct {
	to, key string
	vars    map[string]string
	data    map[string]string
}

type memNotifier struct {
	users []sent
	staff []sent
}

func (n *memNotifier) Notify(_ context.Context, userID, key string, vars, data map[string]string) {
	n.users = append(n.users, sent{to: userID, key: key, vars: vars, data: data})
}

func (n *memNotifier) NotifyStaff(_ context.Context, country, key string, vars, data map[string]string) {
	n.staff = append(n.staff, sent{to: country, key: key, vars: vars, data: data})
}

type memRefs struct{ ref *Reference }

func (r memRefs) ReferenceOf(context.Context, string) (*Reference, error) {
	if r.ref == nil {
		return nil, apperr.NotFound("ride_not_found", "ride not found")
	}
	return r.ref, nil
}

func rideDesk(t *testing.T, ref *Reference) (*Service, *memStore, *memNotifier) {
	t.Helper()
	store := newMemStore()
	n := &memNotifier{}
	svc := NewService(store, RefRide, memRefs{ref})
	svc.SetNotifier(n)
	return svc, store, n
}

func taken() *Reference {
	return &Reference{OwnerID: riderID, CounterpartID: driverID, Label: "Lomé Centre → Aéroport · 19 sept. 14:02"}
}

// --- ouvrir un ticket ---------------------------------------------------------

func TestOpenATicketWithoutAnyReference(t *testing.T) {
	svc, _, n := rideDesk(t, nil)
	resp, err := svc.Create(context.Background(), riderID, "client", CreateRequest{Category: CategoryPayment, Message: "débité deux fois"})
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^TCK-\d{6}$`), resp.Reference)
	assert.Equal(t, StatusOpen, resp.Status)
	assert.Equal(t, PriorityNormal, resp.Priority, "la priorité par défaut est normale")
	assert.Equal(t, "client", resp.Role)
	require.Len(t, resp.Messages, 1)
	assert.Equal(t, "client", resp.Messages[0].AuthorRole)
	assert.Empty(t, resp.RideID)
	// L'équipe est prévenue, le passager non — c'est lui qui écrit.
	require.Len(t, n.staff, 1)
	assert.Equal(t, KeyStaffTicketOpened, n.staff[0].key)
	assert.Equal(t, "Paiement", n.staff[0].vars["kind"])
	assert.Equal(t, "un client", n.staff[0].vars["who"])
	assert.Empty(t, n.users)
}

func TestTheDeskOnlyKnowsItsOwnVocabulary(t *testing.T) {
	svc, _, _ := rideDesk(t, taken())
	ctx := context.Background()

	// Une catégorie inconnue est nommée.
	_, err := svc.Create(ctx, riderID, "client", CreateRequest{Category: "order", Message: "x"})
	require.Error(t, err)
	assert.Equal(t, []string{"category"}, apperr.From(err).Meta["fields"])

	// La référence de l'autre verticale est un bug de l'application.
	_, err = svc.Create(ctx, riderID, "client", CreateRequest{Category: CategoryRide, OrderID: rideID, Message: "x"})
	require.Error(t, err)
	assert.Equal(t, []string{"order_id"}, apperr.From(err).Meta["fields"])
	assert.Equal(t, "wrong_vertical", apperr.From(err).Meta["reason"])

	assert.Equal(t, []string{CategoryRide, CategoryLostItem, CategoryPayment, CategoryTokens, CategoryAccount, CategoryBehaviour, CategoryOther}, svc.Categories())
}

func TestOnlyThoseWhoLivedTheRideMayComplainAboutIt(t *testing.T) {
	svc, _, _ := rideDesk(t, taken())
	ctx := context.Background()

	_, err := svc.Create(ctx, otherID, "client", CreateRequest{Category: CategoryRide, RideID: rideID, Message: "x"})
	require.Error(t, err)
	assert.Equal(t, "forbidden", apperr.From(err).Code)

	// Le chauffeur aussi peut signaler ce qui s'est passé pendant SA course.
	resp, err := svc.Create(ctx, driverID, "driver", CreateRequest{Category: CategoryBehaviour, RideID: rideID, Message: "le passager a refusé de payer"})
	require.NoError(t, err)
	assert.Equal(t, rideID, resp.RideID)
	assert.Equal(t, "Lomé Centre → Aéroport · 19 sept. 14:02", resp.RefLabel)
	assert.Empty(t, resp.CounterpartID, "hors objet perdu, personne d'autre n'est partie au ticket")
}

// --- l'objet perdu -------------------------------------------------------------

func TestALostItemNeedsTheRideAndTheItem(t *testing.T) {
	svc, _, _ := rideDesk(t, taken())
	ctx := context.Background()

	_, err := svc.Create(ctx, riderID, "client", CreateRequest{Category: CategoryLostItem, Message: "j'ai oublié mon sac"})
	require.Error(t, err)
	assert.Equal(t, []string{"ride_id"}, apperr.From(err).Meta["fields"])

	_, err = svc.Create(ctx, riderID, "client", CreateRequest{Category: CategoryLostItem, RideID: rideID, Message: "j'ai oublié mon sac"})
	require.Error(t, err)
	assert.Equal(t, []string{"lost_item"}, apperr.From(err).Meta["fields"])

	// Et l'inverse : un objet décrit sur un ticket de paiement est une
	// erreur, pas un détail à ignorer.
	_, err = svc.Create(ctx, riderID, "client", CreateRequest{Category: CategoryPayment, Message: "x", LostItem: &LostItemRequest{Item: "sac"}})
	require.Error(t, err)
	assert.Equal(t, "category_mismatch", apperr.From(err).Meta["reason"])
}

func TestALostItemWakesTheDriverAtOnce(t *testing.T) {
	svc, _, n := rideDesk(t, taken())
	resp, err := svc.Create(context.Background(), riderID, "client", CreateRequest{
		Category: CategoryLostItem, RideID: rideID, Message: "sur la banquette arrière",
		LostItem: &LostItemRequest{Item: "Sac à dos noir", Details: "avec un ordinateur"},
	})
	require.NoError(t, err)
	assert.Equal(t, PriorityHigh, resp.Priority, "un objet perdu est pressé")
	assert.Equal(t, driverID, resp.CounterpartID)
	require.NotNil(t, resp.LostItem)
	assert.Equal(t, "Sac à dos noir", resp.LostItem.Item)
	assert.Nil(t, resp.LostItem.Found, "pas encore regardé n'est pas « pas trouvé »")

	require.Len(t, n.users, 1)
	assert.Equal(t, driverID, n.users[0].to)
	assert.Equal(t, KeyLostItemReported, n.users[0].key)
	assert.Equal(t, "Sac à dos noir", n.users[0].vars["item"])
	assert.Equal(t, "lost_item", n.users[0].data["type"])
	assert.Equal(t, rideID, n.users[0].data["ride_id"])
	assert.Equal(t, resp.ID, n.users[0].data["ticket_id"])
	require.Len(t, n.staff, 1)
	assert.Equal(t, "Objet perdu", n.staff[0].vars["kind"])
}

func TestNothingGetsLostInACarNobodyDrove(t *testing.T) {
	svc, _, _ := rideDesk(t, &Reference{OwnerID: riderID})
	_, err := svc.Create(context.Background(), riderID, "client", CreateRequest{
		Category: CategoryLostItem, RideID: rideID, Message: "x", LostItem: &LostItemRequest{Item: "sac"},
	})
	require.Error(t, err)
	assert.Equal(t, "no_driver_yet", apperr.From(err).Code)
}

func TestOnlyThePassengerReportsALostItem(t *testing.T) {
	svc, _, _ := rideDesk(t, taken())
	_, err := svc.Create(context.Background(), driverID, "driver", CreateRequest{
		Category: CategoryLostItem, RideID: rideID, Message: "x", LostItem: &LostItemRequest{Item: "sac"},
	})
	require.Error(t, err)
	assert.Equal(t, "forbidden", apperr.From(err).Code)
}

func TestTheDriverAnswersAndThePassengerHears(t *testing.T) {
	svc, _, n := rideDesk(t, taken())
	ctx := context.Background()
	opened, err := svc.Create(ctx, riderID, "client", CreateRequest{
		Category: CategoryLostItem, RideID: rideID, Message: "x", LostItem: &LostItemRequest{Item: "Parapluie"},
	})
	require.NoError(t, err)
	n.users, n.staff = nil, nil

	// Le passager ne répond pas à sa propre question ; un tiers non plus.
	yes := true
	_, err = svc.AnswerLostItem(ctx, riderID, "client", opened.ID, LostItemAnswerRequest{Found: &yes})
	require.Error(t, err)
	assert.Equal(t, "forbidden", apperr.From(err).Code)
	_, err = svc.AnswerLostItem(ctx, otherID, "driver", opened.ID, LostItemAnswerRequest{Found: &yes})
	require.Error(t, err)

	// Le chauffeur, oui.
	answered, err := svc.AnswerLostItem(ctx, driverID, "driver", opened.ID, LostItemAnswerRequest{Found: &yes, Note: "sous le siège"})
	require.NoError(t, err)
	require.NotNil(t, answered.LostItem.Found)
	assert.True(t, *answered.LostItem.Found)
	assert.Equal(t, "sous le siège", answered.LostItem.Note)
	assert.Equal(t, StatusInProgress, answered.Status, "trouvé : le support organise la restitution")

	require.Len(t, n.users, 1)
	assert.Equal(t, riderID, n.users[0].to)
	assert.Equal(t, KeyLostItemFound, n.users[0].key)
	assert.Equal(t, "Parapluie", n.users[0].vars["item"])
	require.Len(t, n.staff, 1)
	assert.Equal(t, KeyStaffLostItemAnswered, n.staff[0].key)
	assert.Equal(t, "retrouvé", n.staff[0].vars["answer"])
}

func TestNotFoundLeavesTheTicketOpenForTheDesk(t *testing.T) {
	svc, _, n := rideDesk(t, taken())
	ctx := context.Background()
	opened, err := svc.Create(ctx, riderID, "client", CreateRequest{
		Category: CategoryLostItem, RideID: rideID, Message: "x", LostItem: &LostItemRequest{Item: "Parapluie"},
	})
	require.NoError(t, err)
	n.users = nil
	no := false
	answered, err := svc.AnswerLostItem(ctx, driverID, "driver", opened.ID, LostItemAnswerRequest{Found: &no})
	require.NoError(t, err)
	assert.False(t, *answered.LostItem.Found)
	assert.Equal(t, StatusOpen, answered.Status, "pas trouvé : c'est au support de décider, pas au chauffeur")
	require.Len(t, n.users, 1)
	assert.Equal(t, KeyLostItemNotFound, n.users[0].key)
}

func TestAnsweringANormalTicketIsRefused(t *testing.T) {
	svc, _, _ := rideDesk(t, taken())
	ctx := context.Background()
	opened, err := svc.Create(ctx, riderID, "client", CreateRequest{Category: CategoryPayment, Message: "x"})
	require.NoError(t, err)
	yes := true
	_, err = svc.AnswerLostItem(ctx, adminID, "admin", opened.ID, LostItemAnswerRequest{Found: &yes})
	require.Error(t, err)
	assert.Equal(t, "not_a_lost_item", apperr.From(err).Code)
}

// --- le fil --------------------------------------------------------------------

func TestTheThreadIsForItsPartiesAndTheDesk(t *testing.T) {
	svc, _, n := rideDesk(t, taken())
	ctx := context.Background()
	opened, err := svc.Create(ctx, riderID, "client", CreateRequest{
		Category: CategoryLostItem, RideID: rideID, Message: "x", LostItem: &LostItemRequest{Item: "Parapluie"},
	})
	require.NoError(t, err)
	n.users = nil

	_, err = svc.AddMessage(ctx, otherID, "client", opened.ID, "moi aussi")
	require.Error(t, err)
	assert.Equal(t, "forbidden", apperr.From(err).Code)
	_, err = svc.Get(ctx, otherID, "client", opened.ID)
	require.Error(t, err)

	// Le support écrit : le passager ET le chauffeur l'entendent.
	_, err = svc.AddMessage(ctx, adminID, "admin", opened.ID, "nous vous rappelons")
	require.NoError(t, err)
	require.Len(t, n.users, 2)
	assert.ElementsMatch(t, []string{riderID, driverID}, []string{n.users[0].to, n.users[1].to})
	assert.Equal(t, KeyTicketReply, n.users[0].key)
	assert.Equal(t, opened.Reference, n.users[0].vars["reference"])
	n.users = nil

	// Le chauffeur écrit : le passager l'entend, pas lui-même.
	withMsg, err := svc.AddMessage(ctx, driverID, "driver", opened.ID, "je regarde ce soir")
	require.NoError(t, err)
	assert.Len(t, withMsg.Messages, 3)
	assert.Equal(t, "driver", withMsg.Messages[2].AuthorRole)
	require.Len(t, n.users, 1)
	assert.Equal(t, riderID, n.users[0].to)

	// Chacun lit le fil.
	_, err = svc.Get(ctx, driverID, "driver", opened.ID)
	require.NoError(t, err)
}

func TestEachOneListsWhatConcernsThem(t *testing.T) {
	svc, _, _ := rideDesk(t, taken())
	ctx := context.Background()
	_, err := svc.Create(ctx, riderID, "client", CreateRequest{Category: CategoryPayment, Message: "a"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, riderID, "client", CreateRequest{
		Category: CategoryLostItem, RideID: rideID, Message: "b", LostItem: &LostItemRequest{Item: "sac"},
	})
	require.NoError(t, err)
	_, err = svc.Create(ctx, otherID, "client", CreateRequest{Category: CategoryOther, Message: "c"})
	require.NoError(t, err)

	page := httpx.Page{Limit: 20}
	mine, _, err := svc.List(ctx, riderID, "client", ListQuery{}, page)
	require.NoError(t, err)
	assert.Len(t, mine, 2)

	// Le chauffeur voit l'objet perdu qui le concerne, et rien d'autre.
	his, _, err := svc.List(ctx, driverID, "driver", ListQuery{}, page)
	require.NoError(t, err)
	require.Len(t, his, 1)
	assert.Equal(t, CategoryLostItem, his[0].Category)

	// L'équipe voit tout, et filtre — les filtres d'un client sont ignorés.
	all, _, err := svc.List(ctx, adminID, "admin", ListQuery{}, page)
	require.NoError(t, err)
	assert.Len(t, all, 3)
	lost, _, err := svc.List(ctx, adminID, "admin", ListQuery{Category: CategoryLostItem}, page)
	require.NoError(t, err)
	assert.Len(t, lost, 1)
	still, _, err := svc.List(ctx, riderID, "client", ListQuery{Category: CategoryOther}, page)
	require.NoError(t, err)
	assert.Len(t, still, 2)
}

// --- le guichet ----------------------------------------------------------------

type memAuditor struct{ actions []string }

func (a *memAuditor) Record(_ context.Context, action, _, _ string, _, _ any) {
	a.actions = append(a.actions, action)
}

func TestTheDeskClosesAndTheOpenerHearsOnce(t *testing.T) {
	svc, _, n := rideDesk(t, taken())
	aud := &memAuditor{}
	svc.SetAuditor(aud)
	ctx := context.Background()
	opened, err := svc.Create(ctx, riderID, "client", CreateRequest{Category: CategoryPayment, Message: "x"})
	require.NoError(t, err)
	n.users = nil

	inProgress, high, agent := StatusInProgress, PriorityHigh, adminID
	upd, err := svc.Update(ctx, opened.ID, UpdateRequest{Status: &inProgress, Priority: &high, AssignedTo: &agent})
	require.NoError(t, err)
	assert.Equal(t, StatusInProgress, upd.Status)
	assert.Equal(t, adminID, upd.AssignedTo)
	assert.Empty(t, n.users, "une prise en charge ne se notifie pas")

	resolved := StatusResolved
	_, err = svc.Update(ctx, opened.ID, UpdateRequest{Status: &resolved})
	require.NoError(t, err)
	require.Len(t, n.users, 1)
	assert.Equal(t, KeyTicketResolved, n.users[0].key)
	assert.Equal(t, riderID, n.users[0].to)

	// Refermer ce qui est déjà clos ne répète pas la notification.
	_, err = svc.Update(ctx, opened.ID, UpdateRequest{Status: &resolved})
	require.NoError(t, err)
	assert.Len(t, n.users, 1)
	assert.Equal(t, []string{"ticket.update", "ticket.update", "ticket.update"}, aud.actions)
}

func TestAnOrderDeskSpeaksOrders(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, RefOrder, memRefs{&Reference{OwnerID: riderID, CounterpartID: driverID, Label: "Commande #A1B2C"}})
	resp, err := svc.Create(context.Background(), riderID, "client", CreateRequest{Category: CategoryOrder, OrderID: rideID, Message: "froid"})
	require.NoError(t, err)
	assert.Equal(t, rideID, resp.OrderID)
	assert.Empty(t, resp.RideID)
	assert.Equal(t, "un livreur", svc.nameOf(context.Background(), driverID, "driver"))
}

func TestATicketFromBeforeTheExtractionStillNamesItsOrder(t *testing.T) {
	oid := primitive.NewObjectID()
	legacy := &Ticket{ID: primitive.NewObjectID(), UserID: primitive.NewObjectID(), LegacyOrderID: &oid, Category: CategoryOrder}
	resp := toResponse(legacy)
	assert.Equal(t, oid.Hex(), resp.OrderID)
	assert.Empty(t, resp.RideID)
	assert.NotNil(t, resp.Messages, "jamais null : une liste vide se parcourt")
}
