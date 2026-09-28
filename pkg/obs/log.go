// Package obs est ce que la plateforme DIT D'ELLE-MÊME : ses journaux, ses
// mesures, ses pannes.
//
// ⚠️ IL EST AU SOCLE, PARTAGÉ PAR LES SIX SERVICES, et ce n'est pas une
// commodité. Une plateforme qu'on supervise service par service, chacun avec
// ses mots et ses unités, ne se supervise pas : on ne peut ni comparer deux
// latences, ni suivre une requête d'un bout à l'autre, ni écrire une seule
// règle d'alerte qui vaille partout. Un incident se lit alors six fois, dans
// six dialectes.
//
// Trois choses, et elles se tiennent :
//
//   - LES JOURNAUX portent TOUJOURS de quoi les recouper — le service, la
//     requête, le pays, le compte. C'est ce qui transforme six flux séparés en
//     un seul récit.
//   - LES MESURES sont les mêmes partout (débit, erreurs, latence), avec les
//     mêmes noms : une règle d'alerte écrite une fois vaut pour tous.
//   - LES PANNES sont CAPTURÉES, pas seulement journalisées : une pile
//     d'exécution perdue dans un flux de texte n'a jamais réparé personne.
package obs

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Les clés de contexte que les journaux savent lire. Elles sont DÉCLARÉES ICI
// et non devinées : un champ que chaque service nommerait à sa façon
// (`user`, `user_id`, `uid`) ne se recoupe pas.
type ctxKey string

const (
	// KeyRequest est l'identifiant d'une requête, porté d'un service à
	// l'autre — voir `middleware.RequestID` et `obs.Propagate`.
	KeyRequest ctxKey = "request_id"
	// KeyCountry est le pays de la requête.
	KeyCountry ctxKey = "country"
	// KeyUser est le compte derrière la requête.
	KeyUser ctxKey = "user_id"
	// KeyRoute est le GABARIT de route (`/rides/{id}`), jamais le chemin
	// réel : `/rides/6ab8…` créerait une étiquette par course.
	KeyRoute ctxKey = "route"
)

// WithField pose une valeur que les journaux reprendront d'eux-mêmes.
func WithField(ctx context.Context, key ctxKey, value string) context.Context {
	if value == "" {
		return ctx
	}
	return context.WithValue(ctx, key, value)
}

// Field lit une valeur posée par WithField.
func Field(ctx context.Context, key ctxKey) string {
	if v, ok := ctx.Value(key).(string); ok {
		return v
	}
	return ""
}

// contextHandler recopie les champs du contexte dans CHAQUE ligne de journal.
//
// ⚠️ C'EST LE CŒUR, ET C'EST CE QUI ÉVITE DE TOUCHER MILLE APPELS. Le code de
// la plateforme écrit déjà `slog.InfoContext(ctx, …)` partout — des centaines
// de fois. Demander à chacun d'ajouter « et aussi l'identifiant de requête, et
// le pays » aurait été mille modifications, dont neuf cents oubliées. Le
// contexte les porte déjà : on les recopie ici, une fois.
type contextHandler struct {
	slog.Handler
	keys []ctxKey
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, k := range h.keys {
		if v := Field(ctx, k); v != "" {
			r.AddAttrs(slog.String(string(k), v))
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs), keys: h.keys}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name), keys: h.keys}
}

// Logger construit le journal d'un service.
//
// `service` le nomme (`core`, `vtc`, `food`, `tracking`…) et `version`
// l'horodate. ⚠️ LES DEUX SONT SUR CHAQUE LIGNE : sans le service, les flux
// agrégés se mélangent ; sans la version, on ne sait pas si l'erreur qu'on lit
// vient du code qu'on regarde ou de celui d'avant-hier — et c'est la première
// question qu'on se pose devant une panne après un déploiement.
//
// Le niveau se règle par `LOG_LEVEL` (`debug`, `info`, `warn`, `error`).
// ⚠️ RÉGLABLE SANS RECOMPILER, parce qu'on a besoin de `debug` au moment
// précis où l'on ne peut pas se permettre de livrer une nouvelle image.
func Logger(service, version string) *slog.Logger {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levelFromEnv()})
	base := contextHandler{
		Handler: h,
		keys:    []ctxKey{KeyRequest, KeyCountry, KeyUser, KeyRoute},
	}
	return slog.New(base).With("service", service, "version", version)
}

func levelFromEnv() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
