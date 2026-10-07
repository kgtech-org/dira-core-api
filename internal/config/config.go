// Package config loads dira-core-api's configuration.
//
// Les réglages communs à toute la plateforme viennent de `pkg/config.Base`.
// Ce qui suit appartient au SOCLE, et à lui seul.
package config

import (
	"fmt"
	"time"

	"github.com/kgtech-org/dira-core-api/internal/country"
	core "github.com/kgtech-org/dira-core-api/pkg/config"
)

type Config struct {
	core.Base
	// FCMServiceAccount est le JSON de compte de service Firebase, en clair
	// dans l'environnement. FCMServiceAccountFile en est le chemin, pour les
	// déploiements qui montent un secret en fichier — une clé privée RSA tient
	// mal dans une variable d'environnement.
	//
	// Les deux vides = aucune notification poussée, et c'est dit au démarrage.
	FCMServiceAccount     string
	FCMServiceAccountFile string
	// MockPaymentSecret signe les rappels du prestataire de paiement simulé.
	MockPaymentSecret string
	// ServiceToken authentifie les appels VENUS des verticales.
	//
	// ⚠️ VIDE = routes de service FERMÉES. Une porte de service sans serrure
	// vaut moins que pas de porte : n'importe qui pourrait débiter le
	// portefeuille de n'importe qui.
	ServiceToken string

	// FoodBaseURL est l'adresse de la VERTICALE livraison, pour les rappels
	// que le socle lui doit — un paiement de commande abouti, notamment.
	//
	// ⚠️ VIDE = rappel IMPOSSIBLE, et le webhook du prestataire reçoit une
	// erreur plutôt qu'un accusé de réception. C'est voulu : répondre « reçu »
	// sans avoir prévenu la livraison laisserait une commande payée et jamais
	// confirmée, et le prestataire ne réessaierait pas.
	FoodBaseURL string
	// FoodCallbackToken authentifie les rappels du socle VERS la livraison.
	//
	// ⚠️ DISTINCT de ServiceToken, et ce n'est pas une coquetterie : celui-ci
	// est le secret que le socle PRÉSENTE, l'autre est celui qu'il EXIGE.
	// Réutiliser le même ferait qu'un secret volé chez la livraison ouvrirait
	// la porte de service du socle — c'est-à-dire tous les portefeuilles.
	FoodCallbackToken string

	// VTCBaseURL et VTCCallbackToken : le même contrat pour les COURSES. Le
	// socle prévient la verticale que le `purpose` du paiement désigne — la
	// confirmation d'une course envoyée à la livraison serait refusée, et le
	// passager attendrait une voiture que personne n'a commandée.
	VTCBaseURL       string
	VTCCallbackToken string

	// OTPSender nomme le canal de remise du code à usage unique :
	// `echo` (défaut), `whatsapp` ou `sms`.
	//
	// ⚠️ `echo` NE REMET RIEN : il rend le code dans la réponse HTTP. C'est
	// ce qui permet de câbler les applications avant qu'une passerelle
	// n'existe, et c'est une faille — quiconque connaît un numéro entre dans
	// le compte. Le démarrage le crie ; voir `internal/user/otp.go`.
	OTPSender string
	// OTPTTL, OTPResend, OTPMaxAttempts, OTPMaxPerHour : la vie du code et sa
	// cadence. Réglables parce qu'un marché lent sur les SMS demande une
	// durée plus longue, et qu'on ne redéploie pas pour ça.
	OTPTTL         time.Duration
	OTPResend      time.Duration
	OTPMaxAttempts int
	OTPMaxPerHour  int

	// CountryIPLookupURL et CountryIPLookupField : le fournisseur qui situe
	// une adresse IP, repli de la résolution de pays quand l'application n'a
	// pas de position. `{ip}` est remplacé dans l'URL ; le champ est celui
	// de la réponse JSON qui porte le code alpha-2. Voir
	// `internal/country.HTTPLookup`.
	CountryIPLookupURL   string
	CountryIPLookupField string
	// FinanceIntegrityInterval : la cadence du balayage d'intégrité des
	// portefeuilles et du journal (soldes recalculés, écritures vérifiées).
	FinanceIntegrityInterval time.Duration
	// FinanceJournalSince : depuis quand chaque mouvement doit porter son
	// écriture comptable — la mise en service du journal. Un mouvement plus
	// ancien sans écriture n'est pas un écart.
	FinanceJournalSince time.Time
}

