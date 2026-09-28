package obs

// LE FIL D'UNE REQUÊTE — la suivre d'un service à l'autre.
//
// ⚠️ UNE COURSE TRAVERSE QUATRE SERVICES : l'application appelle les courses,
// qui appellent le socle (le portefeuille), le suivi (l'appel des chauffeurs)
// et les cartes (l'itinéraire). Quand un passager dit « ça a mis trente
// secondes », la réponse est dans QUATRE journaux — et sans fil conducteur,
// il faut les recouper à l'horodatage, ce qui marche jusqu'au jour où deux
// passagers commandent la même seconde.
//
// ⚠️ CE N'EST PAS DE L'OPENTELEMETRY, et il faut le dire plutôt que de le
// laisser croire. Il n'y a ici ni collecteur, ni flamme, ni échantillonnage :
// un identifiant recopié d'en-tête en en-tête, et chaque service qui note ce
// qu'il a fait et en combien de temps. Cela répond à « où sont passées les
// trente secondes ? » et à « qu'est-ce qui a échoué en amont ? » — les deux
// questions d'un incident. Un vrai traçage distribué (OTLP, Tempo) reste
// possible plus tard : l'identifiant est déjà là pour servir de trace.

import (
	"context"
	"net/http"
	"time"
)

// HeaderRequestID est l'en-tête qui porte le fil. ⚠️ LE MÊME MOT PARTOUT :
// un service qui écrirait `X-Correlation-Id` romprait la chaîne en silence —
// tout continuerait de marcher, et plus rien ne se recouperait.
const HeaderRequestID = "X-Request-ID"

// Propagate recopie le fil de la requête entrante sur une requête sortante.
//
// ⚠️ À APPELER DANS CHAQUE PONT (corebridge, trackingbridge, mapsbridge).
// Sans cela, l'appel sortant naît sans passé : le service d'en face lui
// donnera un identifiant tout neuf, et les deux moitiés de la même histoire
// ne se retrouveront jamais.
func Propagate(ctx context.Context, req *http.Request) *http.Request {
	if id := Field(ctx, KeyRequest); id != "" {
		req.Header.Set(HeaderRequestID, id)
	}
	if code := Field(ctx, KeyCountry); code != "" {
		// Le pays voyage aussi : un appel interne lu dans les journaux doit
		// dire de quel pays il vient, sans qu'il faille remonter au socle.
		req.Header.Set("X-Dira-Country", code)
	}
	return req
}

// Step note UNE étape d'un appel sortant : ce qu'on a demandé, à qui, combien
// de temps cela a pris, et si cela a marché.
//
// Elle écrit une ligne de journal ET une mesure, avec le même vocabulaire :
// `target` et `operation` sont les mêmes des deux côtés, pour qu'une courbe
// suspecte se retrouve dans le texte sans traduction.
func (m *Metrics) Step(ctx context.Context, log Loggerf, target, operation string, start time.Time, err error) {
	d := time.Since(start)
	m.ObserveCall(target, operation, d, err)
	if log == nil {
		return
	}
	if err != nil {
		log.ErrorContext(ctx, "outbound call failed",
			"target", target, "operation", operation,
			"duration_ms", d.Milliseconds(), "error", err.Error())
		return
	}
	// ⚠️ AU NIVEAU `debug` QUAND TOUT VA BIEN. Un service qui journalise
	// chaque appel réussi produit dix lignes par course : le jour de
	// l'incident, l'information utile est noyée dans le bruit du jour normal.
	// La MESURE, elle, compte toujours — c'est elle qui trace la courbe.
	log.DebugContext(ctx, "outbound call",
		"target", target, "operation", operation, "duration_ms", d.Milliseconds())
}

// Loggerf est ce que `Step` attend d'un journal : de quoi écrire avec le
// contexte. `*slog.Logger` le satisfait.
type Loggerf interface {
	DebugContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}
