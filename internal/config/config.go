// Package config loads dira-core-api's configuration.
//
// Les réglages communs à toute la plateforme viennent de `pkg/config.Base`.
// Ce qui suit appartient au SOCLE, et à lui seul.
package config

import (
	"fmt"

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
}

// Load reads the environment, applies defaults and validates.
func Load() (*Config, error) {
	base, err := core.LoadBase("dira_core", "dira-core")
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
