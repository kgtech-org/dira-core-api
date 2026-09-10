// Package config reads configuration from the environment.
//
// Il porte DEUX choses, et rien d'autre : les lecteurs typés, et les réglages
// qu'un service de la plateforme a forcément — port, environnement, bases,
// secret JWT, stockage d'objets.
//
// ⚠️ Ce qu'il ne porte PAS : les réglages MÉTIER. La grille tarifaire d'une
// livraison, les jetons WhatsApp, les taux de commission d'une course
// appartiennent à leur service. Les rassembler ici ferait d'une structure
// partagée le dépotoir de trois produits, et chaque service embarquerait des
// variables qu'il n'utilise pas — donc des variables que personne ne sait plus
// s'il faut renseigner.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Base is what EVERY service of the platform needs.
//
// À EMBARQUER dans la configuration d'un service, jamais à utiliser seule :
//
//	type Config struct {
//	    config.Base
//	    DeliveryFeeBaseXOF int
//	}
type Base struct {
	Port          string
	Env           string // dev | staging | prod
	MongoURI      string
	MongoDB       string
	RedisURI      string
	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
	LocalesPath   string
	RateLimitRPM  int // requêtes par minute, par IP ou par compte

	// Stockage d'objets (MinIO en développement) pour les fichiers envoyés.
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioUseSSL    bool
	MinioBucket    string
	MinioPublicURL string
}

// IsProd dit si le service tourne en production.
func (b *Base) IsProd() bool { return b.Env == "prod" }

// LoadBase reads the shared settings.
//
// ⚠️ Le SECRET JWT n'est pas validé ici, et c'est délibéré : chaque service
// décide s'il peut démarrer sans. Un service de développement le tolère, un
// service qui vérifie des jetons ne le doit pas — et c'est au service de le
// dire, pas au socle de le supposer.
//
// `defaultDB` et `defaultBucket` sont passés par l'appelant : deux services ne
// partagent ni base ni compartiment par défaut, et un défaut commun ferait
// écrire le VTC dans les collections de la livraison au premier oubli de
// variable d'environnement.
func LoadBase(defaultDB, defaultBucket string) (Base, error) {
	b := Base{
		Port:        Env("PORT", "8080"),
		Env:         Env("ENV", "dev"),
		MongoURI:    Env("MONGO_URI", "mongodb://localhost:27017/?replicaSet=rs0"),
		MongoDB:     Env("MONGO_DB", defaultDB),
		RedisURI:    Env("REDIS_URI", "redis://localhost:6379/0"),
		JWTSecret:   Env("JWT_SECRET", ""),
		LocalesPath: Env("LOCALES_PATH", "locales"),

		MinioEndpoint:  Env("MINIO_ENDPOINT", "localhost:9000"),
		MinioAccessKey: Env("MINIO_ACCESS_KEY", ""),
		MinioSecretKey: Env("MINIO_SECRET_KEY", ""),
		MinioUseSSL:    os.Getenv("MINIO_USE_SSL") == "true",
		MinioBucket:    Env("MINIO_BUCKET", defaultBucket),
		MinioPublicURL: Env("MINIO_PUBLIC_URL", "http://localhost:9000"),
	}
	var err error
	if b.JWTAccessTTL, err = Duration("JWT_ACCESS_TTL", 15*time.Minute); err != nil {
		return b, err
	}
	if b.JWTRefreshTTL, err = Duration("JWT_REFRESH_TTL", 30*24*time.Hour); err != nil {
		return b, err
	}
	if b.RateLimitRPM, err = Int("RATE_LIMIT_RPM", 120); err != nil {
		return b, err
	}
	return b, nil
}

// Env reads a string, falling back to def when unset or empty.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Duration reads a Go duration ("15m", "720h").
//
// Une valeur illisible est une ERREUR, pas un repli silencieux sur le défaut :
// un service qui démarre avec un jeton valable quinze minutes au lieu de trente
// jours, parce qu'on a écrit « 30d » que Go ne comprend pas, déconnecte tout le
// monde sans que rien ne le dise.
func Duration(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s is not a duration: %w", key, err)
	}
	return d, nil
}

// Int reads an integer, erroring rather than falling back on a bad value.
func Int(key string, def int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s is not an integer: %w", key, err)
	}
	return n, nil
}

// Bool reads a boolean; anything but "true" is false.
func Bool(key string) bool { return os.Getenv(key) == "true" }
