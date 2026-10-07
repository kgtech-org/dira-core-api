package user

// L'INSCRIPTION ET LA CONNEXION PAR CODE À USAGE UNIQUE — le téléphone, et
// rien d'autre.
//
// Un client entre son numéro, reçoit six chiffres, les saisit : il est inscrit
// s'il était inconnu, connecté s'il était déjà là. Pas de mot de passe à
// choisir, pas de mot de passe à retrouver — et c'est le point : sur ce
// marché, le mot de passe oublié est la première cause d'abandon, et le socle
// n'avait aucune porte de réinitialisation à offrir.
//
// ⚠️ RÉSERVÉ AUX CLIENTS, ET C'EST UNE RÈGLE DE SÉCURITÉ, PAS UN PÉRIMÈTRE.
// Un code à six chiffres remplace un mot de passe : l'ouvrir aux chauffeurs,
// aux marchands ou à l'administration ferait du numéro de téléphone — qui
// s'affiche sur une plaque, se donne à un passager, se lit dans un annuaire —
// le seul secret protégeant un compte qui débite des portefeuilles. Un compte
// qui n'est pas `client` est refusé à la vérification, même avec le bon code.
//
// ⚠️ LA PASSERELLE N'EXISTE PAS ENCORE. Tant qu'aucun fournisseur WhatsApp ou
// SMS n'est câblé, l'expéditeur par défaut (`echo`) RETOURNE LE CODE DANS LA
// RÉPONSE. C'est ce qui permet de câbler les applications aujourd'hui, et
// c'est une faille assumée et temporaire : quiconque connaît un numéro entre
// dans le compte. Le service le crie au démarrage, la réponse le dit
// (`delivery: "echo"`, `dev_code`), et le jour où un vrai canal est branché,
// ce champ disparaît sans qu'une seule ligne d'application ne change — une
// application qui lit `dev_code` doit le traiter comme un bonus, jamais comme
// un dû.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// CollectionOTP porte les codes en cours. Une ligne par TÉLÉPHONE, pas par
// demande : un numéro n'a jamais qu'un code vivant, et redemander remplace le
// précédent. Empiler les codes aurait multiplié par le nombre de demandes les
// chances d'en deviner un.
const CollectionOTP = "auth_otp_codes"

// appOfClients est le mot que les DEUX applications de clients — les repas et
// les courses — envoient dans `app`. Voir `appRole` : elles partagent le rôle,
// donc elles partagent la porte.
const appOfClients = "client"

// Canaux de remise.
const (
	// ChannelEcho ne remet rien : il rend le code à l'appelant. Voir l'en-tête.
	ChannelEcho     = "echo"
	ChannelWhatsApp = "whatsapp"
	ChannelSMS      = "sms"
)

// Réglages par défaut, tous surchargeables (voir `OTPPolicy`).
const (
	otpCodeDigits       = 6
	otpDefaultTTL       = 5 * time.Minute
	otpDefaultResend    = 60 * time.Second
	otpDefaultAttempts  = 5
	otpDefaultPerWindow = 5
	otpRequestWindow    = time.Hour
)

var (
	errOTPTooSoon = apperr.New("otp_too_soon",
		"a code was just sent to this number; wait before asking for another",
		http.StatusTooManyRequests)
	errOTPTooMany = apperr.New("otp_too_many_requests",
		"too many codes requested for this number; try again later",
		http.StatusTooManyRequests)
	// ⚠️ UN SEUL CODE POUR « PAS DE CODE » ET « MAUVAIS CODE ». Les
	// distinguer dirait à qui essaie des numéros au hasard lesquels ont une
	// demande en cours, c'est-à-dire lesquels sont en train de se connecter.
	errOTPInvalid = apperr.Unauthorized("otp_invalid", "invalid code")
	errOTPExpired = apperr.Unauthorized("otp_expired", "this code has expired; ask for a new one")
	// ⚠️ Le code est MORT, pas seulement refusé : sans cela, cinq tentatives
	// suivies d'une sixième demande donneraient un essai de plus à chaque
	// fois, et le plafond ne plafonnerait rien.
	errOTPAttempts = apperr.Unauthorized("otp_too_many_attempts",
		"too many wrong codes; ask for a new one")
	// errOTPNotAvailable refuse la porte à un compte qui n'est pas client, et
	// à toute application qui n'est pas une application de client.
	errOTPNotAvailable = apperr.Forbidden("otp_not_available",
		"this account signs in with a password")
)

