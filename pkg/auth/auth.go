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
}

type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (m *Manager) GenerateAccess(userID, role string) (string, error) {
	return m.generate(userID, role, TokenTypeAccess, m.accessTTL)
}

func (m *Manager) GenerateRefresh(userID, role string) (string, error) {
	return m.generate(userID, role, TokenTypeRefresh, m.refreshTTL)
}

func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

func (m *Manager) generate(userID, role, typ string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  userID,
		"role": role,
		"typ":  typ,
		"iat":  now.Unix(),
		"exp":  now.Add(ttl).Unix(),
	})
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
	return Claims{UserID: sub, Role: role, Type: typ}, nil
}

type ctxKey struct{}

type ctxUser struct {
	id   string
	role string
}

// WithUser stores the authenticated user in the context.
func WithUser(ctx context.Context, id, role string) context.Context {
	return context.WithValue(ctx, ctxKey{}, ctxUser{id: id, role: role})
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
