// Package callback rappelle une VERTICALE depuis le socle.
//
// C'est la direction inverse de `serviceapi` : le socle apprend quelque chose
// que seule la verticale sait interpréter. Un paiement mobile abouti pour une
// « commande » — le socle ne sait pas ce qu'est une commande, il sait
// seulement qu'un `ref_id` vient d'être payé.
//
// ⚠️ Volontairement MINCE, et volontairement UNIDIRECTIONNEL. Le socle ne
// demande rien à une verticale : il lui dit un fait, une fois. Une dépendance
// où chacun interroge l'autre finirait par un interblocage au démarrage, et
// par un socle qui ne peut plus être déployé seul.
package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client appelle une verticale.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// Registry résout la verticale à prévenir pour un OBJET payé.
//
// ⚠️ Le socle ne sait pas ce qu'est une commande ni une course : il sait qu'un
// paiement portait un `purpose`. C'est ce mot, et lui seul, qui décide de qui
// prévenir — envoyer la confirmation d'une course à la livraison la ferait
// refuser, et le passager attendrait une voiture que personne n'a commandée.
type Registry struct{ byPurpose map[string]*Client }

// NewRegistry branche les verticales. Une entrée sans client est ignorée : en
// développement, un paiement reste confirmé au socle sans que personne ne soit
// prévenu — et le journal le dit.
func NewRegistry(entries map[string]*Client) *Registry {
	r := &Registry{byPurpose: map[string]*Client{}}
	for purpose, c := range entries {
		if c != nil && c.Enabled() {
			r.byPurpose[purpose] = c
		}
	}
	return r
}

// For rend la verticale à prévenir, ou nil.
func (r *Registry) For(purpose string) *Client {
	if r == nil {
		return nil
	}
	return r.byPurpose[purpose]
}

// Purposes liste ce qui est branché, trié. Un socle qui ne dit pas ce qu'il
// sait confirmer laisse découvrir en production qu'il ne confirme rien.
func (r *Registry) Purposes() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.byPurpose))
	for p := range r.byPurpose {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// New builds the client. baseURL vide = client INERTE : chaque appel échoue
// avec une erreur nommée plutôt que d'atteindre une adresse vide.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		// Plus long que le sens inverse : ce rappel n'est pas dans le chemin
		// d'un écran. C'est un prestataire de paiement qui attend, et il
		// réessaiera.
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled dit si la verticale est configurée.
func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" }

// ErrNotConfigured dit qu'aucune verticale n'est branchée.
//
// ⚠️ Rendue comme une ERREUR, pas avalée en silence : le prestataire de
// paiement doit recevoir un échec et réessayer. Répondre « reçu » sans avoir
// prévenu la livraison laisserait une commande payée et jamais confirmée —
// exactement ce que ce paquet existe pour empêcher.
var ErrNotConfigured = errors.New("callback: vertical not configured")

// RefPaid tells the vertical that one of its objects is paid.
//
// ⚠️ Le chemin n'a PAS le préfixe `/food`. Ce préfixe est posé par la
// passerelle, qui le retire avant de proxifier : chaque service continue de
// servir `/api/v1/...` chez lui. Un appel de service va DIRECTEMENT au
// service — passer par la passerelle publique ajouterait un saut et ferait
// dépendre un rappel interne de la santé de l'étage public.
func (c *Client) RefPaid(ctx context.Context, refID, paymentID string) error {
	return c.post(ctx, "/api/v1/internal/payments/ref-paid", map[string]string{
		"ref_id": refID, "payment_id": paymentID,
	})
}

func (c *Client) post(ctx context.Context, path string, in any) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("callback: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("callback: %s: status %d", path, resp.StatusCode)
	}
	return nil
}
