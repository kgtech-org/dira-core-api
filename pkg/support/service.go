package support

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Notification keys — rendered by the core's `internal/notify` templates.
//
// Des CHAÎNES, pas des constantes du socle : ce paquet est importé par les
// verticales, qui ne doivent pas tirer les gabarits avec lui. Un test du
// socle vérifie que chaque clé a bien son gabarit.
const (
	// KeyLostItemReported va au CHAUFFEUR / LIVREUR : un passager dit avoir
	// oublié quelque chose dans son véhicule. Vars : item, ref.
	KeyLostItemReported = "lost_item_reported"
	// KeyLostItemFound / KeyLostItemNotFound vont au PASSAGER : la réponse du
	// chauffeur. Vars : item.
	KeyLostItemFound    = "lost_item_found"
	KeyLostItemNotFound = "lost_item_not_found"
	// KeyTicketReply : quelqu'un a écrit sur un ticket qu'on a ouvert, ou qui
	// nous concerne. Vars : reference.
	KeyTicketReply = "ticket_reply"
	// KeyTicketResolved : le support a clos la demande. Vars : reference.
	KeyTicketResolved = "ticket_resolved"
	// KeyStaffTicketOpened / KeyStaffLostItemAnswered : les alertes de
	// l'équipe. Vars : kind, ref, who — et answer pour la seconde.
	KeyStaffTicketOpened     = "staff_ticket_opened"
	KeyStaffLostItemAnswered = "staff_lost_item_answered"
)

var (
	errNotFound  = apperr.NotFound("ticket_not_found", "ticket not found")
	errForbidden = apperr.Forbidden("forbidden", "this ticket is not yours")
	// errNotYourRef : on ne se plaint que de ce qu'on a vécu. Le message ne
	// dit ni « commande » ni « course » — ce paquet sert les deux.
	errNotYourRef = apperr.Forbidden("forbidden", "this reference is not yours")
	// errNoDriver : un objet ne se perd pas dans un véhicule que personne
	// n'a conduit.
	errNoDriver    = apperr.Conflict("no_driver_yet", "nobody has taken this yet")
	errNotLostItem = apperr.Conflict("not_a_lost_item", "this ticket is not about a lost item")
)

// Reference is what the vertical knows about the thing a ticket is about.
//
// Un seul appel rend les trois faits : à qui c'est, qui la portait, comment
// la dire. Le service n'a pas à savoir ce qu'est une commande.
type Reference struct {
	// OwnerID est le compte du client.
	OwnerID string
	// CounterpartID est le compte du chauffeur ou du livreur — vide tant que
	// personne n'a pris la course.
	CounterpartID string
	// Label est la référence DITE : « Lomé Centre → Aéroport · 19 sept.
	// 14:02 », ou « Commande #A1B2C · Chez Fifi ».
	Label string
}

// Refs resolves a reference. Implemented at wiring time from the ride or
// order module — ce paquet n'importe ni l'un ni l'autre.
type Refs interface {
	ReferenceOf(ctx context.Context, refID string) (*Reference, error)
}

// Notifier reaches people and the operations team. Facultatif : sans lui,
// le guichet fonctionne, il se lit simplement à l'ouverture de l'écran.
type Notifier interface {
	Notify(ctx context.Context, userID, key string, vars, data map[string]string)
	NotifyStaff(ctx context.Context, country, key string, vars, data map[string]string)
}

// Auditor records what the operations team changed. Facultatif.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Directory names a person for the team's alerts. Facultatif : sans lui,
// l'alerte dit « un client ».
type Directory interface {
	NameOf(ctx context.Context, userID string) string
}

// Store is what the service needs from persistence — déclaré ici, côté
// consommateur, pour que la logique se teste sans Mongo.
type Store interface {
	NextReference(ctx context.Context) (string, error)
	Create(ctx context.Context, t *Ticket) error
	ByID(ctx context.Context, id primitive.ObjectID) (*Ticket, error)
	List(ctx context.Context, f Filter, limit int, cursor string) ([]Ticket, string, error)
	AppendMessage(ctx context.Context, ticketID primitive.ObjectID, msg Message) error
	Update(ctx context.Context, ticketID primitive.ObjectID, upd Update) (*Ticket, error)
	AnswerLostItem(ctx context.Context, ticketID primitive.ObjectID, found bool, note, status string, at time.Time) (*Ticket, error)
}

type Service struct {
	store Store
	// refKind dit de quoi l'on parle ici — `RefOrder` ou `RefRide`. Il tient
	// dans le service et non dans l'appel : une instance ne sert qu'une
	// verticale, et le passer à chaque appel laisserait la possibilité
	// d'ouvrir un ticket de course chez la livraison.
	refKind   string
	refs      Refs
	notifier  Notifier
	auditor   Auditor
	directory Directory
}

