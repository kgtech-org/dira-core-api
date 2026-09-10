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
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/i18n"
)

type requestIDKey struct{}

// RequestID reuses the inbound X-Request-ID or generates one, propagating it
// through the context and the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			buf := make([]byte, 8)
			_, _ = rand.Read(buf)
			id = hex.EncodeToString(buf)
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"request_id", RequestIDFromContext(r.Context()),
					"panic", fmt.Sprint(rec),
				)
				httpx.Error(w, r, apperr.Internal(fmt.Errorf("panic: %v", rec)))
			}
		}()
		next.ServeHTTP(w, r)
	})
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

// Auth validates the Bearer access token and injects the user into context.
func Auth(m *auth.Manager) func(http.Handler) http.Handler {
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
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
// Une portée absente autorise tout : voir `auth.Claims.Scopes`.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := auth.RoleFromContext(r.Context()); !ok {
				httpx.Error(w, r, apperr.Unauthorized("missing_token", "missing bearer token"))
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