// OTPCode est une demande de code en cours, telle qu'elle vit en base.
type OTPCode struct {
	ID    primitive.ObjectID `bson:"_id,omitempty"`
	Phone string             `bson:"phone"`
	// Hash est l'empreinte du code, jamais le code.
	//
	// ⚠️ ET CE N'EST PAS UN MOT DE PASSE HACHÉ : six chiffres se retrouvent
	// par force brute en un instant si l'empreinte fuite. Ce qui protège
	// vraiment, c'est le POIVRE — une clé qui ne vit pas dans la base, donc
	// qu'une sauvegarde volée n'emporte pas — et le fait qu'un code meure en
	// cinq minutes, après cinq essais. Dit ici pour que personne ne croie le
	// contraire.
	Hash      string    `bson:"hash"`
	Channel   string    `bson:"channel"`
	ExpiresAt time.Time `bson:"expires_at"`
	Attempts  int       `bson:"attempts"`
	// Requests et WindowStart plafonnent les DEMANDES sur une heure glissante.
	// Ils vivent dans le même document que le code parce qu'ils comptent la
	// même chose — ce numéro-ci — et qu'un second document aurait fallu le
	// purger séparément.
	Requests    int       `bson:"requests"`
	WindowStart time.Time `bson:"window_start"`
	SentAt      time.Time `bson:"sent_at"`
	// PurgeAt est la date à laquelle Mongo efface la ligne (index TTL). Elle
	// suit la FENÊTRE de cadence, pas l'échéance du code : effacer à
	// l'expiration du code remettrait le compteur horaire à zéro toutes les
	// cinq minutes.
	PurgeAt time.Time `bson:"purge_at"`
}

// OTPSender remet le code à son destinataire.
//
// Déclarée côté consommateur, comme le reste : le socle n'a pas à connaître
// l'agrégateur du jour. Le canal RENDU est celui qui a servi — l'appelant
// annonce `whatsapp` et le fournisseur peut répondre `sms` s'il a basculé.
type OTPSender interface {
	SendCode(ctx context.Context, phone, code, locale, channel string) (string, error)
}

// EchoSender ne remet rien et rend `echo` : le code repart dans la réponse
// HTTP. C'est l'expéditeur par défaut tant qu'aucune passerelle n'est câblée.
type EchoSender struct{}

func (EchoSender) SendCode(ctx context.Context, phone, _, _, _ string) (string, error) {
	// ⚠️ LE CODE N'EST PAS JOURNALISÉ. Il part déjà en clair dans la réponse ;
	// l'écrire aussi dans les journaux le ferait entrer dans la supervision,
	// la rétention et les sauvegardes — trois endroits de plus où il traîne,
	// pour aucun usage.
	slog.WarnContext(ctx, "user: OTP non remis — expéditeur echo, le code part dans la réponse", "phone", maskPhone(phone))
	return ChannelEcho, nil
}

// OTPPolicy règle la durée de vie, la cadence et les plafonds.
type OTPPolicy struct {
	TTL         time.Duration
	Resend      time.Duration
	MaxAttempts int
	// MaxPerWindow est le nombre de codes qu'un numéro peut demander par heure.
	MaxPerWindow int
}

func (p OTPPolicy) withDefaults() OTPPolicy {
	if p.TTL <= 0 {
		p.TTL = otpDefaultTTL
	}
	if p.Resend <= 0 {
		p.Resend = otpDefaultResend
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = otpDefaultAttempts
	}
	if p.MaxPerWindow <= 0 {
		p.MaxPerWindow = otpDefaultPerWindow
	}
	return p
}

// EnableOTP branche la porte par code. Sans cet appel, les deux routes
// répondent `otp_not_available` : une fonction d'authentification à moitié
// câblée doit se taire, pas deviner.
//
// `pepper` ne vit pas dans la base — voir `OTPCode.Hash`.
func (s *Service) EnableOTP(sender OTPSender, pepper string, policy OTPPolicy) {
	s.otpSender = sender
	s.otpPepper = pepper
	s.otpPolicy = policy.withDefaults()
}