// NewService builds the support desk of ONE vertical.
func NewService(store Store, refKind string, refs Refs) *Service {
	return &Service{store: store, refKind: refKind, refs: refs}
}

// SetNotifier branche les notifications (câblage).
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// SetAuditor branche le journal d'audit (câblage).
func (s *Service) SetAuditor(a Auditor) { s.auditor = a }

// SetDirectory branche l'annuaire, pour nommer les gens dans les alertes.
func (s *Service) SetDirectory(d Directory) { s.directory = d }

// RefKind rend ce dont ce guichet parle.
func (s *Service) RefKind() string { return s.refKind }

// tripCategory est la catégorie « pendant la course / la commande » de
// cette verticale.
func (s *Service) tripCategory() string {
	if s.refKind == RefRide {
		return CategoryRide
	}
	return CategoryOrder
}

// Categories rend celles que ce guichet accepte, dans l'ordre où une
// application les propose.
func (s *Service) Categories() []string {
	return []string{s.tripCategory(), CategoryLostItem, CategoryPayment, CategoryTokens, CategoryAccount, CategoryBehaviour, CategoryOther}
}

func (s *Service) categoryAllowed(c string) bool {
	for _, k := range s.Categories() {
		if k == c {
			return true
		}
	}
	return false
}

// refField est le nom du champ de référence dans le vocabulaire de la
// verticale — ce que `fields` nomme dans un 422.
func (s *Service) refField() string {
	if s.refKind == RefRide {
		return "ride_id"
	}
	return "order_id"
}

func invalid(field, reason string) error {
	return apperr.Validation("invalid " + field).
		WithMeta(map[string]any{"fields": []string{field}, "reason": reason})
}

// Create opens a ticket for the caller with an initial message.
//
// Le cas OBJET PERDU : la référence est obligatoire, le chauffeur qui la
// portait devient partie au ticket et est prévenu sur-le-champ — c'est dans
// les minutes qui suivent qu'un sac se retrouve, pas quand le support ouvre
// sa file le lendemain.
func (s *Service) Create(ctx context.Context, userID, role string, req CreateRequest) (*Response, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	if !s.categoryAllowed(req.Category) {
		return nil, invalid("category", "unknown_category")
	}
	// La référence de l'AUTRE verticale est un bug de l'application, pas une
	// référence : refusée en la nommant.
	if s.refKind == RefRide && req.OrderID != "" {
		return nil, invalid("order_id", "wrong_vertical")
	}
	if s.refKind == RefOrder && req.RideID != "" {
		return nil, invalid("ride_id", "wrong_vertical")
	}
	refHex := req.OrderID
	if s.refKind == RefRide {
		refHex = req.RideID
	}
	lost := req.Category == CategoryLostItem
	if lost && refHex == "" {
		return nil, invalid(s.refField(), "required_for_lost_item")
	}
	if lost && req.LostItem == nil {
		return nil, invalid("lost_item", "required_for_lost_item")
	}
	if !lost && req.LostItem != nil {
		return nil, invalid("lost_item", "category_mismatch")
	}

	t := &Ticket{
		UserID:   uid,
		Role:     role,
		Country:  country.FromContext(ctx),
		Category: req.Category,
		Priority: req.Priority,
		Status:   StatusOpen,
	}
	if t.Priority == "" {
		t.Priority = PriorityNormal
	}
	var ref *Reference
	if refHex != "" {
		rid, err := primitive.ObjectIDFromHex(refHex)
		if err != nil {
			return nil, invalid(s.refField(), "invalid_id")
		}
		ref, err = s.refs.ReferenceOf(ctx, refHex)
		if err != nil {
			return nil, err
		}
		// On ne se plaint que de ce qu'on a vécu — des deux côtés : un
		// chauffeur peut aussi signaler ce qui s'est passé pendant SA course.
		if ref.OwnerID != userID && (ref.CounterpartID == "" || ref.CounterpartID != userID) {
			return nil, errNotYourRef
		}
		t.RefKind = s.refKind
		t.RefID = &rid
		t.RefLabel = ref.Label
	}
	if lost {
		// Seul le passager perd quelque chose DANS le véhicule ; et il faut
		// un véhicule. Un chauffeur qui trouve un objet ouvre un ticket de
		// course, le support fait le lien.
		if ref.OwnerID != userID {
			return nil, errNotYourRef
		}
		if ref.CounterpartID == "" {
			return nil, errNoDriver
		}
		cid, err := primitive.ObjectIDFromHex(ref.CounterpartID)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		t.CounterpartID = &cid
		t.LostItem = &LostItem{Item: strings.TrimSpace(req.LostItem.Item), Details: strings.TrimSpace(req.LostItem.Details)}
		// Un objet perdu est pressé par nature : la priorité monte, jamais
		// elle ne descend.
		if t.Priority == PriorityLow || t.Priority == PriorityNormal {
			t.Priority = PriorityHigh
		}
	}

	reference, err := s.store.NextReference(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	t.Reference = reference
	now := time.Now().UTC()
	t.Messages = []Message{{AuthorID: uid, AuthorRole: role, Body: req.Message, At: now}}
	if err := s.store.Create(ctx, t); err != nil {
		return nil, err
	}

	if s.notifier != nil {
		data := s.data(t)
		if lost {
			s.notifier.Notify(ctx, ref.CounterpartID, KeyLostItemReported,
				map[string]string{"item": t.LostItem.Item, "ref": t.RefLabel}, data)
		}
		s.notifier.NotifyStaff(ctx, t.Country, KeyStaffTicketOpened,
			map[string]string{"kind": CategoryLabel(t.Category), "ref": t.Reference, "who": s.nameOf(ctx, userID, role)}, data)
	}
	resp := toResponse(t)
	return &resp, nil
}

// List returns the caller's tickets — ceux qu'il a ouverts et ceux qui le
// concernent ; admins see all tickets and may filter.
func (s *Service) List(ctx context.Context, userID, role string, q ListQuery, page httpx.Page) ([]Response, string, error) {
	f := Filter{}
	if role != auth.RoleAdmin {
		uid, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			return nil, "", apperr.Validation("invalid user id").WithCause(err)
		}
		f.ParticipantID = &uid
	} else {
		f.Status = q.Status
		f.Category = q.Category
		if q.AssignedTo != "" {
			aid, err := primitive.ObjectIDFromHex(q.AssignedTo)
			if err != nil {
				return nil, "", apperr.Validation("invalid assigned_to").WithCause(err)
			}
			f.AssignedTo = &aid
		}
	}
	items, next, err := s.store.List(ctx, f, page.Limit, page.Cursor)
	if err != nil {
		return nil, "", err
	}
	out := make([]Response, 0, len(items))
	for i := range items {
		out = append(out, toResponse(&items[i]))
	}
	return out, next, nil
}

