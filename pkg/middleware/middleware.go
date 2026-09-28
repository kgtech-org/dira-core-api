// Package middleware provides the global and per-route HTTP middlewares:
// request id, structured logging, panic recovery, language detection,
// Redis-backed rate limiting, JWT auth and RBAC.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/i18n"
	"github.com/kgtech-org/dira-core-api/pkg/obs"
	"github.com/kgtech-org/dira-core-api/pkg/session"
)

type requestIDKey struct{}

// RequestID reuses the inbound X-Request-ID or generates one, propagating it
// through the context and the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := usableRequestID(r.Header.Get(obs.HeaderRequestID))
		if id == "" {
			buf := make([]byte, 8)
			_, _ = rand.Read(buf)
			id = hex.EncodeToString(buf)
		}
		w.Header().Set(obs.HeaderRequestID, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		// ⚠️ QUI POSE UNE VALEUR LA PUBLIE POUR LES JOURNAUX. Sans cette
		// ligne, il faudrait ajouter « et aussi l'identifiant de requête » à
		// chaque appel de journalisation du code — des centaines, dont la
		// moitié serait oubliée. Le contexte le porte déjà ; `pkg/obs` le
		// recopie dans chaque ligne.
		ctx = obs.WithField(ctx, obs.KeyRequest, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// maxRequestIDLen borne ce qu'un client peut nous faire écrire.
const maxRequestIDLen = 64

// usableRequestID ne garde l'identifiant fourni par l'appelant que s'il est
// court et inoffensif ; sinon on tire le nôtre.
//
// ⚠️ CE QUI ARRIVE DU CLIENT REPART DANS L'EN-TÊTE ET DANS CHAQUE LIGNE DE
// JOURNAL DE SIX SERVICES. Non borné, il suffit d'une application qui envoie
// dix kilo-octets à chaque appel pour remplir la base de journaux ; et un
// identifiant qui porterait un numéro de téléphone le recopierait dans des
// journaux que personne ne songe à purger.
//
// ⚠️ ON REFUSE, ON NE TRONQUE PAS. Tronqué, un identifiant de trente
// caractères en devient un de soixante-quatre — et deux fils différents
// finiraient par se confondre au moment précis où on les cherche.
// L'application n'est pas laissée sans rien pour autant : la réponse porte
// TOUJOURS l'identifiant retenu, le nôtre le cas échéant.
func usableRequestID(id string) string {
	if id == "" || len(id) > maxRequestIDLen {
		return ""
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
		default:
			return ""
		}
	}
	return id
}

// RequestIDFromContext returns the request id, or "" when absent.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// Logger emits one structured JSON log line per request.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			logger.Info("http_request",
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// Recoverer converts panics into a standard 500 error response.
// ⚠️ ELLE PREND LES MESURES EN PARAMÈTRE, ET C'EST UN CHANGEMENT VOULU. Une
// panique écrite dans les journaux d'un conteneur a trois défauts : elle
// disparaît à la rotation, personne ne la voit passer, et la même panique
// répétée mille fois ressemble à mille problèmes. Comptée et capturée, elle
// est groupée, datée, et elle DÉCLENCHE une alerte — `dira_panics_total` doit
// rester à zéro, et toute valeur non nulle est un défaut à corriger.
//
// `m` peut être nul : le service se comporte alors comme avant.
func Recoverer(m *obs.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// ⚠️ LA PILE D'EXÉCUTION EST PRISE ICI, PAS PLUS LOIN. Une
				// pile relevée après le retour de `recover` ne montre plus
				// l'endroit où ça a cassé : elle montre le mécanisme de
				// rattrapage.
				stack := string(debug.Stack())
				ctx := r.Context()
				slog.ErrorContext(ctx, "panic recovered",
					"panic", fmt.Sprint(rec), "stack", stack)
				if m != nil {
					m.PanicRecorded()
					m.Capture(ctx, obs.Fault{
						Kind: "panic", Message: fmt.Sprint(rec), Stack: stack,
						Method: r.Method, Status: http.StatusInternalServerError,
					})
				}
				httpx.Error(w, r, apperr.Internal(fmt.Errorf("panic: %v", rec)))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Language detects the request language (Accept-Language) into the context.
func Language(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lang := i18n.DetectLanguage(r.Header.Get("Accept-Language"))
		next.ServeHTTP(w, r.WithContext(i18n.WithLang(r.Context(), lang)))
	})
}

