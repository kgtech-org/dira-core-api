package support

import "time"

// --- requests ---

// CreateRequest is the body of POST /tickets.
//
// `order_id` et `ride_id` sont tous deux déclarés parce que le paquet sert
// les deux verticales ; le service n'accepte que celui de la sienne, et
// nomme l'autre dans `fields` — une application qui envoie `order_id` aux
// courses a un bug, pas une commande.
type CreateRequest struct {
	Category string `json:"category" validate:"required"`
	Priority string `json:"priority" validate:"omitempty,oneof=low normal high critical"`
	OrderID  string `json:"order_id" validate:"omitempty"`
	RideID   string `json:"ride_id" validate:"omitempty"`
	Message  string `json:"message" validate:"required,max=4000"`
	// LostItem accompagne la catégorie `lost_item`, et elle seule.
	LostItem *LostItemRequest `json:"lost_item" validate:"omitempty"`
}

// LostItemRequest describes what was forgotten.
type LostItemRequest struct {
	Item    string `json:"item" validate:"required,max=200"`
	Details string `json:"details" validate:"omitempty,max=2000"`
}

// MessageRequest is the body of POST /tickets/{id}/messages.
type MessageRequest struct {
	Body string `json:"body" validate:"required,max=4000"`
}

// LostItemAnswerRequest is the body of POST /tickets/{id}/lost-item — the
// driver's answer.
type LostItemAnswerRequest struct {
	Found *bool  `json:"found" validate:"required"`
	Note  string `json:"note" validate:"omitempty,max=2000"`
}

// UpdateRequest is the body of PATCH /admin/tickets/{id}. Nil fields are
// left unchanged.
type UpdateRequest struct {
	Status     *string `json:"status" validate:"omitempty,oneof=open in_progress waiting resolved closed"`
	Priority   *string `json:"priority" validate:"omitempty,oneof=low normal high critical"`
	AssignedTo *string `json:"assigned_to" validate:"omitempty"`
}

// ListQuery carries the GET /tickets filters (admin only).
type ListQuery struct {
	Status     string
	Category   string
	AssignedTo string
}

// --- responses ---

// MessageResponse is one thread message.
type MessageResponse struct {
	AuthorID   string    `json:"author_id"`
	AuthorRole string    `json:"author_role,omitempty"`
	Body       string    `json:"body"`
	At         time.Time `json:"at"`
}

// LostItemResponse is the lost-item block of a ticket.
type LostItemResponse struct {
	Item       string     `json:"item"`
	Details    string     `json:"details,omitempty"`
	Found      *bool      `json:"found"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
	Note       string     `json:"note,omitempty"`
}

// Response is the public representation of a ticket.
type Response struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	UserID    string `json:"user_id"`
	// UserName et CounterpartName ne sont posés que pour l'ADMINISTRATION.
	UserName   string `json:"user_name,omitempty"`
	Role       string `json:"role,omitempty"`
	Category   string `json:"category"`
	Priority   string `json:"priority"`
	Status     string `json:"status"`
	AssignedTo string `json:"assigned_to,omitempty"`
	// La référence, dans le vocabulaire de la verticale : l'un des deux.
	OrderID  string `json:"order_id,omitempty"`
	RideID   string `json:"ride_id,omitempty"`
	RefLabel string `json:"ref_label,omitempty"`
	// CounterpartID : le chauffeur ou livreur concerné (objet perdu).
	CounterpartID   string            `json:"counterpart_id,omitempty"`
	CounterpartName string            `json:"counterpart_name,omitempty"`
	LostItem        *LostItemResponse `json:"lost_item,omitempty"`
	Messages        []MessageResponse `json:"messages"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func toResponse(t *Ticket) Response {
	resp := Response{
		ID:        t.ID.Hex(),
		Reference: t.Reference,
		UserID:    t.UserID.Hex(),
		Role:      t.Role,
		Category:  t.Category,
		Priority:  t.Priority,
		Status:    t.Status,
		RefLabel:  t.RefLabel,
		Messages:  make([]MessageResponse, 0, len(t.Messages)),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
	if t.AssignedTo != nil {
		resp.AssignedTo = t.AssignedTo.Hex()
	}
	if id := t.refID(); id != nil {
		// Un ticket d'avant l'extraction n'a pas de `ref_kind` : il vient de
		// la livraison, il parle d'une commande.
		if t.RefKind == RefRide {
			resp.RideID = id.Hex()
		} else {
			resp.OrderID = id.Hex()
		}
	}
	if t.CounterpartID != nil {
		resp.CounterpartID = t.CounterpartID.Hex()
	}
	if t.LostItem != nil {
		resp.LostItem = &LostItemResponse{
			Item: t.LostItem.Item, Details: t.LostItem.Details,
			Found: t.LostItem.Found, AnsweredAt: t.LostItem.AnsweredAt, Note: t.LostItem.Note,
		}
	}
	for _, m := range t.Messages {
		resp.Messages = append(resp.Messages, MessageResponse{
			AuthorID: m.AuthorID.Hex(), AuthorRole: m.AuthorRole, Body: m.Body, At: m.At,
		})
	}
	return resp
}