// Get returns one ticket to a participant or an admin.
func (s *Service) Get(ctx context.Context, userID, role, ticketID string) (*Response, error) {
	t, err := s.load(ctx, userID, role, ticketID)
	if err != nil {
		return nil, err
	}
	resp := toResponse(t)
	return &resp, nil
}

// AddMessage appends a message; participants and admins may post. L'autre
// côté est prévenu : une réponse qu'on ne voit pas n'est pas une réponse.
func (s *Service) AddMessage(ctx context.Context, userID, role, ticketID, body string) (*Response, error) {
	t, err := s.load(ctx, userID, role, ticketID)
	if err != nil {
		return nil, err
	}
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	msg := Message{AuthorID: uid, AuthorRole: role, Body: body, At: time.Now().UTC()}
	if err := s.store.AppendMessage(ctx, t.ID, msg); err != nil {
		return nil, err
	}
	t.Messages = append(t.Messages, msg)
	if s.notifier != nil {
		vars := map[string]string{"reference": t.Reference}
		data := s.data(t)
		for _, p := range participants(t) {
			if p != userID {
				s.notifier.Notify(ctx, p, KeyTicketReply, vars, data)
			}
		}
	}
	resp := toResponse(t)
	return &resp, nil
}

// AnswerLostItem records what the driver found. Trouvé : le support prend
// la main pour organiser la restitution (`in_progress`). Pas trouvé : le
// ticket reste ouvert — c'est au support de décider, pas au chauffeur.
func (s *Service) AnswerLostItem(ctx context.Context, userID, role, ticketID string, req LostItemAnswerRequest) (*Response, error) {
	t, err := s.load(ctx, userID, role, ticketID)
	if err != nil {
		return nil, err
	}
	if t.LostItem == nil {
		return nil, errNotLostItem
	}
	// Le passager ne répond pas à sa propre question.
	if role != auth.RoleAdmin && (t.CounterpartID == nil || t.CounterpartID.Hex() != userID) {
		return nil, errForbidden
	}
	found := req.Found != nil && *req.Found
	status := t.Status
	if found && (status == StatusOpen || status == StatusWaiting) {
		status = StatusInProgress
	}
	now := time.Now().UTC()
	after, err := s.store.AnswerLostItem(ctx, t.ID, found, strings.TrimSpace(req.Note), status, now)
	if err != nil {
		return nil, err
	}
	if s.notifier != nil {
		key := KeyLostItemNotFound
		answer := "non retrouvé"
		if found {
			key = KeyLostItemFound
			answer = "retrouvé"
		}
		data := s.data(after)
		s.notifier.Notify(ctx, after.UserID.Hex(), key, map[string]string{"item": after.LostItem.Item}, data)
		s.notifier.NotifyStaff(ctx, after.Country, KeyStaffLostItemAnswered,
			map[string]string{"ref": after.Reference, "who": s.nameOf(ctx, userID, role), "answer": answer, "item": after.LostItem.Item}, data)
	}
	resp := toResponse(after)
	return &resp, nil
}

