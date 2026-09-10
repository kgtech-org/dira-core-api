// Package auth issues and verifies JWT access/refresh tokens and exposes the
// authenticated user through the request context.
package auth

import (
	"context"
	"fmt"
	"slices"
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
	// Scopes borne un ADMINISTRATEUR à certaines VERTICALES : « core »,
	// « food », « vtc ».
	//
	// ⚠️ VIDE = AUCUNE. C'est la lecture littérale, et c'est la seule qui se
	// tienne : une liste vide de permissions n'accorde rien. La convention
	// inverse — vide = toutes — a existé ici pour ne casser aucun jeton en
	// vol, et elle a coûté cher : il fallait une portée SENTINELLE pour
	// suspendre quelqu'un sans le rendre tout-puissant, et une phrase
	// d'avertissement dans l'interface pour que des cases à cocher vides ne
	// se lisent pas comme « aucun accès ». Trois mécanismes pour compenser une
	// convention à l'envers.
	//
	// Conséquence assumée : un compte `admin` SANS fiche de staff n'administre
	// rien. C'est voulu — le provisionnement crée la fiche en même temps que
	// le compte.
	//
	// ⚠️ Ne concerne QUE les administrateurs. Un client, un livreur ou un
	// marchand n'a pas de portée et ne doit pas en avoir : c'est son RÔLE qui
	// borne ce qu'il atteint. Voir `middleware.RequireScope`.
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

// AllScopes est l'ensemble des verticales de la plateforme.
//
// Exporté pour que le provisionnement et la console lisent la MÊME liste que
// la validation — deux listes divergeraient au premier métier ajouté.
var AllScopes = []string{ScopeCore, ScopeFood, ScopeVTC}

// Allows dit si ces claims couvrent la verticale demandée.
//
// ⚠️ Appartenance PURE : pas de cas particulier, pas de valeur magique. Une
// liste vide n'accorde rien. Toute la subtilité — « qui a besoin d'une portée
// et qui n'en a pas besoin » — vit dans `middleware.RequireScope`, qui connaît
// le rôle ; la mettre ici obligerait chaque lecteur de cette fonction à
// deviner de quel type de compte on parle.
func (c Claims) Allows(scope string) bool {
	return slices.Contains(c.Scopes, scope)
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
