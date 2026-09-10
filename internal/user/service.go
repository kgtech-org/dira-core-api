package user

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// Repo abstracts persistence for the user service. Implemented by
// *Repository; faked in tests (fakes must reproduce the duplicate-key
// semantics of the unique phone and token_hash indexes).
type Repo interface {
	CreateUser(ctx context.Context, u *User) error
	FindByPhone(ctx context.Context, phone string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByID(ctx context.Context, id primitive.ObjectID) (*User, error)
	FindNamesByIDs(ctx context.Context, ids []primitive.ObjectID) (map[string]string, error)
	UpdateUser(ctx context.Context, u *User) error
	DeleteUser(ctx context.Context, id primitive.ObjectID) error
	InsertRefreshToken(ctx context.Context, t *RefreshToken) error
	DeleteRefreshTokenByHash(ctx context.Context, tokenHash string) (bool, error)

	// Carnet d'adresses. Le client répétait jusqu'ici son adresse à chaque
	// commande, avec ses indications de porte.
	ListAddresses(ctx context.Context, userID primitive.ObjectID) ([]Address, error)
	CountAddresses(ctx context.Context, userID primitive.ObjectID) (int64, error)
	FindAddress(ctx context.Context, userID, id primitive.ObjectID) (*Address, error)
	SaveAddress(ctx context.Context, a *Address) error
	DeleteAddress(ctx context.Context, userID, id primitive.ObjectID) (bool, error)
	ClearDefaultAddress(ctx context.Context, userID, except primitive.ObjectID) error

	// Administration des comptes : chercher, lire, suspendre. Voir
	// backoffice.go — ces lectures servent la console, et la fiche complète
	// d'un livreur se compose ensuite dans la verticale.
	ListAccounts(ctx context.Context, f AccountFilter, cursor string, limit int) ([]AccountRow, string, error)
	AccountByID(ctx context.Context, id string) (*AccountRow, error)
	AccountsByIDs(ctx context.Context, ids []string) ([]AccountRow, error)
	SetAccountStatus(ctx context.Context, id, status string) (*AccountRow, error)
}

// WalletCreator creates the driver token wallet at registration time. Wiring
// injects the token service (its CreateWallet is idempotent).
type WalletCreator interface {
	CreateWallet(ctx context.Context, ownerID, walletType string) error
}

var (
	errPhoneTaken         = apperr.Conflict("phone_taken", "this phone number is already registered")
	errInvalidCredentials = apperr.Unauthorized("invalid_credentials", "invalid phone number or password")
	errAccountSuspended   = apperr.Forbidden("account_suspended", "this account is suspended")
	errUserNotFound       = apperr.NotFound("user_not_found", "user not found")
)

// Service implements accounts and authentication.
type Service struct {
	repo    Repo
	tokens  *auth.Manager
	wallets WalletCreator
}

// NewService builds the user service. tokens issues/verifies JWTs; wallets is
// the token module adapter used to create driver wallets at registration.
func NewService(repo Repo, tokens *auth.Manager, wallets WalletCreator) *Service {
	return &Service{repo: repo, tokens: tokens, wallets: wallets}
}

// Register creates an account and returns it with a fresh token pair. The
// admin role is not self-assignable: only client, driver and merchant are
// accepted (enforced by DTO validation and re-checked here). Driver accounts
// get a driver token wallet; if wallet creation fails the user is deleted so
// no orphan account remains.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (AuthResponse, error) {
	role := req.Role
	if role == "" {
		role = auth.RoleClient
	}
	// ⚠️ `admin` n'est PAS acceptable ici, et cette porte est publique : la
	// laisser ouverte donnerait les pleins pouvoirs à quiconque poste un
	// formulaire d'inscription. Le provisionnement d'un administrateur passe
	// par `EnsureAccount`, derrière le secret de service.
	switch role {
	case auth.RoleClient, auth.RoleDriver, auth.RoleMerchant:
	default:
		return AuthResponse{}, apperr.Validation("role must be client, driver or merchant")
	}
	return s.register(ctx, req, role)
}

