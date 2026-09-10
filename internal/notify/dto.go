package notify

import (
	"sort"
	"time"
)

// RegisterDeviceRequest enregistre un téléphone joignable.
type RegisterDeviceRequest struct {
	Token    string `json:"token" validate:"required,max=4096"`
	Platform string `json:"platform" validate:"omitempty,oneof=android ios web"`
	// Locale est la langue de l'APPAREIL. Acceptée sous n'importe quelle
	// forme (`fr`, `fr-FR`, `FR_fr`) : le serveur la normalise plutôt que
	// d'imposer une écriture qu'aucune plateforme mobile ne rend pareil.
	Locale string `json:"locale" validate:"omitempty,max=16"`
}

// SaveTemplateRequest écrit un gabarit dans toutes ses langues.
type SaveTemplateRequest struct {
	// Locales : code de langue → texte. `fr` est OBLIGATOIRE, c'est le repli.
	Locales     map[string]Text `json:"locales" validate:"required"`
	Description string          `json:"description" validate:"omitempty,max=300"`
	Enabled     bool            `json:"enabled"`
}

// PreviewRequest rend un gabarit sans l'envoyer.
type PreviewRequest struct {
	Locale string            `json:"locale" validate:"omitempty,max=16"`
	Vars   map[string]string `json:"vars"`
}

// TemplateResponse est un gabarit tel que l'écran d'administration le lit.
type TemplateResponse struct {
	Key         string          `json:"key"`
	Locales     map[string]Text `json:"locales"`
	Description string          `json:"description,omitempty"`
	Enabled     bool            `json:"enabled"`
	// Variables : ce que le texte ATTEND, extrait du gabarit lui-même. Sans
	// cette liste, un exploitant découvrirait un trou dans sa phrase après
	// l'envoi.
	Variables []string `json:"variables"`
	// Available : ce que la PLATEFORME sait remplir pour cette clé. C'est
	// dans cette liste, et nulle part ailleurs, qu'un exploitant pioche.
	Available []string `json:"available_variables"`
	// Customized dit si ce gabarit a été modifié, ou s'il est encore celui
	// livré avec la version. La distinction compte : un texte jamais touché
	// suivra les corrections des versions suivantes, un texte modifié non.
	Customized bool `json:"customized"`
}

// PreviewResponse est le rendu final, tel qu'il arriverait sur un téléphone.
type PreviewResponse struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Missing : les variables sans valeur, remplacées par du vide. Elles se
	// voient ICI plutôt que chez le destinataire.
	Missing []string `json:"missing,omitempty"`
}

func toTemplateResponse(t Template, customized bool) TemplateResponse {
	parts := make([]string, 0, len(t.Locales)*2)
	for _, txt := range t.Locales {
		parts = append(parts, txt.Title, txt.Body)
	}
	return TemplateResponse{
		Key:         t.Key,
		Available:   Provided(t.Key),
		Locales:     t.Locales,
		Description: t.Description,
		Enabled:     t.Enabled,
		Variables:   Variables(parts...),
		Customized:  customized,
	}
}

func sortByKey(items []TemplateResponse) {
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
}

func mergeSorted(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range append(append([]string{}, a...), b...) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// InboxResponse is one notification as the notification centre shows it.
type InboxResponse struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Category string `json:"category"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	// Data ouvre la BONNE commande depuis la liste, exactement comme depuis
	// la bannière.
	Data      map[string]string `json:"data,omitempty"`
	Read      bool              `json:"read"`
	CreatedAt time.Time         `json:"created_at"`
}

func toInboxResponse(n Inbox) InboxResponse {
	return InboxResponse{
		ID:        n.ID.Hex(),
		Key:       n.Key,
		Category:  n.Category,
		Title:     n.Title,
		Body:      n.Body,
		Data:      n.Data,
		Read:      n.ReadAt != nil,
		CreatedAt: n.CreatedAt,
	}
}