// Update lets an admin change status, priority and assignment; audited. Une
// demande close est DITE à celui qui l'a ouverte.
func (s *Service) Update(ctx context.Context, ticketID string, req UpdateRequest) (*Response, error) {
	tid, err := primitive.ObjectIDFromHex(ticketID)
	if err != nil {
		return nil, errNotFound
	}
	upd := Update{Status: req.Status, Priority: req.Priority}
	if req.AssignedTo != nil {
		aid, err := primitive.ObjectIDFromHex(*req.AssignedTo)
		if err != nil {
			return nil, apperr.Validation("invalid assigned_to").WithCause(err)
		}
		upd.AssignedTo = &aid
	}
	before, err := s.store.ByID(ctx, tid)
	if err != nil {
		return nil, err
	}
	after, err := s.store.Update(ctx, tid, upd)
	if err != nil {
		return nil, err
	}
	if s.auditor != nil {
		s.auditor.Record(ctx, "ticket.update", "ticket", ticketID, auditView(before), auditView(after))
	}
	closed := after.Status == StatusResolved || after.Status == StatusClosed
	if s.notifier != nil && closed && before.Status != after.Status {
		s.notifier.Notify(ctx, after.UserID.Hex(), KeyTicketResolved,
			map[string]string{"reference": after.Reference}, s.data(after))
	}
	resp := toResponse(after)
	return &resp, nil
}

// load rend le ticket si l'appelant peut le voir : une partie, ou l'admin.
func (s *Service) load(ctx context.Context, userID, role, ticketID string) (*Ticket, error) {
	tid, err := primitive.ObjectIDFromHex(ticketID)
	if err != nil {
		return nil, errNotFound
	}
	t, err := s.store.ByID(ctx, tid)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && !isParticipant(t, userID) {
		return nil, errForbidden
	}
	return t, nil
}

// participants sont ceux que le fil concerne : qui l'a ouvert, et le
// chauffeur d'un objet perdu.
func participants(t *Ticket) []string {
	out := []string{t.UserID.Hex()}
	if t.CounterpartID != nil {
		out = append(out, t.CounterpartID.Hex())
	}
	return out
}

func isParticipant(t *Ticket, userID string) bool {
	for _, p := range participants(t) {
		if p == userID {
			return true
		}
	}
	return false
}

// data est ce que l'application ouvre en touchant la notification.
func (s *Service) data(t *Ticket) map[string]string {
	d := map[string]string{"type": "ticket", "ticket_id": t.ID.Hex(), "ref_kind": s.refKind}
	if t.LostItem != nil {
		d["type"] = "lost_item"
	}
	if id := t.refID(); id != nil {
		d[s.refField()] = id.Hex()
	}
	return d
}

func (s *Service) nameOf(ctx context.Context, userID, role string) string {
	if s.directory != nil {
		if n := strings.TrimSpace(s.directory.NameOf(ctx, userID)); n != "" {
			return n
		}
	}
	switch role {
	case auth.RoleDriver:
		if s.refKind == RefRide {
			return "un chauffeur"
		}
		return "un livreur"
	case auth.RoleMerchant:
		return "un marchand"
	default:
		return "un client"
	}
}

func auditView(t *Ticket) map[string]any {
	v := map[string]any{"status": t.Status, "priority": t.Priority}
	if t.AssignedTo != nil {
		v["assigned_to"] = t.AssignedTo.Hex()
	}
	return v
}

// CategoryLabel dit une catégorie comme l'équipe la lit — dans ses alertes,
// qui ne sont pas traduites : l'exploitation travaille en français.
func CategoryLabel(c string) string {
	switch c {
	case CategoryOrder:
		return "Commande"
	case CategoryRide:
		return "Course"
	case CategoryPayment:
		return "Paiement"
	case CategoryTokens:
		return "Jetons"
	case CategoryAccount:
		return "Compte"
	case CategoryLostItem:
		return "Objet perdu"
	case CategoryBehaviour:
		return "Comportement"
	default:
		return "Autre"
	}
}