// RateLimit limits requests per minute per user (when authenticated) or per
// IP, using a fixed window counter in Redis. Fails open if Redis is down.
func RateLimit(rdb *redis.Client, perMinute int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ⚠️ LA SURFACE DE SERVICE N'EST PAS LIMITÉE. Ces routes sont
			// appelées par les verticales, depuis UNE adresse chacune, à un
			// rythme proportionnel au trafic de toute la plateforme : nommer
			// un chauffeur, débiter un portefeuille, notifier — pour chaque
			// commande. Les compter contre le quota d'une adresse revenait à
			// plafonner la livraison à 120 gestes par minute, et la console
			// des courses, qui rafraîchit ses appels toutes les quatre
			// secondes, épuisait ce quota à elle seule : les noms
			// disparaissaient des écrans en 429. Ces routes sont gardées par
			// le secret partagé, pas par un compteur.
			if strings.HasPrefix(r.URL.Path, "/api/v1/internal/") {
				next.ServeHTTP(w, r)
				return
			}
			key := clientKey(r)
			window := time.Now().UTC().Format("200601021504") // minute bucket
			redisKey := "ratelimit:" + key + ":" + window

			count, err := rdb.Incr(r.Context(), redisKey).Result()
			if err == nil {
				if count == 1 {
					rdb.Expire(r.Context(), redisKey, time.Minute)
				}
				if count > int64(perMinute) {
					httpx.Error(w, r, apperr.TooManyRequests("too many requests"))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientKey(r *http.Request) string {
	if userID, ok := auth.UserFromContext(r.Context()); ok {
		return "user:" + userID
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		ip = strings.TrimSpace(strings.Split(ip, ",")[0])
	} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	} else {
		ip = r.RemoteAddr
	}
	return "ip:" + ip
}

// SessionGuard dit si un appareil détient encore la session de ce compte.
// Implémenté par `pkg/session.Registry` ; déclaré ici, côté consommateur, pour
// que ce paquet n'ait pas besoin de connaître Redis.
//
// `holder` nomme l'appareil qui a pris la place — c'est ce qui permet au refus
// de DIRE à la personne où elle s'est connectée, au lieu de la laisser croire
// que l'application est cassée.
type SessionGuard interface {
	Accepts(ctx context.Context, userID, deviceID string) (bool, session.Device)
}

// ErrSessionSuperseded : le jeton est valide, signé, non expiré — mais
// l'appareil qui le porte n'est plus celui du compte. Un autre téléphone s'est
// connecté depuis.
//
// ⚠️ UN CODE À LUI, ET C'EST TOUT L'INTÉRÊT. Avec `invalid_token`, l'application
// affiche « session expirée, reconnectez-vous » : la personne se reconnecte, ça
// marche, et elle chasse à son tour l'autre téléphone — une bascule sans fin
// entre deux appareils, dont personne ne comprend la cause. Nommé, le refus se
// traduit par une phrase qui dit ce qui s'est passé.
//
// ⚠️ 401 ET NON 403, délibérément : les applications ont déjà un intercepteur
// sur 401 qui tente un rafraîchissement puis déconnecte. Ce chemin-là mène au
// bon endroit — le rafraîchissement répond le MÊME code — et une application
// pas encore mise à jour se comporte donc correctement, juste sans la bonne
// phrase. Un 403 l'aurait fait échouer silencieusement sur l'écran courant.
var ErrSessionSuperseded = apperr.Unauthorized("session_superseded",
	"this device is no longer the one signed in to this account")

// authOptions porte les réglages facultatifs de `Auth`.
type authOptions struct {
	sessions SessionGuard
	tolerate bool
}

// AuthOption règle `Auth`. Variadique parce que l'immense majorité des
// montages n'en veut aucune : changer la signature aurait demandé un `nil` de
// plus dans les trois services et dans des dizaines de tests, sans rien
// apprendre à personne.
type AuthOption func(*authOptions)

// WithSessions fait refuser les jetons d'un appareil chassé.
//
// ⚠️ NE CONCERNE QUE LES CHAUFFEURS ET LES LIVREURS — les seuls comptes dont
// le jeton porte un appareil. Un client a le droit d'être sur sa tablette ET
// son téléphone, un membre du staff a trois onglets de console ouverts : leur
// jeton ne nomme aucun appareil, et ce garde ne les voit pas.
func WithSessions(g SessionGuard) AuthOption {
	return func(o *authOptions) { o.sessions = g }
}

// TolerateSupersededSession laisse passer un appareil chassé, en posant le
// drapeau dans le contexte.
//
// ⚠️ ELLE EXISTE POUR LE RATTRAPAGE HORS LIGNE, ET POUR RIEN D'AUTRE. Un
// chauffeur chassé peut avoir dix courses faites sans réseau, jamais remontées.
// Refuser sa resynchronisation ferait PERDRE ces courses — donc l'argent du
// chauffeur — pour appliquer une règle dont l'objet est d'éviter deux flux de
// positions. Raconter le passé ne crée aucun second flux : c'est la seule chose
// qu'un appareil chassé a encore le droit de faire.
//
// À poser UNIQUEMENT sur les routes de resynchronisation. L'exemption est
// choisie route par route, jamais par défaut : l'oubli inverse — tout tolérer
// — ne se verrait nulle part.
func TolerateSupersededSession() AuthOption {
	return func(o *authOptions) { o.tolerate = true }
}

type supersededKey struct{}

// SupersededSession dit si la requête arrive d'un appareil chassé, sur une
// route qui le tolère. Faux partout ailleurs — ces requêtes-là ont été
// refusées.
func SupersededSession(ctx context.Context) bool {
	v, _ := ctx.Value(supersededKey{}).(bool)
	return v
}

// Auth validates the Bearer access token and injects the user into context.
func Auth(m *auth.Manager, opts ...AuthOption) func(http.Handler) http.Handler {
	var o authOptions
	for _, opt := range opts {
		opt(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || token == "" {
				httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
				return
			}
			claims, err := m.Verify(token)
			if err != nil || claims.Type != auth.TokenTypeAccess {
				httpx.Error(w, r, auth.ErrInvalidToken)
				return
			}
			ctx := auth.WithClaims(r.Context(), claims)
			// Le COMPTE, publié pour les journaux : « qui » est la deuxième
			// question d'un incident, juste après « quoi ».
			ctx = obs.WithField(ctx, obs.KeyUser, claims.UserID)
			// UN SEUL APPAREIL PAR CHAUFFEUR. Le jeton ne nomme un appareil
			// que pour eux ; pour tous les autres comptes, il n'y a rien à
			// comparer et rien ne change.
			if o.sessions != nil && claims.Device != "" {
				live, holder := o.sessions.Accepts(ctx, claims.UserID, claims.Device)
				if !live {
					if !o.tolerate {
						httpx.Error(w, r, supersededBy(holder))
						return
					}
					ctx = context.WithValue(ctx, supersededKey{}, true)
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// supersededBy habille le refus du NOM de l'appareil qui a pris la place.
//
// ⚠️ `reason` EST LA SEULE CLÉ QUI SORT. L'enveloppe d'erreur ne rend que
// `fields` et `reason` ; le reste de `Meta` ne sert qu'à composer la phrase
// traduite. C'est donc `reason` qui porte le nom de l'appareil — et une spec
// qui promettrait un autre champ promettrait du vide.
//
// Sans nom connu (l'application ne l'a pas envoyé), `reason` reste absent :
// mieux vaut « vous vous êtes connecté sur un autre appareil » que « vous vous
// êtes connecté sur "" ».
func supersededBy(holder session.Device) error {
	if holder.Name == "" {
		return ErrSessionSuperseded
	}
	return ErrSessionSuperseded.WithMeta(map[string]any{
		"reason": holder.Name,
		"device": holder.Name,
	})
}

// RequireRole rejects with 403 when the authenticated role is not allowed.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := auth.RoleFromContext(r.Context())
			if !ok {
				httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
				return
			}
			if _, ok := allowed[role]; !ok {
				httpx.Error(w, r, apperr.Forbidden("forbidden", "role not allowed"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireScope refuse un administrateur dont la portée ne couvre pas cette
// verticale.
//
// ⚠️ C'est ce qui fait la différence entre des habilitations et une
// décoration. Un « périmètre » affiché sur une fiche de staff mais qu'aucune
// route ne vérifie donne à l'exploitation la certitude d'avoir restreint
// quelqu'un qui ne l'est pas — pire que de n'avoir rien restreint, parce que
// personne ne surveille plus.
//
// À poser DANS chaque verticale, sur ses routes d'administration :
// `middleware.RequireScope(auth.ScopeFood)`. Le socle ne peut pas le faire
// pour elles — il ne voit pas leurs routes.
//
// ⚠️ NE CONCERNE QUE LES ADMINISTRATEURS. Un client, un livreur ou un marchand
// n'a pas de portée et ne doit pas en avoir : c'est son RÔLE qui borne ce
// qu'il atteint. Sans cette distinction, poser ce garde à l'entrée d'un
// service — ce que fait la livraison, pour ne pas dépendre d'une liste de
// vingt et une routes — aurait fermé la porte à tous ses clients.
//
// Un administrateur, lui, doit porter la portée : une liste vide n'accorde
// rien. Un compte `admin` sans fiche de staff n'administre donc rien, et
// c'est voulu — le provisionnement crée la fiche avec le compte.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := auth.RoleFromContext(r.Context())
			if !ok {
				httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
				return
			}
			if role != auth.RoleAdmin {
				next.ServeHTTP(w, r) // la portée est une notion de STAFF
				return
			}
			if !auth.AllowsFromContext(r.Context(), scope) {
				httpx.Error(w, r, apperr.Forbidden("out_of_scope",
					"your staff scope does not cover this part of the platform"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
