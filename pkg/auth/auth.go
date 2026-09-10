// Package auth issues and verifies JWT access/refresh tokens and exposes the
// authenticated user through the request context.
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Roles (RBAC).
const (
	RoleClient   = "client"
	RoleDriver   = "driver"
	RoleMerchant = "merchant"
	RoleAdmin    = "admin"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

var ErrInvalidToken = apperr.Unauthorized("invalid_token", "invalid or expired token")

type Claims struct {
	UserID string
	Role   string
	Type   string // access | refresh
	// Scopes borne un administrateur à certaines VERTICALES : « food »,
	// « vtc », « core ».
	//
	// ⚠️ VIDE = TOUTES. Ce n'est pas une négligence, c'est ce qui rend le
	// changement rétrocompatible : tous les jetons déjà émis, et tout compte
	// d'administration sans restriction déclarée, gardent l'accès qu'ils
	// avaient. L'inverse — vide = aucune — aurait coupé l'accès de chaque
	// administrateur connecté à l'instant du déploiement, y compris celui qui
	// aurait dû corriger la situation.
	//
	// La portée voyage DANS le jeton parce que chaque verticale le vérifie
	// localement : la faire lire au socle à chaque requête referait de lui le
	// point de panne unique que la vérification locale existe pour éviter.
	Scopes []string
}

// Portées connues. Une portée est une VERTICALE, pas une permission fine :
// « ce membre du staff s'occupe des courses » se tient ; « ce membre du staff
// peut lire mais pas écrire les tarifs » demanderait un modèle de droits que
// personne n'entretiendrait à jour.
const (
	ScopeCore = "core"
	ScopeFood = "food"
	ScopeVTC  = "vtc"
)

// Allows dit si ces claims couvrent la verticale demandée.
func (c Claims) Allows(scope string) bool {
	if len(c.Scopes) == 0 {
		return true // aucune restriction déclarée
	}
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// GenerateAccess mints an access token. `scopes` est VARIADIQUE parce que
// l'absence de portée est un cas légitime et courant — un client, un livreur,
// un administrateur sans restriction — et non un oubli à signaler.
func (m *Manager) GenerateAccess(userID, role string, scopes ...string) (string, error) {
	return m.generate(userID, role, TokenTypeAccess, m.accessTTL, scopes)
}

func (m *Manager) GenerateRefresh(userID, role string, scopes ...string) (string, error) {
	return m.generate(userID, role, TokenTypeRefresh, m.refreshTTL, scopes)
}

func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

func (m *Manager) generate(userID, role, typ string, ttl time.Duration, scopes []string) (string, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":  userID,
		"role": role,
		"typ":  typ,
		"iat":  now.Unix(),
		"exp":  now.Add(ttl).Unix(),
	}
	// ⚠️ Omis quand il n'y a pas de restriction, plutôt qu'écrit vide. Un
	// tableau vide dans un jeton se lit « aucune portée autorisée » par un
	// futur lecteur qui n'aurait pas la convention en tête ; un champ absent
	// ne prête pas à cette lecture.
	if len(scopes) > 0 {
		claims["scp"] = scopes
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

// Verify parses and validates a token and returns its claims.
func (m *Manager) Verify(tokenStr string) (Claims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil || !token.Valid {
		return Claims{}, ErrInvalidToken.WithCause(err)
	}
	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrInvalidToken
	}
	sub, _ := mapClaims["sub"].(string)
	role, _ := mapClaims["role"].(string)
	typ, _ := mapClaims["typ"].(string)
	if sub == "" || role == "" {
		return Claims{}, ErrInvalidToken
	}
	out := Claims{UserID: sub, Role: role, Type: typ}
	if raw, ok := mapClaims["scp"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				out.Scopes = append(out.Scopes, s)
			}
		}
	}
	return out, nil
}

type ctxKey struct{}

type ctxUser struct {
	scopes []string
	id     string
	role   string
}

// WithUser stores the authenticated user in the context.
func WithUser(ctx context.Context, id, role string) context.Context {
	return context.WithValue(ctx, ctxKey{}, ctxUser{id: id, role: role})
}

// WithClaims stores the user AND its scopes.
//
// ⚠️ Une seconde fonction plutôt qu'un troisième argument à `WithUser` : cette
// dernière est appelée dans des dizaines de tests, et leur imposer un `nil`
// de plus n'aurait rien appris à personne. Le middleware, lui, doit passer par
// ici — sinon la portée s'arrête au jeton et ne protège rien.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, ctxUser{id: c.UserID, role: c.Role, scopes: c.Scopes})
}

// ScopesFromContext rend les portées du porteur, vides si aucune restriction.
func ScopesFromContext(ctx context.Context) []string {
	u, _ := ctx.Value(ctxKey{}).(ctxUser)
	return u.scopes
}

// AllowsFromContext dit si le porteur couvre cette verticale.
//
// Vide = toutes : voir `Claims.Scopes`.
func AllowsFromContext(ctx context.Context, scope string) bool {
	return Claims{Scopes: ScopesFromContext(ctx)}.Allows(scope)
}

// UserFromContext returns the authenticated user id.
func UserFromContext(ctx context.Context) (string, bool) {
	u, ok := ctx.Value(ctxKey{}).(ctxUser)
	return u.id, ok && u.id != ""
}

// RoleFromContext returns the authenticated user role.
func RoleFromContext(ctx context.Context) (string, bool) {
	u, ok := ctx.Value(ctxKey{}).(ctxUser)
	return u.role, ok && u.role != ""
}
