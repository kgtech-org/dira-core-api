package chat

import "time"

// SendMessageRequest posts one line to the conversation.
type SendMessageRequest struct {
	Body string `json:"body" validate:"required,max=1000"`
}

// SendAsRequest posts a message on behalf of one side (back-office emulators).
type SendAsRequest struct {
	As   string `json:"as" validate:"required,oneof=client driver"`
	Body string `json:"body" validate:"required,max=1000"`
}

// MessageResponse is one message as both sides read it.
//
// L'EXPÉDITEUR n'est identifié que par son RÔLE, jamais par son identifiant
// d'utilisateur ni son téléphone : toute la raison d'être de cette
// conversation est que les deux puissent se parler sans échanger leurs
// coordonnées.
type MessageResponse struct {
	ID   string `json:"id"`
	From string `json:"from"` // "client" | "driver"
	Body string `json:"body"`
	// ReadAt dit que l'AUTRE l'a lu. Absent = pas encore.
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ConversationResponse is a page of the conversation.
type ConversationResponse struct {
	Items []MessageResponse `json:"items"`
	// Unread est ce que l'APPELANT n'a pas lu. Zéro pour un administrateur :
	// il n'est destinataire de rien.
	Unread int `json:"unread"`
	// NextCursor : l'identifiant du dernier message servi. Le renvoyer demande
	// la suite — c'est aussi le rattrapage après une coupure.
	NextCursor string `json:"next_cursor,omitempty"`
}

func toResponse(m Message) MessageResponse {
	return MessageResponse{
		ID:        m.ID.Hex(),
		From:      m.SenderRole,
		Body:      m.Body,
		ReadAt:    m.ReadAt,
		CreatedAt: m.CreatedAt,
	}
}

func toResponses(items []Message) []MessageResponse {
	out := make([]MessageResponse, 0, len(items))
	for _, m := range items {
		out = append(out, toResponse(m))
	}
	return out
}