// register crée le compte, une fois le rôle ADMIS par l'appelant.
//
// Séparé de `Register` pour une raison précise : l'inscription publique et le
// provisionnement d'un service n'admettent pas les mêmes rôles, et faire passer
// le second par la validation du premier revenait à annoncer un pouvoir —
// « ouvrir un compte de n'importe quel rôle » — que le socle refusait ensuite
// en silence, avec un 422 que personne ne savait lire.
func (s *Service) register(ctx context.Context, req RegisterRequest, role string) (AuthResponse, error) {

	hash, err := HashPassword(req.Password)
	if err != nil {
		return AuthResponse{}, apperr.Internal(err)
	}

	now := time.Now().UTC()
	u := &User{
		Role:         role,
		Phone:        req.Phone,
		Name:         req.Name,
		Email:        strings.ToLower(req.Email),
		PasswordHash: hash,
		Status:       StatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		if errors.Is(err, ErrDuplicatePhone) {
			return AuthResponse{}, errPhoneTaken
		}
		return AuthResponse{}, apperr.Internal(err)
	}

	if role == auth.RoleDriver {
		if err := s.wallets.CreateWallet(ctx, u.ID.Hex(), "driver"); err != nil {
			// Compensate: never leave a driver account without a wallet.
			if delErr := s.repo.DeleteUser(ctx, u.ID); delErr != nil {
				return AuthResponse{}, apperr.Internal(fmt.Errorf("wallet creation failed (%w) and user cleanup failed: %w", err, delErr))
			}
			return AuthResponse{}, apperr.Internal(fmt.Errorf("create driver wallet: %w", err))
		}
	}
	if role == auth.RoleClient {
		// Le portefeuille d'ARGENT du client — « Dira Cash ». AU MIEUX, à la
		// différence de celui du livreur : sans jetons, un client peut
		// commander tout de suite en mobile money ou en espèces. Refuser
		// l'inscription pour un portefeuille vide serait disproportionné.
		if err := s.wallets.CreateWallet(ctx, u.ID.Hex(), "client"); err != nil {
			slog.WarnContext(ctx, "user: client wallet not created", "user_id", u.ID.Hex(), "error", err)
		}
	}

	pair, err := s.issueTokens(ctx, u)
	if err != nil {
		return AuthResponse{}, err
	}
	return AuthResponse{User: newUserResponse(u), AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken}, nil
}

// Login authenticates by phone + password, or email + password (back-office
// accounts). An unknown identifier or wrong password both return
// invalid_credentials (401); a suspended account with correct credentials
// returns account_suspended (403).
func (s *Service) Login(ctx context.Context, req LoginRequest) (AuthResponse, error) {
	if (req.Phone == "") == (req.Email == "") {
		return AuthResponse{}, apperr.Validation("provide exactly one of phone or email")
	}
	var u *User
	var err error
	if req.Email != "" {
		u, err = s.repo.FindByEmail(ctx, req.Email)
	} else {
		u, err = s.repo.FindByPhone(ctx, req.Phone)
	}
	if err != nil {
		return AuthResponse{}, apperr.Internal(err)
	}
	if u == nil {
		return AuthResponse{}, errInvalidCredentials
	}
	ok, err := VerifyPassword(req.Password, u.PasswordHash)
	if err != nil {
		return AuthResponse{}, apperr.Internal(err)
	}
	if !ok {
		return AuthResponse{}, errInvalidCredentials
	}
	if u.Status == StatusSuspended {
		return AuthResponse{}, errAccountSuspended
	}

	pair, err := s.issueTokens(ctx, u)
	if err != nil {
		return AuthResponse{}, err
	}
	return AuthResponse{User: newUserResponse(u), AccessToken: pair.AccessToken, RefreshToken: pair.RefreshToken}, nil
}

