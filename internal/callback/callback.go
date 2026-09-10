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
	"strings"
	"time"
)

// Client appelle une verticale.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
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

// OrderPaid tells the vertical that one of its orders is paid.
//
// ⚠️ Le chemin n'a PAS le préfixe `/food`. Ce préfixe est posé par la
// passerelle, qui le retire avant de proxifier : chaque service continue de
// servir `/api/v1/...` chez lui. Un appel de service va DIRECTEMENT au
// service — passer par la passerelle publique ajouterait un saut et ferait
// dépendre un rappel interne de la santé de l'étage public.
func (c *Client) OrderPaid(ctx context.Context, orderID, paymentID string) error {
	return c.post(ctx, "/api/v1/internal/payments/order-paid", map[string]string{
		"order_id": orderID, "payment_id": paymentID,
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
