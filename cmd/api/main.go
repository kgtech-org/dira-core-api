// Service SOCLE de la plateforme Dira.
//
// Il porte ce qui n'appartient à aucune verticale : identité, portefeuilles,
// paiements, notifications, véhicules et notes. `dira-food-api` (livraison) et
// `dira-vtc-api` (courses) l'appellent, et ne réimplémentent rien de tout ça.
//
// ⚠️ Le secret JWT est le MÊME que celui des verticales. C'est ce qui leur
// permet de vérifier un jeton LOCALEMENT, sans appeler le socle : une
// vérification par requête ferait de ce service le point de panne unique de
// toute la plateforme.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/kgtech-org/dira-core-api/api"
	"github.com/kgtech-org/dira-core-api/internal/config"
	"github.com/kgtech-org/dira-core-api/internal/indexes"
	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/internal/user"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/db"
	"github.com/kgtech-org/dira-core-api/pkg/docs"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/i18n"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("core: fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	mongo, err := db.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return err
	}
	defer func() { _ = mongo.Close(context.Background()) }()

	// Les index sont posés au DÉMARRAGE : un index qu'il faut penser à créer à
	// la main finit toujours par manquer quelque part. L'opération est
	// idempotente et n'interrompt pas le démarrage en cas d'échec.
	indexes.Ensure(ctx, mongo.DB, logger)

	redisOpts, err := redis.ParseURL(cfg.RedisURI)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()

	translator, err := i18n.New(cfg.LocalesPath)
	if err != nil {
		return err
	}
	httpx.SetTranslator(translator)

	tokens := auth.NewManager(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	authMW := middleware.Auth(tokens)

	// Le portefeuille de jetons est créé À L'INSCRIPTION d'un livreur — c'est
	// pourquoi l'identité connaît les jetons, et non l'inverse. `CreateWallet`
	// est idempotent : une inscription rejouée ne crée pas deux portefeuilles.
	// ⚠️ Le module des jetons n'est PAS entièrement du socle, et ce câblage le
	// montre : `Purchase` a besoin du paiement (qui arrive au socle), mais
	// `BoostDish` et les options de boutique ont besoin du CATALOGUE et de la
	// PROPRIÉTÉ D'UNE ENSEIGNE — deux notions de la livraison.
	//
	// Ces collaborateurs sont donc nil ICI, et les routes qui les utilisent ne
	// sont pas montées. Le découpage — le grand livre au socle, la propulsion
	// à la livraison — reste à faire ; le laisser dans cet état sans le dire
	// ferait croire que le module a trouvé sa place.
	tokenSvc := token.NewService(token.NewRepository(mongo), nil, nil, nil, nil,
		token.DefaultTokenPriceXOF, token.DefaultBoostCost, nil)
	userSvc := user.NewService(user.NewRepository(mongo), tokens, tokenSvc)

	router := chi.NewRouter()
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apperr.NotFound("not_found", "resource not found"))
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, apperr.NotFound("not_found", "resource not found"))
	})
	router.Use(middleware.RequestID)
	router.Use(middleware.Logger(logger))
	router.Use(middleware.Recoverer)
	router.Use(middleware.Language)
	router.Use(middleware.RateLimit(rdb, cfg.RateLimitRPM))

	docs.Mount(router, "Dira Core API — Documentation", api.OpenAPISpec)

	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		user.NewHandler(userSvc).Mount(r, authMW)
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("core listening", "port", cfg.Port, "env", cfg.Env)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