// Refresh rotates a refresh token: the presented token is verified, its
// stored hash deleted (single use) and a new pair issued. A token that was
// already rotated, revoked or never issued is rejected.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPairResponse, error) {
	jwtPart, ok := splitRefreshToken(refreshToken)
	if !ok {
		return TokenPairResponse{}, auth.ErrInvalidToken
	}
	claims, err := s.tokens.Verify(jwtPart)
	if err != nil || claims.Type != auth.TokenTypeRefresh {
		return TokenPairResponse{}, auth.ErrInvalidToken
	}

	deleted, err := s.repo.DeleteRefreshTokenByHash(ctx, hashToken(refreshToken))
	if err != nil {
		return TokenPairResponse{}, apperr.Internal(err)
	}
	if !deleted {
		return TokenPairResponse{}, auth.ErrInvalidToken
	}

	userID, err := primitive.ObjectIDFromHex(claims.UserID)
	if err != nil {
		return TokenPairResponse{}, auth.ErrInvalidToken
	}
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return TokenPairResponse{}, apperr.Internal(err)
	}
	if u == nil {
		return TokenPairResponse{}, auth.ErrInvalidToken
	}
	if u.Status == StatusSuspended {
		return TokenPairResponse{}, errAccountSuspended
	}

	return s.issueTokens(ctx, u)
}

// Logout invalidates the presented refresh token. Idempotent: logging out an
// already-invalidated token succeeds.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if _, err := s.repo.DeleteRefreshTokenByHash(ctx, hashToken(refreshToken)); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// Me returns the caller's profile.
func (s *Service) Me(ctx context.Context, userID string) (UserResponse, error) {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return UserResponse{}, err
	}
	return newUserResponse(u), nil
}

// UpdateProfile updates the caller's name and/or email.
func (s *Service) UpdateProfile(ctx context.Context, userID string, req UpdateMeRequest) (UserResponse, error) {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return UserResponse{}, err
	}
	if req.Name != nil {
		u.Name = *req.Name
	}
	if req.Email != nil {
		u.Email = strings.ToLower(*req.Email)
	}
	if req.AvatarURL != nil {
		u.AvatarURL = *req.AvatarURL
	}
	if req.FirstName != nil {
		u.FirstName = strings.TrimSpace(*req.FirstName)
	}
	if req.LastName != nil {
		u.LastName = strings.TrimSpace(*req.LastName)
	}
	if req.Gender != nil {
		u.Gender = *req.Gender
	}
	if req.BirthDate != nil {
		if *req.BirthDate == "" {
			u.BirthDate = nil
		} else {
			d, err := time.Parse("2006-01-02", *req.BirthDate)
			if err != nil {
				return UserResponse{}, apperr.Validation("birth_date must be YYYY-MM-DD").WithCause(err)
			}
			if d.After(time.Now().UTC()) {
				return UserResponse{}, apperr.Validation("birth_date cannot be in the future")
			}
			u.BirthDate = &d
		}
	}
	// Le nom d'AFFICHAGE suit les prénom/nom quand il n'a jamais été posé à
	// la main : un compte créé au téléphone puis complété au formulaire
	// afficherait sinon toujours son numéro.
	if u.Name == "" || u.Name == u.Phone {
		if full := strings.TrimSpace(u.FirstName + " " + u.LastName); full != "" {
			u.Name = full
		}
	}
	u.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateUser(ctx, u); err != nil {
		return UserResponse{}, apperr.Internal(err)
	}
	return newUserResponse(u), nil
}