// RequestOTP envoie un code au numéro donné, et dit ce qu'il a fait.
//
// ⚠️ ELLE NE DIT JAMAIS SI LE NUMÉRO EST CONNU. Rendre « compte existant »
// ferait de cette route un annuaire : qui veut savoir si quelqu'un est client
// de Dira n'aurait qu'à poster son numéro. L'application demande le nom APRÈS
// la vérification, quand la réponse lui dit que le compte vient de naître.
func (s *Service) RequestOTP(ctx context.Context, req OTPRequest) (OTPRequestResponse, error) {
	if s.otpSender == nil {
		return OTPRequestResponse{}, errOTPNotAvailable
	}
	// L'application qui demande doit être une application de CLIENT. Vide =
	// toléré, comme partout ailleurs : une application pas encore mise à jour
	// continue de fonctionner, et le rôle du compte sera de toute façon vérifié
	// à la vérification.
	if req.App != "" && req.App != appOfClients {
		return OTPRequestResponse{}, errOTPNotAvailable
	}
	phone, err := canonPhone(req.Phone)
	if err != nil {
		return OTPRequestResponse{}, err
	}

	// Le compte est lu AVANT d'envoyer quoi que ce soit : un chauffeur ou un
	// administrateur ne doit pas recevoir de code du tout, pas même un code
	// qui serait refusé ensuite.
	existing, err := s.repo.FindByPhone(ctx, phone)
	if err != nil {
		return OTPRequestResponse{}, apperr.Internal(err)
	}
	if existing != nil && existing.Role != auth.RoleClient {
		return OTPRequestResponse{}, errOTPNotAvailable
	}

	now := time.Now().UTC()
	policy := s.otpPolicy.withDefaults()
	current, err := s.repo.FindOTP(ctx, phone)
	if err != nil {
		return OTPRequestResponse{}, apperr.Internal(err)
	}

	requests := 1
	windowStart := now
	if current != nil {
		if now.Sub(current.SentAt) < policy.Resend {
			return OTPRequestResponse{}, errOTPTooSoon
		}
		if now.Sub(current.WindowStart) < otpRequestWindow {
			if current.Requests >= policy.MaxPerWindow {
				return OTPRequestResponse{}, errOTPTooMany
			}
			requests = current.Requests + 1
			windowStart = current.WindowStart
		}
	}

	code, err := newOTPCode()
	if err != nil {
		return OTPRequestResponse{}, apperr.Internal(err)
	}
	channel := normaliseChannel(req.Channel)
	sent, err := s.otpSender.SendCode(ctx, phone, code, normalizeLocale(req.Locale), channel)
	if err != nil {
		// ⚠️ RIEN N'EST ÉCRIT SI RIEN N'EST PARTI. Poser le code puis échouer
		// à le remettre aurait brûlé une demande de la fenêtre horaire et
		// bloqué la suivante pendant une minute, pour un code que personne n'a
		// reçu.
		slog.ErrorContext(ctx, "user: OTP non remis", "phone", maskPhone(phone), "channel", channel, "error", err)
		return OTPRequestResponse{}, apperr.New("otp_delivery_failed",
			"could not send the code; try again", http.StatusServiceUnavailable)
	}
	if sent != "" {
		channel = sent
	}

	rec := &OTPCode{
		Phone:       phone,
		Hash:        s.hashOTP(phone, code),
		Channel:     channel,
		ExpiresAt:   now.Add(policy.TTL),
		Attempts:    0,
		Requests:    requests,
		WindowStart: windowStart,
		SentAt:      now,
		PurgeAt:     windowStart.Add(otpRequestWindow),
	}
	if err := s.repo.SaveOTP(ctx, rec); err != nil {
		return OTPRequestResponse{}, apperr.Internal(err)
	}

	resp := OTPRequestResponse{
		Sent:        true,
		Channel:     channel,
		ExpiresAt:   rec.ExpiresAt,
		ResendAfter: int(policy.Resend / time.Second),
	}
	if channel == ChannelEcho {
		resp.DevCode = code
	}
	return resp, nil
}

