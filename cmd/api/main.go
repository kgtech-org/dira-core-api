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
	"github.com/kgtech-org/dira-core-api/internal/notify"
	"github.com/kgtech-org/dira-core-api/internal/payment"
	"github.com/kgtech-org/dira-core-api/internal/rating"
	"github.com/kgtech-org/dira-core-api/internal/serviceapi"
	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/internal/user"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/db"
	"github.com/kgtech-org/dira-core-api/pkg/docs"
	"github.com/kgtech-org/dira-core-api/pkg/fcm"
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
	paymentSvc := payment.NewService(payment.NewRepository(mongo),
		map[string]payment.PaymentProvider{"mock": payment.NewMockProvider(cfg.MockPaymentSecret)}, nil)

	tokenSvc := token.NewService(token.NewRepository(mongo), nil, paymentSvc, nil, nil,
		token.DefaultTokenPriceXOF, token.DefaultBoostCost, nil)
	userSvc := user.NewService(user.NewRepository(mongo), tokens, tokenSvc)

	// L'opérateur habituel d'un client vient des COMPTES : le proposer d'office
	// évite de redemander à chaque paiement lequel il utilise.
	paymentSvc.SetPreferences(userSvc)

	// Les deux dénouements que le socle sait mener lui-même.
	//
	// ⚠️ Le crédit n'a lieu qu'à la CONFIRMATION du prestataire. Créditer sur
	// la réponse d'une initiation reviendrait à offrir l'argent : une
	// initiation dit qu'on a demandé à payer, pas qu'on a payé.
	paymentSvc.OnWalletToppedUp = func(ctx context.Context, userID string, amountXOF int, paymentID string) error {
		return tokenSvc.TopUp(ctx, userID, amountXOF, map[string]any{"payment_id": paymentID})
	}
	paymentSvc.OnTokensPurchased = func(ctx context.Context, walletOwnerID string, tokens int) error {
		return tokenSvc.Credit(ctx, walletOwnerID, tokens, "topup")
	}

	// ⚠️ `OnOrderPaid` reste NIL, et c'est le premier vrai manque de service à
	// service : confirmer le paiement d'une commande demande de prévenir la
	// LIVRAISON, qui seule sait ce qu'est une commande.
	//
	// Le module le journalise à voix haute plutôt que de l'oublier. Ce crochet
	// deviendra un rappel HTTP vers la verticale à l'étape C ; tant que
	// dira-food-api encaisse lui-même, rien n'est cassé — mais router les
	// paiements ici avant d'avoir posé ce rappel laisserait des commandes
	// payées et jamais confirmées.

	ratingSvc := rating.NewService(rating.NewRepository(mongo), nil)

	notifySvc := notify.NewService(notify.NewRepository(mongo))
	if sa := fcmServiceAccount(cfg, logger); sa != "" {
		if client, err := fcm.New(sa); err != nil {
			// Une clé illisible se corrige ; démarrer en la taisant ferait
			// chercher la panne du côté des téléphones.
			logger.Error("core: FCM service account unreadable, push notifications disabled", "error", err)
		} else {
			notifySvc.SetPusher(fcmPusher{client: client})
		}
	} else {
		logger.Warn("core: no FCM service account, push notifications disabled")
	}

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
		notify.NewHandler(notifySvc).Mount(r, authMW)
		// ⚠️ La confirmation manuelle d'un encaissement n'est ouverte qu'HORS
		// production : simuler l'arrivée d'un paiement est un pouvoir qui n'a
		// rien à faire sur un service qui manipule de l'argent réel.
		payment.NewHandler(paymentSvc).Mount(r, authMW, !cfg.IsProd())
		// Les notes : lecture publique, écriture RÉSERVÉE aux services. Le
		// socle ne sait pas si une commande est livrée — la verticale valide,
		// puis dépose.
		rating.NewHandler(ratingSvc).Mount(r, middleware.Service(cfg.ServiceToken))
		// ⚠️ LA SURFACE DE SERVICE DU SOCLE. Ces routes portent les pouvoirs
		// d'une verticale — débiter un portefeuille, ouvrir un compte — et
		// n'ont aucun sens pour une personne. Elles sont gardées par le secret
		// partagé, et rassemblées en un seul endroit pour être auditables.
		serviceapi.NewHandler(userSvc, tokenSvc, notifySvc, paymentSvc).
			Mount(r, middleware.Service(cfg.ServiceToken))
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

// fcmServiceAccount lit le compte de service Firebase, du fichier de
// préférence.
//
// Deux sources parce qu'une clé privée RSA tient mal dans une variable
// d'environnement : la plupart des déploiements montent un secret en fichier.
// Le FICHIER l'emporte quand les deux sont donnés — c'est la source la moins
// susceptible d'avoir été tronquée en chemin.
func fcmServiceAccount(cfg *config.Config, logger *slog.Logger) string {
	if cfg.FCMServiceAccountFile != "" {
		data, err := os.ReadFile(cfg.FCMServiceAccountFile)
		if err != nil {
			logger.Error("FCM: compte de service illisible", "path", cfg.FCMServiceAccountFile, "error", err)
			return ""
		}
		return string(data)
	}
	return cfg.FCMServiceAccount
}

// fcmPusher adapts the Firebase client to what the notify module expects.
//
// Le module ne connaît pas Firebase : il connaît « envoyer un titre et un corps
// à des jetons, et me dire lesquels sont morts ». Changer de fournisseur ne
// toucherait que cet adaptateur.
type fcmPusher struct{ client *fcm.Client }

func (p fcmPusher) Push(ctx context.Context, msgs []notify.PushMessage) ([]notify.PushResult, error) {
	out := make([]fcm.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, fcm.Message{Token: m.Token, Title: m.Title, Body: m.Body, Data: m.Data})
	}
	res, err := p.client.Send(ctx, out)
	if err != nil {
		return nil, err
	}
	results := make([]notify.PushResult, 0, len(res))
	for _, r := range res {
		results = append(results, notify.PushResult{
			Token: r.Token, Err: r.Err, Unregistered: r.Unregistered,
		})
	}
	return results, nil
}