// Load reads the environment, applies defaults and validates.
func Load() (*Config, error) {
	// `dira-media` : le bucket des fichiers envoyés par TOUTE la plateforme —
	// avatars, véhicules, permis, plats, vidéos. Il est au socle parce que
	// l'envoi y est ; le worker de la livraison, qui réencode les vidéos de
	// feed, lit et écrit le MÊME bucket (voir dira-devops).
	base, err := core.LoadBase("dira_core", "dira-media")
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Base:                  base,
		FCMServiceAccount:     core.Env("FCM_SERVICE_ACCOUNT_JSON", ""),
		FCMServiceAccountFile: core.Env("FCM_SERVICE_ACCOUNT_FILE", ""),
		MockPaymentSecret:     core.Env("MOCK_PAYMENT_SECRET", "mock-secret"),
		ServiceToken:          core.Env("CORE_SERVICE_TOKEN", ""),
		FoodBaseURL:           core.Env("FOOD_BASE_URL", ""),
		FoodCallbackToken:     core.Env("FOOD_CALLBACK_TOKEN", ""),
		VTCBaseURL:            core.Env("VTC_BASE_URL", ""),
		VTCCallbackToken:      core.Env("VTC_CALLBACK_TOKEN", ""),
		OTPSender:             core.Env("OTP_SENDER", "echo"),
		CountryIPLookupURL:    core.Env("COUNTRY_IP_LOOKUP_URL", country.DefaultIPLookupURL),
		CountryIPLookupField:  core.Env("COUNTRY_IP_LOOKUP_FIELD", country.DefaultIPLookupField),
	}

	if cfg.FinanceIntegrityInterval, err = core.Duration("FINANCE_INTEGRITY_INTERVAL", 15*time.Minute); err != nil {
		return nil, err
	}
	if cfg.OTPTTL, err = core.Duration("OTP_TTL", 5*time.Minute); err != nil {
		return nil, err
	}
	if cfg.OTPResend, err = core.Duration("OTP_RESEND", time.Minute); err != nil {
		return nil, err
	}
	if cfg.OTPMaxAttempts, err = core.Int("OTP_MAX_ATTEMPTS", 5); err != nil {
		return nil, err
	}
	if cfg.OTPMaxPerHour, err = core.Int("OTP_MAX_PER_HOUR", 5); err != nil {
		return nil, err
	}
	switch cfg.OTPSender {
	case "echo", "whatsapp", "sms":
	default:
		return nil, fmt.Errorf("config: invalid OTP_SENDER %q (want echo|whatsapp|sms)", cfg.OTPSender)
	}
	// La mise en service du journal : le 21 septembre 2026. Réglable pour
	// une base qui aurait été reprise plus tard.
	cfg.FinanceJournalSince = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if raw := core.Env("FINANCE_JOURNAL_SINCE", ""); raw != "" {
		if cfg.FinanceJournalSince, err = time.Parse("2006-01-02", raw); err != nil {
			return nil, fmt.Errorf("config: FINANCE_JOURNAL_SINCE must be YYYY-MM-DD")
		}
	}
	switch cfg.Env {
	case "dev", "staging", "prod":
	default:
		return nil, fmt.Errorf("config: invalid ENV %q (want dev|staging|prod)", cfg.Env)
	}
	// ⚠️ Le secret JWT est le MÊME que celui des verticales : c'est ce qui
	// permet à chacune de vérifier un jeton localement, sans appeler le socle.
	// Sans lui, le socle émettrait des jetons que personne ne saurait lire.
	if cfg.JWTSecret == "" && cfg.IsProd() {
		return nil, fmt.Errorf("config: JWT_SECRET is required in production")
	}
	return cfg, nil
}