// VerifyOTP consomme le code : elle inscrit le compte s'il n'existait pas, et
// ouvre une session dans les deux cas.
func (s *Service) VerifyOTP(ctx context.Context, req OTPVerifyRequest) (AuthResponse, error) {
	if s.otpSender == nil {
		return AuthResponse{}, errOTPNotAvailable
	}
	if req.App != "" && req.App != appOfClients {
		return AuthResponse{}, errOTPNotAvailable
	}
	phone, err := canonPhone(req.Phone)
	if err != nil {
		return AuthResponse{}, errOTPInvalid
	}

	rec, err := s.repo.FindOTP(ctx, phone)
	if err != nil {
		return AuthResponse{}, apperr.Internal(err)
	}
	if rec == nil {
		return AuthResponse{}, errOTPInvalid
	}
	now := time.Now().UTC()
	if now.After(rec.ExpiresAt) {
		return AuthResponse{}, errOTPExpired
	}
	policy := s.otpPolicy.withDefaults()
	if rec.Attempts >= policy.MaxAttempts {
		_ = s.repo.DeleteOTP(ctx, phone)
		return AuthResponse{}, errOTPAttempts
	}
	// Comparaison à temps constant : la durée d'un `==` sur des chaînes dit
	// combien de caractères de tête sont justes.
	if subtle.ConstantTimeCompare([]byte(rec.Hash), []byte(s.hashOTP(phone, strings.TrimSpace(req.Code)))) != 1 {
		attempts, aerr := s.repo.IncOTPAttempts(ctx, phone)
		if aerr != nil {
			return AuthResponse{}, apperr.Internal(aerr)
		}
		if attempts >= policy.MaxAttempts {
			_ = s.repo.DeleteOTP(ctx, phone)
			return AuthResponse{}, errOTPAttempts
		}
		return AuthResponse{}, errOTPInvalid
	}

	u, err := s.repo.FindByPhone(ctx, phone)
	if err != nil {
		return AuthResponse{}, apperr.Internal(err)
	}
	// ⚠️ LE CODE EST CONSOMMÉ AVANT TOUT CE QUI SUIT, et avant même de savoir
	// si la connexion aboutira. Un code qui survivrait à un refus — compte
	// suspendu, mauvaise application — resterait valide pour être rejoué
	// ailleurs.
	if err := s.repo.DeleteOTP(ctx, phone); err != nil {
		return AuthResponse{}, apperr.Internal(err)
	}

	created := false
	if u == nil {
		u, err = s.createClientFromOTP(ctx, phone, req)
		if err != nil {
			return AuthResponse{}, err
		}
		created = true
	} else {
		if u.Role != auth.RoleClient {
			return AuthResponse{}, errOTPNotAvailable
		}
		if u.Status == StatusSuspended {
			return AuthResponse{}, errAccountSuspended
		}
		if err := allowedInApp(req.App, u); err != nil {
			return AuthResponse{}, err
		}
	}

	deviceID, chased := s.claimDevice(ctx, u, req.App, req.DeviceID, req.DeviceName)
	pair, err := s.issueTokens(ctx, u, deviceID)
	if err != nil {
		return AuthResponse{}, err
	}
	return AuthResponse{
		User:         s.userResponse(ctx, u),
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		Session:      sessionResponse(u, deviceID, chased),
		Maps:         s.basemap(ctx, u),
		AppLock:      s.appLock(ctx, u),
		Created:      created,
	}, nil
}

// createClientFromOTP ouvre le compte d'un client qui vient de prouver son
// numéro.
//
// ⚠️ SANS MOT DE PASSE, et `PasswordHash` reste vide. `VerifyPassword` refuse
// une empreinte vide (voir `password.go`) : ce compte ne peut donc pas se
// connecter par `POST /auth/login`, et c'est exactement ce qu'on veut — une
// chaîne vide qui vaudrait « pas de mot de passe exigé » ouvrirait tous les
// comptes du pays.
//
// ⚠️ LE NOM EST FACULTATIF. Le numéro fait office de nom d'affichage tant que
// la personne n'en a pas donné — la même convention que `PATCH /me`, qui
// traite « nom égal au téléphone » comme « nom non posé ». Exiger un nom
// aurait ajouté un écran entre le code et la première course.
func (s *Service) createClientFromOTP(ctx context.Context, phone string, req OTPVerifyRequest) (*User, error) {
	now := time.Now().UTC()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = phone
	}
	u := &User{
		Role:      auth.RoleClient,
		Phone:     phone,
		Name:      name,
		FirstName: strings.TrimSpace(req.FirstName),
		LastName:  strings.TrimSpace(req.LastName),
		Status:    StatusActive,
		Country:   s.countryForNew(ctx, phone),
		CreatedAt: now,
		UpdatedAt: now,
		AgentApp:  newAgentApp(auth.RoleClient, req.App),
	}
	if err := s.repo.CreateUser(ctx, u); err != nil {
		if errors.Is(err, ErrDuplicatePhone) {
			// Deux vérifications du même code au même instant : la seconde
			// retrouve le compte que la première vient de créer plutôt que de
			// rendre un conflit à quelqu'un qui a fait les choses dans l'ordre.
			if existing, ferr := s.repo.FindByPhone(ctx, phone); ferr == nil && existing != nil {
				return existing, nil
			}
			return nil, errPhoneTaken
		}
		return nil, apperr.Internal(err)
	}
	ctx = country.WithCountry(ctx, u.Country, country.SourceClaims)
	if err := s.wallets.CreateWallet(ctx, u.ID.Hex(), "client"); err != nil {
		// Au mieux, comme à l'inscription ordinaire : sans portefeuille on
		// paie en espèces ou en mobile money, et refuser l'inscription pour un
		// portefeuille vide serait disproportionné.
		slog.WarnContext(ctx, "user: client wallet not created", "user_id", u.ID.Hex(), "error", err)
	}
	return u, nil
}

