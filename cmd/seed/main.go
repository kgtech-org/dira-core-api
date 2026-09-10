// Commande `seed` — amorce le compte d'ADMINISTRATION du socle.
//
// ⚠️ RIEN D'AUTRE. Pas de jeu de démonstration, pas d'enseignes, pas de
// courses : le socle ne sait pas ce qu'est un restaurant ni un trajet. Chaque
// verticale sème son propre jeu, et demande au socle d'ouvrir les comptes dont
// elle a besoin par la surface de service (`/internal/accounts/ensure`).
//
// Ce binaire existe pour un cas précis, et il est réel : un socle déployé SEUL
// — sans livraison, sans courses — n'a personne pour se connecter. Sans lui, il
// faudrait semer une verticale entière pour obtenir un administrateur, ou
// écrire à la main dans la base.
//
// ⚠️ Il écrit DIRECTEMENT dans la base du socle, et c'est le seul endroit où
// c'est légitime : c'est sa base. Une verticale, elle, passe par la surface de
// service — écrire dans les collections d'un autre service ferait deux
// écrivains sur une même table, et le second ignorerait toujours quelque chose
// que le premier garantit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kgtech-org/dira-core-api/internal/config"
	"github.com/kgtech-org/dira-core-api/internal/indexes"
	"github.com/kgtech-org/dira-core-api/internal/user"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/db"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("seed: fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	// ⚠️ `--reset` VIDE les comptes du socle : tous les utilisateurs de la
	// plateforme, leurs jetons de session, leurs portefeuilles. Les commandes
	// et les courses des verticales, elles, resteraient — et pointeraient vers
	// des comptes disparus. Ne s'utilise qu'en développement, et le service le
	// dit avant de le faire.
	reset := flag.Bool("reset", false, "⚠️ DROPS the core's accounts, sessions and wallets before seeding")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.IsProd() && *reset {
		return errors.New("seed: --reset is refused in production: it would delete every account of the platform")
	}

	adminEmail := envOr("SEED_ADMIN_EMAIL", "dev@dira.llc")
	adminPhone := envOr("SEED_ADMIN_PHONE", "+22890000100")
	// Le mot de passe n'a AUCUN défaut. Un défaut publiable ferait d'un
	// environnement oublié une porte ouverte, et personne ne s'en apercevrait
	// puisque tout fonctionnerait.
	adminPassword := strings.TrimSpace(os.Getenv("SEED_ADMIN_PASSWORD"))
	if adminPassword == "" {
		return errors.New("seed: SEED_ADMIN_PASSWORD is required (no default: a publishable one would be an open door)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	mongo, err := db.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return fmt.Errorf("seed: mongo: %w", err)
	}
	defer func() { _ = mongo.Close(context.Background()) }()

	if *reset {
		logger.Warn("seed: DROPPING the core's accounts — every session of the platform is invalidated")
		for _, name := range []string{
			user.CollectionUsers, user.CollectionRefreshTokens, user.CollectionAddresses,
		} {
			if err := mongo.DB.Collection(name).Drop(ctx); err != nil {
				return fmt.Errorf("seed: drop %s: %w", name, err)
			}
		}
	}

	// Les index AVANT le compte : sans l'unicité du téléphone, deux
	// provisionnements concurrents créeraient deux administrateurs, et le
	// second se connecterait avec un mot de passe que personne n'attend.
	indexes.Ensure(ctx, mongo.DB, logger)

	svc := user.NewService(user.NewRepository(mongo), auth.NewManager(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL), nil)

	// Idempotent : un téléphone déjà pris rend le compte existant. Rejouer ne
	// crée donc pas de doublon, et ne réécrit PAS le mot de passe — un seed
	// relancé ne doit pas rendre un administrateur inaccessible à celui qui
	// l'utilisait.
	id, err := svc.EnsureAccount(ctx, auth.RoleAdmin, adminPhone, "Dira Ops", adminPassword)
	if err != nil {
		return fmt.Errorf("seed: ensure admin: %w", err)
	}

	logger.Info("seed: admin ready",
		"user_id", id, "phone", adminPhone, "email", adminEmail,
		"password_source", "SEED_ADMIN_PASSWORD",
		"db", cfg.MongoDB)
	logger.Info("seed: done — les verticales sèment leur propre jeu de démonstration")
	return nil
}

// envOr returns the environment value for key, or def when unset/blank.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
