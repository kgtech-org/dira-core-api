package obs

// LES APPELS ENTRE NOS SERVICES — mesurés sans qu'un pont ait à y penser.
//
// ⚠️ UN PONT QUI DOIT PENSER À MESURER NE MESURE PAS. `Propagate` et `Step`
// existaient avec leurs tests, et n'étaient appelés NULLE PART : une vingtaine
// d'appels sortants, autant d'occasions d'oublier, et personne pour s'en
// apercevoir puisque tout continue de fonctionner sans eux. Le transport
// ci-dessous s'installe UNE fois, là où le pont construit son client ; une
// méthode ajoutée demain est mesurée sans une ligne de plus.
//
// ⚠️ ET C'EST LA MOITIÉ QUI MANQUAIT. Un service sait déjà dire « j'ai mis
// trente secondes » ; sans cette moitié, il ne sait pas dire « dont vingt-huit
// à attendre le socle ». La question posée pendant un incident n'est jamais
// « est-ce lent ? », c'est « lent OÙ ? ».

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// observation est ce que le service a déclaré une fois pour toutes au démarrage.
type observation struct {
	metrics *Metrics
	log     Loggerf
}

var current atomic.Pointer[observation]

// SetDefault déclare la couche d'observation du service, une fois, depuis
// `main` — comme `httpx.SetFaultSink`.
func SetDefault(m *Metrics, log Loggerf) {
	current.Store(&observation{metrics: m, log: log})
}

// HTTPClient rend le client HTTP d'un pont vers `target` : le service appelé
// (`core`, `tracking`, `maps`, `analytics`), en UN mot fixe.
//
// ⚠️ LA COUCHE EST LUE À L'APPEL, PAS À LA CONSTRUCTION. Les ponts naissent
// dans `main` dans un ordre que personne ne garde en tête ; un transport qui
// capturerait la couche au moment où le pont se construit mesurerait ou non
// selon cet ordre — et le jour où quelqu'un déplace deux lignes, une courbe
// disparaîtrait sans message d'erreur.
func HTTPClient(target string, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: WrapTransport(target, nil)}
}

// WrapTransport habille un transport existant, pour un pont qui en configure
// un (TLS, mandataire, limite de connexions).
func WrapTransport(target string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{target: target, base: base}
}

type transport struct {
	target string
	base   http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	// ⚠️ UN TRANSPORT NE TOUCHE PAS À LA REQUÊTE QU'ON LUI CONFIE. La
	// bibliothèque standard peut la rejouer (redirection, reprise), et une
	// en-tête posée sur l'original resterait collée au rejeu suivant.
	out := req.Clone(ctx)
	Propagate(ctx, out)

	start := time.Now()
	resp, err := t.base.RoundTrip(out)

	// Sans couche déclarée, le fil voyage quand même : la propagation ne coûte
	// rien et un outil de ligne de commande ne doit pas avoir à s'inscrire.
	if o := current.Load(); o != nil && o.metrics != nil {
		// ⚠️ UN `4xx` N'EST PAS UNE PANNE. « Solde insuffisant » est une
		// RÉPONSE : la compter comme échec ferait clignoter l'alerte à chaque
		// client fauché, et une alerte qui clignote tous les jours ne se
		// regarde plus. Seul le `5xx` accuse le service d'en face — la même
		// règle que pour les défauts (`httpx`).
		outcome := err
		if outcome == nil && resp != nil && resp.StatusCode >= 500 {
			outcome = fmt.Errorf("%s a répondu %d", t.target, resp.StatusCode)
		}
		o.metrics.Step(ctx, o.log, t.target, operationOf(req), start, outcome)
	}
	return resp, err
}

// operationOf nomme ce qu'on est allé demander : la méthode et le chemin, les
// identifiants remplacés.
//
// ⚠️ UNE ÉTIQUETTE PAR COURSE TUE LA BASE DE MESURES. `/rides/6ab8…/complete`
// produirait une série chronologique par course — quelques dizaines de
// milliers en un mois, gardées treize mois. C'est le gabarit qui a du sens :
// `/rides/-/complete` répond à « terminer une course est-il lent ? », la seule
// question qu'on pose vraiment.
func operationOf(req *http.Request) string {
	if req.URL == nil {
		return req.Method
	}
	return req.Method + " " + scrubPath(req.URL.Path)
}

func scrubPath(p string) string {
	if p == "" {
		return "/"
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if looksLikeID(s) {
			parts[i] = "-"
		}
	}
	return strings.Join(parts, "/")
}

// looksLikeID reconnaît un identifiant sans connaître les routes des autres.
//
// ⚠️ `v1` DOIT SURVIVRE. La règle naïve « ce segment contient un chiffre »
// écrasait `/api/v1/` en `/api/-/` sur la plateforme entière : plus aucun
// chemin lisible, et le même remède appliqué au mal qu'il devait soigner.
func looksLikeID(s string) bool {
	digit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digit = true
			break
		}
	}
	if !digit {
		return false
	}
	// Long et mêlé de chiffres : un identifiant Mongo (24), un UUID (36).
	if len(s) >= 8 {
		return true
	}
	// Court : un identifiant seulement s'il n'est fait QUE de chiffres.
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