// UpdatePreferences règle les préférences du compte.
//
// FUSION et non remplacement : l'écran envoie l'interrupteur qu'on vient de
// basculer, pas les six autres. Remplacer le document remettrait à zéro tout
// ce que la requête ne mentionne pas.
func (s *Service) UpdatePreferences(ctx context.Context, userID string, req UpdatePreferencesRequest) (UserResponse, error) {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return UserResponse{}, err
	}
	if u.Preferences == nil {
		u.Preferences = &Preferences{}
	}
	p := u.Preferences
	if req.OrderUpdates != nil {
		p.OrderUpdates = req.OrderUpdates
	}
	if req.ChatMessages != nil {
		p.ChatMessages = req.ChatMessages
	}
	if req.Promotions != nil {
		p.Promotions = req.Promotions
	}
	if req.Tombola != nil {
		p.TombolaAlerts = req.Tombola
	}
	if req.Sounds != nil {
		p.Sounds = req.Sounds
	}
	if req.Theme != nil {
		p.Theme = *req.Theme
	}
	if req.Locale != nil {
		p.Locale = normalizeLocale(*req.Locale)
	}
	if req.PaymentProvider != nil {
		p.PaymentProvider = strings.TrimSpace(*req.PaymentProvider)
	}
	u.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateUser(ctx, u); err != nil {
		return UserResponse{}, apperr.Internal(err)
	}
	return newUserResponse(u), nil
}

// NotificationPrefs rend, pour le module de notification, la langue choisie et
// l'autorisation d'une catégorie.
//
// Deux réponses en un appel : le module de notification les demande ensemble,
// juste avant d'envoyer, et deux lectures du même document n'apporteraient
// rien.
func (s *Service) NotificationPrefs(ctx context.Context, userID, category string) (locale string, allowed bool) {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		// AU MIEUX : un compte illisible ne doit pas faire taire une
		// notification de livraison. Le défaut est le comportement d'avant
		// ces réglages — tout autorisé, langue de l'appareil.
		return "", true
	}
	if u.Preferences == nil {
		return "", true
	}
	return u.Preferences.Locale, u.Preferences.Allows(category)
}

// PreferredPaymentProvider rend l'opérateur mobile money habituel d'un
// client, ou "".
//
// PRÉSÉLECTION seulement : aucun jeton de paiement n'est conservé, et chaque
// paiement passe par l'opérateur comme la première fois. C'est la seule forme
// de « moyen enregistré » qui ne demande pas de garder chez nous des données
// de paiement — et la seule qu'on puisse offrir sans agrément.
func (s *Service) PreferredPaymentProvider(ctx context.Context, userID string) string {
	u, err := s.findUser(ctx, userID)
	if err != nil || u.Preferences == nil {
		return ""
	}
	return u.Preferences.PaymentProvider
}

