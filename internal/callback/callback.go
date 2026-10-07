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

	"github.com/kgtech-org/dira-core-api/pkg/obs"
)

// Client appelle une verticale.
type Client struct {
	// target nomme la verticale (`food`, `vtc`) — l'étiquette des mesures, et
	// le mot qui apparaît dans un journal quand une annonce échoue. Sans lui,
	// « la verticale a refusé » ne dirait pas laquelle.
	target  string
	baseURL string
	token   string
	http    *http.Client
}

// Target nomme la verticale appelée.
func (c *Client) Target() string {
	if c == nil {
		return ""
	}
	return c.target
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

// All liste les verticales branchées, UNE FOIS CHACUNE.
//
// ⚠️ DÉDOUBLONNÉE PAR ADRESSE, parce que la table est indexée par OBJET payé
// et qu'une même verticale y apparaît plusieurs fois — les courses y sont
// sous `ride` et sous `ride_subscription`. Un fait qui concerne la verticale
// entière, et non un objet, doit lui parvenir une fois : annoncer deux fois le
// même compte effacé ferait purger deux fois, et compter deux fois dans les
// mesures.
//
// Triée sur le nom, pour qu'un journal se relise d'une exécution à l'autre.
func (r *Registry) All() []*Client {
	if r == nil {
		return nil
	}
	seen := map[string]*Client{}
	for _, c := range r.byPurpose {
		if c == nil || !c.Enabled() {
			continue
		}
		if _, dup := seen[c.baseURL]; !dup {
			seen[c.baseURL] = c
		}
	}
	out := make([]*Client, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].target != out[j].target {
			return out[i].target < out[j].target
		}
		return out[i].baseURL < out[j].baseURL
	})
	return out
}

// New builds the client. baseURL vide = client INERTE : chaque appel échoue
// avec une erreur nommée plutôt que d'atteindre une adresse vide.
//
// `target` nomme la verticale rappelée (`food`, `vtc`) : c'est l'étiquette des
// mesures, et sans elle les deux verticales se confondraient dans une seule
// courbe — celle qui ne dit rien.
func New(target, baseURL, token string) *Client {
	return &Client{
		target:  target,
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		// Plus long que le sens inverse : ce rappel n'est pas dans le chemin
		// d'un écran. C'est un prestataire de paiement qui attend, et il
		// réessaiera.
		http: obs.HTTPClient(target, 10*time.Second),
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
// ⚠️ LE `purpose` PART AVEC. Une verticale peut se faire confirmer PLUSIEURS
// sortes d'objets — une course et un abonnement de courses arrivent par la
// même porte —, et `ref_id` seul ne dit pas lequel. Sans ce mot, il faudrait
// chercher l'identifiant dans chaque collection jusqu'à en trouver une qui
// réponde : un abonnement payé finirait rejeté parce qu'aucune course ne
// porte son identifiant.
//
// Le champ est ADDITIF : une verticale qui l'ignore se comporte comme avant.
func (c *Client) RefPaid(ctx context.Context, purpose, refID, paymentID string) error {
	return c.post(ctx, "/api/v1/internal/payments/ref-paid", map[string]string{
		"purpose": purpose, "ref_id": refID, "payment_id": paymentID,
	})
}

// AccountErased dit à une verticale qu'un compte vient d'être EFFACÉ, pour
// qu'elle purge ce qu'elle seule détient de cette personne : les messages
// qu'elle a écrits dans une conversation, ses fils de support, les pièces
// qu'elle a déposées.
//
// ⚠️ UN FAIT, PAS UNE PERMISSION. Le socle n'a pas demandé « puis-je
// effacer ? » : l'identité est DÉJÀ partie de ses bases quand cet appel sort.
// Un socle qui attendrait l'accord de deux verticales avant d'accepter une
// suppression ne pourrait plus être déployé seul — et une verticale
// indisponible rendrait le droit à l'effacement indisponible avec elle.
//
// ⚠️ LA VERTICALE N'A RIEN À ANONYMISER DE SON CÔTÉ : elle ne stocke ni nom ni
// téléphone, elle les demande au socle au moment d'afficher
// (`UserNames`/`ContactOf`). Ses courses et ses commandes sont donc déjà
// anonymes. Ce qu'on lui demande ici, c'est de jeter le TEXTE écrit par la
// personne, que le socle ne connaît pas.
// ⚠️ LE TÉLÉPHONE PART AVEC, et c'est le seul moment où il le fait. Une
// verticale garde des traces classées par NUMÉRO et non par compte — la
// conversation du robot WhatsApp porte le numéro de la personne et tout ce
// qu'elle a écrit pour commander. Sans lui, elles resteraient là pour toujours
// et rien ne dirait comment les retrouver : l'identifiant de compte n'y
// apparaît pas.
func (c *Client) AccountErased(ctx context.Context, userID, phone string) error {
	return c.post(ctx, "/api/v1/internal/accounts/erased", map[string]string{
		"user_id": userID, "phone": phone,
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
