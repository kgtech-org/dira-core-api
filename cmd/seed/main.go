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
	"github.com/kgtech-org/dira-core-api/internal/staff"
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
	// L'E-MAIL est passé : la console se connecte par e-mail, et un
	// administrateur sans adresse ne peut pas l'ouvrir.
	id, err := svc.EnsureAccount(ctx, auth.RoleAdmin, adminPhone, "Dira Ops", adminEmail, adminPassword)
	if err != nil {
		return fmt.Errorf("seed: ensure admin: %w", err)
	}

	// ⚠️ LA FICHE DE STAFF EST INDISPENSABLE, pas décorative.
	//
	// Une portée vide n'accorde rien : sans fiche, ce compte se connecterait
	// et se verrait refuser CHAQUE route d'administration, sans qu'aucun
	// message n'explique pourquoi. Un provisionnement qui produit un
	// administrateur incapable d'administrer est un provisionnement raté.
	//
	// Toutes les portées : c'est le compte de démarrage, celui par lequel on
	// crée les autres. Les restreindre reviendrait à livrer une plateforme
	// dont personne ne peut ouvrir une partie.
	staffSvc := staff.NewService(staff.NewRepository(mongo), staff.FromAccounts{Reader: seedAccounts{svc: svc}})
	member, err := staffSvc.EnsureMember(ctx, id, staff.FunctionAdmin, "Administrateur de la plateforme", auth.AllScopes)
	if err != nil {
		return fmt.Errorf("seed: ensure staff record: %w", err)
	}

	logger.Info("seed: admin ready",
		"user_id", id, "phone", adminPhone, "email", adminEmail,
		"password_source", "SEED_ADMIN_PASSWORD",
		"staff_id", member.ID, "scopes", member.Scopes,
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

// seedAccounts adapte le service des comptes à ce que le staff lit.
//
// Le même adaptateur qu'au démarrage de l'API : la traduction vit au câblage
// pour que `internal/staff` reste testable sans annuaire.
type seedAccounts struct{ svc *user.Service }

func (a seedAccounts) AccountByID(ctx context.Context, id string) (*staff.AccountRow, error) {
	row, err := a.svc.AccountByID(ctx, id)
	if err != nil || row == nil {
		return nil, err
	}
	return &staff.AccountRow{
		ID: row.ID, Role: row.Role, Name: row.Name,
		Phone: row.Phone, Email: row.Email, Status: row.Status,
	}, nil
}

func (a seedAccounts) AccountsByIDs(ctx context.Context, ids []string) ([]staff.AccountRow, error) {
	rows, err := a.svc.AccountsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]staff.AccountRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, staff.AccountRow{
			ID: r.ID, Role: r.Role, Name: r.Name,
			Phone: r.Phone, Email: r.Email, Status: r.Status,
		})
	}
	return out, nil
}