// normalizeLocale ramène « fr-FR », « FR_fr » à « fr ».
func normalizeLocale(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' || s[i] == '_' {
			s = s[:i]
			break
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// --- helpers ---

func (s *Service) findUser(ctx context.Context, userID string) (*User, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id").WithCause(err)
	}
	u, err := s.repo.FindByID(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if u == nil {
		return nil, errUserNotFound
	}
	return u, nil
}

// issueTokens generates an access/refresh pair and stores the refresh token
// hash for rotation.
//
// The refresh token handed to clients is "<jwt>.<nonce>": the JWT (from
// auth.Manager) carries identity, type and expiry; the random nonce makes
// every issuance unique. Without it, two refresh tokens issued for the same
// user within the same second would be byte-identical (JWT iat has second
// granularity), colliding on the unique token_hash index and defeating
// single-use rotation.
func (s *Service) issueTokens(ctx context.Context, u *User) (TokenPairResponse, error) {
	access, err := s.tokens.GenerateAccess(u.ID.Hex(), u.Role)
	if err != nil {
		return TokenPairResponse{}, apperr.Internal(err)
	}
	refreshJWT, err := s.tokens.GenerateRefresh(u.ID.Hex(), u.Role)
	if err != nil {
		return TokenPairResponse{}, apperr.Internal(err)
	}
	nonce, err := newNonce()
	if err != nil {
		return TokenPairResponse{}, apperr.Internal(err)
	}
	refresh := refreshJWT + "." + nonce
	now := time.Now().UTC()
	rt := &RefreshToken{
		TokenHash: hashToken(refresh),
		UserID:    u.ID,
		ExpiresAt: now.Add(s.tokens.RefreshTTL()),
		CreatedAt: now,
	}
	if err := s.repo.InsertRefreshToken(ctx, rt); err != nil {
		return TokenPairResponse{}, apperr.Internal(err)
	}
	return TokenPairResponse{AccessToken: access, RefreshToken: refresh}, nil
}

// hashToken returns the sha256 hex digest of a refresh token; only hashes are
// stored server-side.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// splitRefreshToken separates the JWT from the issuance nonce in a
// "<header>.<payload>.<signature>.<nonce>" refresh token.
func splitRefreshToken(token string) (jwtPart string, ok bool) {
	if strings.Count(token, ".") != 3 {
		return "", false
	}
	idx := strings.LastIndex(token, ".")
	return token[:idx], true
}

// newNonce returns 16 hex chars of cryptographic randomness.
func newNonce() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("user: generate nonce: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// EnsureMerchantAccount returns the user id behind a phone number, creating a
// `merchant` account when none exists.
//
// Appelé quand un propriétaire ajoute un membre à son personnel. Deux cas :
//
//   - Le compte EXISTE : on le rend tel quel. On ne touche ni son nom ni son
//     mot de passe — quelqu'un d'autre s'en sert peut-être déjà, et un
//     propriétaire ne doit pas pouvoir réécrire le compte d'un tiers en
//     tapant son numéro.
//   - Le compte N'EXISTE PAS : on le crée, avec le mot de passe fourni. Le
//     propriétaire le communique de vive voix, le membre le change ensuite.
//
// ⚠️ Le rôle du compte existant n'est PAS changé. Ajouter un client comme
// membre du personnel lui donnerait le rôle marchand et casserait son
// application : c'est refusé plutôt que silencieusement accepté.
func (s *Service) EnsureMerchantAccount(ctx context.Context, phone, name, password string) (string, error) {
	existing, err := s.repo.FindByPhone(ctx, phone)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if existing != nil {
		if existing.Role != auth.RoleMerchant {
			return "", apperr.Conflict("account_not_merchant",
				"this phone number belongs to an account that is not a merchant")
		}
		return existing.ID.Hex(), nil
	}
	if password == "" {
		return "", apperr.Validation("a password is required to open the member's account")
	}
	resp, err := s.Register(ctx, RegisterRequest{
		Phone: phone, Name: name, Password: password, Role: auth.RoleMerchant,
	})
	if err != nil {
		return "", err
	}
	return resp.User.ID, nil
}

// ContactOf resolves a name and a phone number for one account.
//
// ⚠️ Le TÉLÉPHONE sort d'ici. Cette méthode est le seul chemin par lequel un
// numéro quitte le module, et l'appelant en répond : aujourd'hui c'est la
// course, pour que le livreur AFFECTÉ puisse joindre son client quand la
// conversation ne suffit plus. Ne l'ouvrez pas à une liste.
func (s *Service) ContactOf(ctx context.Context, userID string) (name, phone string, err error) {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return "", "", err
	}
	return u.Name, u.Phone, nil
}

// UserNames resolves display names for several accounts at once.
func (s *Service) UserNames(ctx context.Context, ids []string) (map[string]string, error) {
	oids := make([]primitive.ObjectID, 0, len(ids))
	for _, raw := range ids {
		if oid, err := primitive.ObjectIDFromHex(raw); err == nil {
			oids = append(oids, oid)
		}
	}
	return s.repo.FindNamesByIDs(ctx, oids)
}

// EnsureAccount ouvre un compte de n'importe quel rôle, ou rend l'existant.
//
// ⚠️ Sert au PROVISIONNEMENT : le jeu de démonstration d'une verticale, qui ne
// peut plus créer de compte lui-même depuis que l'identité vit ici.
//
// Un téléphone déjà pris rend le compte EXISTANT sans vérifier son rôle — à la
// différence d'`EnsureMerchantAccount`, qui refuse un compte non marchand.
// C'est voulu : le provisionnement rejoue le même jeu de données, et échouer
// parce qu'un compte existe déjà en ferait un outil à usage unique.
func (s *Service) EnsureAccount(ctx context.Context, role, phone, name, email, password string) (string, error) {
	// ⚠️ `admin` est admis ICI, et nulle part ailleurs. C'est un pouvoir plus
	// large que le reste de la surface de service, gardé par le même secret :
	// un service qui peut créer un administrateur peut tout.
	switch role {
	case auth.RoleClient, auth.RoleDriver, auth.RoleMerchant, auth.RoleAdmin:
	default:
		return "", apperr.Validation("role must be client, driver, merchant or admin")
	}
	existing, err := s.repo.FindByPhone(ctx, phone)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if existing != nil {
		// ⚠️ Un e-mail MANQUANT est COMBLÉ, jamais remplacé.
		//
		// Combler n'est pas réécrire : un compte provisionné avant que
		// l'adresse ne soit transmise ne peut pas ouvrir la console, et un
		// seed incapable de réparer ce qu'il a créé est un seed qu'on ne peut
		// pas relancer. Écraser une adresse existante, en revanche, couperait
		// l'accès de quelqu'un qui s'en sert.
		if email != "" && existing.Email == "" {
			existing.Email = strings.ToLower(email)
			existing.UpdatedAt = time.Now().UTC()
			if err := s.repo.UpdateUser(ctx, existing); err != nil {
				return "", apperr.Internal(err)
			}
			slog.InfoContext(ctx, "user: email backfilled on an existing account",
				"user_id", existing.ID.Hex(), "email", existing.Email)
		}
		return existing.ID.Hex(), nil
	}
	// `register`, pas `Register` : l'inscription publique refuse `admin`, et
	// c'est exactement ce qu'on veut qu'elle continue de faire.
	// ⚠️ L'E-MAIL COMPTE. La console d'administration se connecte PAR E-MAIL :
	// un administrateur provisionné sans adresse est un administrateur qui ne
	// peut pas ouvrir la console — et le message qu'il lit, « numéro de
	// téléphone ou mot de passe invalide », ne lui dit rien de ce qui manque.
	resp, err := s.register(ctx, RegisterRequest{
		Phone: phone, Name: name, Email: email, Password: password,
	}, role)
	if err != nil {
		return "", err
	}
	return resp.User.ID, nil
}

// --- administration des comptes ---

// ListAccounts searches accounts for an administration screen.
func (s *Service) ListAccounts(ctx context.Context, f AccountFilter, cursor string, limit int) ([]AccountRow, string, error) {
	return s.repo.ListAccounts(ctx, f, cursor, limit)
}

// AccountByID reads one account.
func (s *Service) AccountByID(ctx context.Context, id string) (*AccountRow, error) {
	return s.repo.AccountByID(ctx, id)
}

// AccountsByIDs reads several accounts at once, for listings a vertical
// decorates with an identity.
func (s *Service) AccountsByIDs(ctx context.Context, ids []string) ([]AccountRow, error) {
	const batch = 200
	if len(ids) > batch {
		return nil, apperr.Validation("too many ids").
			WithMeta(map[string]any{"max": batch, "got": len(ids)})
	}
	return s.repo.AccountsByIDs(ctx, ids)
}

// SetAccountStatus activates or suspends an account and returns the state
// BEFORE the change.
//
// ⚠️ Une SUSPENSION ne ferme pas les sessions en cours : le jeton d'accès
// reste valable jusqu'à son expiration, parce que les verticales le vérifient
// localement, sans appeler le socle. C'est le prix assumé de cette
// vérification locale — voir le commentaire de `pkg/auth`. Le rafraîchissement
// est refusé, donc la porte se referme au plus tard à l'expiration.
func (s *Service) SetAccountStatus(ctx context.Context, id, status string) (*AccountRow, error) {
	switch status {
	case StatusActive, StatusSuspended:
	default:
		return nil, apperr.Validation("status must be active or suspended")
	}
	return s.repo.SetAccountStatus(ctx, id, status)
}
