package obs

// LES MESURES — ce que la plateforme compte sur elle-même.
//
// ⚠️ LES MÊMES NOMS PARTOUT, et c'est tout l'intérêt d'être au socle. Une
// règle d'alerte écrite une fois — « plus de 5 % d'erreurs sur une route
// pendant cinq minutes » — vaut alors pour les courses, la livraison, le
// socle et le suivi. Six conventions différentes auraient donné six règles à
// tenir d'accord, et c'est ainsi qu'une alerte finit par ne plus rien
// surveiller.
//
// ⚠️ LE GABARIT DE ROUTE, JAMAIS LE CHEMIN. `/rides/{id}` compte ; `/rides/6ab8…`
// créerait une série temporelle par course — quelques milliers de séries en un
// après-midi, et la base de mesures tombe. C'est la faute classique, et elle
// ne se voit qu'une fois le mal fait.

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics porte le registre d'un service et ses mesures communes.
type Metrics struct {
	Registry *prometheus.Registry
	service  string

	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
	panics   prometheus.Counter
	// calls compte les appels SORTANTS vers les autres services : c'est ce
	// qui dit lequel des six est en train de ralentir les autres.
	calls    *prometheus.CounterVec
	callTime *prometheus.HistogramVec
	// sink range les pannes — voir errors.go. Facultatif : sans lui, elles
	// restent journalisées.
	sink Sink
}

// NewMetrics construit le registre d'un service.
//
// ⚠️ UN REGISTRE PROPRE, pas celui par défaut. Le registre global ramasse tout
// ce qu'une dépendance décide d'y publier, et deux bibliothèques qui déclarent
// la même mesure font paniquer le processus AU DÉMARRAGE. Ici, ce qui est
// publié est ce qu'on a écrit.
func NewMetrics(service, version string) *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		service:  service,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dira_http_requests_total",
			Help: "Requêtes HTTP servies, par route et par statut.",
		}, []string{"service", "method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "dira_http_request_duration_seconds",
			Help: "Durée d'une requête HTTP servie.",
			// ⚠️ DES BORNES CHOISIES POUR CE QU'ON SURVEILLE. Les bornes par
			// défaut de la bibliothèque s'arrêtent à 10 s ; un devis qui
			// interroge le réseau routier ou un envoi de média les dépasse, et
			// tout finirait dans « +Inf » — la mesure dirait alors seulement
			// « c'était long », sans jamais dire combien.
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"service", "method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "dira_http_requests_in_flight",
			Help:        "Requêtes en cours de traitement.",
			ConstLabels: prometheus.Labels{"service": service},
		}),
		panics: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "dira_panics_total",
			Help:        "Paniques rattrapées. ⚠️ Toute valeur non nulle est un défaut à corriger.",
			ConstLabels: prometheus.Labels{"service": service},
		}),
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dira_outbound_calls_total",
			Help: "Appels sortants vers un autre service de la plateforme.",
		}, []string{"service", "target", "operation", "outcome"}),
		callTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "dira_outbound_call_duration_seconds",
			Help:    "Durée d'un appel sortant vers un autre service.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"service", "target", "operation"}),
	}
	reg.MustRegister(m.requests, m.duration, m.inFlight, m.panics, m.calls, m.callTime)
	// Le processus lui-même : mémoire, goroutines, descripteurs, GC. C'est ce
	// qui distingue « le service est lent » de « la machine est à genoux ».
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	// ⚠️ LA VERSION EST UNE MESURE. Elle vaut toujours 1 ; ce qui compte est
	// son ÉTIQUETTE. Sans elle, un tableau de bord ne sait pas quelle version
	// tournait au moment du pic, et c'est la première question posée après un
	// déploiement.
	build := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dira_build_info",
		Help: "La version qui tourne. Vaut toujours 1 ; lire l'étiquette.",
	}, []string{"service", "version"})
	build.WithLabelValues(service, version).Set(1)
	reg.MustRegister(build)
	return m
}

// Handler sert `/metrics`.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry})
}

// Middleware compte chaque requête servie.
//
// ⚠️ IL S'INSTALLE APRÈS LE ROUTEUR, pas avant : le gabarit de route
// (`/rides/{id}`) n'est connu qu'une fois la route choisie. Posé trop tôt, il
// compterait tout sous `route=""` — une mesure qui ne dit plus où ça casse.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		m.inFlight.Inc()
		defer m.inFlight.Dec()

		next.ServeHTTP(ww, r)

		// ⚠️ LE GABARIT SE LIT DANS LE CONTEXTE DU ROUTEUR, et il n'y est
		// qu'APRÈS le routage — d'où la position de ce relevé, après l'appel
		// au suivant.
		route := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			route = rc.RoutePattern()
		}
		if route == "" {
			// Une requête qui n'a trouvé aucune route : on la compte sous un
			// nom FIXE. Reprendre le chemin demandé laisserait n'importe qui
			// créer autant de séries qu'il veut en tapant des URL au hasard.
			route = "unmatched"
		}
		status := strconv.Itoa(ww.Status())
		m.requests.WithLabelValues(m.service, r.Method, route, status).Inc()
		m.duration.WithLabelValues(m.service, r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// PanicRecorded note une panique rattrapée.
func (m *Metrics) PanicRecorded() { m.panics.Inc() }

// ObserveCall note un appel sortant vers un autre service.
//
// `target` est le service appelé (`core`, `tracking`, `maps`), `operation` ce
// qu'on lui a demandé. ⚠️ `operation` doit être un mot FIXE — « route », pas
// l'itinéraire demandé.
func (m *Metrics) ObserveCall(target, operation string, d time.Duration, err error) {
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	m.calls.WithLabelValues(m.service, target, operation, outcome).Inc()
	m.callTime.WithLabelValues(m.service, target, operation).Observe(d.Seconds())
}

// Gauge déclare une mesure MÉTIER lue à chaque interrogation.
//
// ⚠️ LUE À LA DEMANDE, PAS POUSSÉE. « Combien de courses cherchent un
// chauffeur en ce moment ? » est une question dont la réponse vit dans la
// base : la pousser à chaque changement demanderait de penser à le faire à
// vingt endroits, et le premier oubli ferait mentir le chiffre pour toujours.
// Lue au moment où on la regarde, elle ne peut pas dériver.
func (m *Metrics) Gauge(name, help string, labels prometheus.Labels, read func() float64) {
	all := prometheus.Labels{"service": m.service}
	for k, v := range labels {
		all[k] = v
	}
	m.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: name, Help: help, ConstLabels: all,
	}, read))
}