// hashOTP empreinte le code avec le poivre du déploiement.
func (s *Service) hashOTP(phone, code string) string {
	mac := hmac.New(sha256.New, []byte(s.otpPepper))
	mac.Write([]byte(phone))
	mac.Write([]byte{0})
	mac.Write([]byte(code))
	return hex.EncodeToString(mac.Sum(nil))
}

// newOTPCode tire six chiffres.
//
// ⚠️ `crypto/rand`, et un tirage UNIFORME. `math/rand` ensemencé sur l'horloge
// donne une suite que l'on rejoue, et un modulo sur un tirage non borné rend
// les premiers chiffres plus probables que les derniers : deux façons de
// rétrécir un espace de un million à bien moins.
func newOTPCode() (string, error) {
	max := big.NewInt(1)
	for range otpCodeDigits {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("otp: random: %w", err)
	}
	return fmt.Sprintf("%0*d", otpCodeDigits, n), nil
}

func normaliseChannel(c string) string {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case ChannelSMS:
		return ChannelSMS
	default:
		// WhatsApp par défaut : il coûte moins cher qu'un SMS, il est lu, et
		// c'est déjà le canal par lequel la livraison parle à ses clients.
		return ChannelWhatsApp
	}
}

// maskPhone ne garde que les quatre derniers chiffres. Un numéro complet dans
// un journal est une donnée personnelle qui part en supervision et y reste.
func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return "****"
	}
	return "****" + phone[len(phone)-4:]
}

// --- persistance -----------------------------------------------------------

// SaveOTP remplace le code en cours de ce numéro, ou le crée.
func (r *Repository) SaveOTP(ctx context.Context, c *OTPCode) error {
	_, err := r.otpCodes.UpdateOne(ctx,
		bson.M{"phone": c.Phone},
		bson.M{"$set": bson.M{
			"hash":         c.Hash,
			"channel":      c.Channel,
			"expires_at":   c.ExpiresAt,
			"attempts":     c.Attempts,
			"requests":     c.Requests,
			"window_start": c.WindowStart,
			"sent_at":      c.SentAt,
			"purge_at":     c.PurgeAt,
		}},
		options.Update().SetUpsert(true),
	)
	return err
}

// FindOTP rend le code en cours, ou nil.
func (r *Repository) FindOTP(ctx context.Context, phone string) (*OTPCode, error) {
	var c OTPCode
	err := r.otpCodes.FindOne(ctx, bson.M{"phone": phone}).Decode(&c)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// IncOTPAttempts compte un essai raté et rend le total.
//
// ⚠️ EN BASE, pas en mémoire : deux répliques du socle servent le même
// numéro, et un compteur local aurait donné cinq essais par réplique.
func (r *Repository) IncOTPAttempts(ctx context.Context, phone string) (int, error) {
	var c OTPCode
	err := r.otpCodes.FindOneAndUpdate(ctx,
		bson.M{"phone": phone},
		bson.M{"$inc": bson.M{"attempts": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&c)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return c.Attempts, nil
}

// DeleteOTP consomme le code.
func (r *Repository) DeleteOTP(ctx context.Context, phone string) error {
	_, err := r.otpCodes.DeleteOne(ctx, bson.M{"phone": phone})
	return err
}
