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
	"fmt"
	"github.com/hibiken/asynq"
	"github.com/kgtech-org/dira-core-api/pkg/jobs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/kgtech-org/dira-core-api/api"
	"github.com/kgtech-org/dira-core-api/internal/callback"
	"github.com/kgtech-org/dira-core-api/internal/config"
	"github.com/kgtech-org/dira-core-api/internal/fleet"
	"github.com/kgtech-org/dira-core-api/internal/indexes"
	"github.com/kgtech-org/dira-core-api/internal/notify"
	"github.com/kgtech-org/dira-core-api/internal/payment"
	"github.com/kgtech-org/dira-core-api/internal/rating"
	"github.com/kgtech-org/dira-core-api/internal/serviceapi"
	"github.com/kgtech-org/dira-core-api/internal/staff"
	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/internal/user"
	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
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

	// LA FILE. Une seule tâche y passe — l'annonce d'un paiement abouti à sa
	// verticale — et c'est la seule qui la mérite : les vingt-trois autres
	// échanges entre services sont des commandes qui attendent une réponse.
	//
	// ⚠️ Le CONSOMMATEUR tourne DANS ce processus, pas dans un binaire à part.
	// Un conteneur de plus sur un seul serveur, pour une tâche qui consiste à
	// faire un appel HTTP, coûterait plus à exploiter qu'il ne rapporte. Le
	// jour où le volume le justifie, l'extraire est mécanique : le
	// gestionnaire est déjà un type autonome.
	asynqRedis := asynq.RedisClientOpt{
		Addr: redisOpts.Addr, Password: redisOpts.Password, DB: redisOpts.DB,
	}
	asynqClient := asynq.NewClient(asynqRedis)
	defer func() { _ = asynqClient.Close() }()

	translator, err := i18n.New(cfg.LocalesPath)
	if err != nil {
		return err
	}
	httpx.SetTranslator(translator)

	tokens := auth.NewManager(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	// ⚠️ LA PORTÉE DU STAFF EST VÉRIFIÉE UNE FOIS, ICI — comme dans chaque
	// verticale. Sans ce garde, un administrateur borné à la livraison lisait
	// quand même les comptes, les portefeuilles et l'équipe du socle : la
	// portée « core » ne bornait rien, puisque personne ne la demandait.
	//
	// Un non-administrateur passe : la portée est une notion de STAFF, et ce
	// service sert aussi la connexion, le profil et le portefeuille de tous
	// les clients.
	//
	// ⚠️ SEULEMENT sous `/admin/` : `/me`, `/wallet`, `/me/notifications` sont
	// le profil de la personne connectée, et un administrateur borné à la
	// livraison doit pouvoir voir le sien. Un garde global l'aurait déconnecté
	// de la console à la première lecture de son propre nom.
	authMW := chain(middleware.Auth(tokens),
		onlyUnder("/api/v1/admin/", middleware.RequireScope(auth.ScopeCore)))

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
	paymentRepo := payment.NewRepository(mongo)
	paymentSvc := payment.NewService(paymentRepo,
		map[string]payment.PaymentProvider{"mock": payment.NewMockProvider(cfg.MockPaymentSecret)}, nil)

	tokenRepo := token.NewRepository(mongo)
	tokenSvc := token.NewService(tokenRepo, nil, paymentSvc, nil, nil,
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

	// `OnOrderPaid` PRÉVIENT LA LIVRAISON. Le socle ne sait pas ce qu'est une
	// commande : il sait qu'un `ref_id` de type « order » vient d'être payé,
	// et c'est la verticale qui en tire les conséquences.
	//
	// ⚠️ L'erreur REMONTE jusqu'au webhook. Répondre « reçu » au prestataire
	// sans avoir prévenu la livraison laisserait une commande payée et jamais
	// confirmée — et le prestataire, ayant reçu un accusé, ne réessaierait
	// pas. Un échec ici lui fait retenter ; c'est le comportement voulu.
	// ⚠️ Le jeton de RAPPEL, pas celui de service. Le socle PRÉSENTE celui-ci
	// et EXIGE l'autre : réutiliser le même ferait qu'un secret volé chez la
	// livraison ouvrirait tous les portefeuilles du socle.
	verticals := callback.NewRegistry(map[string]*callback.Client{
		payment.PurposeOrder: callback.New(cfg.FoodBaseURL, cfg.FoodCallbackToken),
		payment.PurposeRide:  callback.New(cfg.VTCBaseURL, cfg.VTCCallbackToken),
	})
	if len(verticals.Purposes()) == 0 {
		logger.Error("no vertical configured: no mobile-money payment can ever be confirmed",
			"hint", "set FOOD_BASE_URL / VTC_BASE_URL and their callback tokens")
	} else {
		logger.Info("payment confirmations routed", "purposes", verticals.Purposes())
	}
	// ⚠️ LE RAPPEL PART EN FILE, il n'est plus émis en ligne.
	//
	// Avant, le webhook du prestataire de paiement attendait la réponse de la
	// verticale. Pendant un redéploiement de la livraison, le paiement d'un
	// client ÉCHOUAIT — on faisait porter à l'acheteur la latence de nos mises
	// en production, et c'est au prestataire qu'il revenait de réessayer.
	//
	// La mise en file répond en quelques millisecondes ; les reprises sont
	// exponentielles et durables, et la file morte garde ce qui n'est jamais
	// passé.
	//
	// ⚠️ L'ÉCHEC DE MISE EN FILE RESTE FATAL au webhook, et il le faut : le
	// paiement vient d'être marqué « abouti », et répondre « reçu » sans avoir
	// enfilé le rappel laisserait la commande payée et jamais confirmée. Le
	// prestataire réessaie, et le socle ré-enfile — voir `RefPaidDuplicate`
	// ci-dessous, sans quoi la seconde tentative se croirait en doublon et ne
	// rappellerait personne.
	paymentSvc.OnRefPaid = func(ctx context.Context, purpose, refID, paymentID string) error {
		return enqueueRefPaid(ctx, asynqClient, purpose, refID, paymentID)
	}
	// ⚠️ Le DOUBLON ré-enfile aussi.
	//
	// Sans cela, un échec de mise en file serait définitif : le paiement est
	// déjà « abouti », la reprise du prestataire tomberait sur la branche
	// « doublon », et personne ne serait jamais prévenu. C'est le rappel qui
	// est idempotent côté verticale — une commande déjà payée y poursuit vers
	// la livraison — donc en enfiler un de trop ne coûte rien, et en oublier
	// un coûte une commande perdue.
	paymentSvc.OnRefPaidDuplicate = paymentSvc.OnRefPaid

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
	} else if cfg.FCMServiceAccountFile == "" && cfg.FCMServiceAccount == "" {
		// Uniquement quand AUCUNE source n'est configurée. Une clé configurée
		// mais illisible a déjà produit une erreur juste au-dessus, qui dit
		// laquelle et pourquoi ; ajouter « aucune » ensuite contredit ce
		// message et envoie vérifier une variable qu'on a pourtant renseignée.
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

	// ⚠️ LE SOCLE N'AUDITAIT RIEN. Plusieurs de ses modules déclaraient une
	// interface `Auditor` — `token`, `rating` — et aucun enregistreur n'était
	// branché : les gestes sensibles du service qui tient l'argent et les
	// comptes ne laissaient aucune trace. Une interface prévue et jamais
	// câblée se lit, à la relecture, comme une garantie qui existe.
	auditRec := audit.NewRecorder(mongo.DB)

	fleetSvc := fleet.NewService(fleet.NewRepository(mongo))
	fleetSvc.SetAuditor(auditRec)

	// LE STAFF : les employés de Dira, leur fonction et leur PÉRIMÈTRE.
	//
	// ⚠️ Le périmètre n'est pas décoratif : il est inscrit dans le jeton à la
	// connexion et vérifié par `middleware.RequireScope` dans chaque
	// verticale. Un « périmètre » affiché sur une fiche mais qu'aucune route
	// ne contrôle donnerait à l'exploitation la certitude d'avoir restreint
	// quelqu'un qui ne l'est pas.
	staffSvc := staff.NewService(staff.NewRepository(mongo), staff.FromAccounts{Reader: userAccounts{svc: userSvc}})
	staffSvc.SetAuditor(auditRec)
	// ⚠️ Réglé APRÈS construction, pour casser le cycle : le staff a besoin
	// des comptes, et les comptes ont besoin des portées.
	userSvc.SetStaffScopes(staffSvc)

	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		user.NewHandler(userSvc).Mount(r, authMW)
		notify.NewHandler(notifySvc).Mount(r, authMW)
		// LE PORTEFEUILLE : `/wallet` pour la personne, `/admin/wallets/...`
		// pour l'exploitation. Il n'était monté NULLE PART — ni ici, ni dans
		// les verticales, qui venaient de s'en séparer. Un client ne pouvait
		// plus lire son solde, un livreur plus recharger, et la console plus
		// créditer personne.
		//
		// ⚠️ La PROPULSION d'un plat n'est PAS montée : elle débite un
		// portefeuille du socle mais porte sur le catalogue d'une verticale,
		// que le socle ne connaît pas. `MountCatalogueSpending` ne s'allume
		// que si les collaborateurs sont branchés — ils ne le sont pas, et des
		// routes qui échouent seraient pires que des routes absentes.
		tokenHandler := token.NewHandler(tokenSvc)
		tokenHandler.Mount(r, authMW)
		tokenHandler.MountCatalogueSpending(r, authMW)
		// ⚠️ La confirmation manuelle d'un encaissement n'est ouverte qu'HORS
		// production : simuler l'arrivée d'un paiement est un pouvoir qui n'a
		// rien à faire sur un service qui manipule de l'argent réel.
		payment.NewHandler(paymentSvc).Mount(r, authMW, !cfg.IsProd())
		// Les notes : lecture publique, écriture RÉSERVÉE aux services. Le
		// socle ne sait pas si une commande est livrée — la verticale valide,
		// puis dépose.
		rating.NewHandler(ratingSvc).Mount(r, middleware.Service(cfg.ServiceToken))
		// LES FLOTTES PRIVÉES — les sociétés qui possèdent des véhicules
		// conduits par d'autres. Au socle parce qu'une même société possède
		// des motos qui livrent ET des voitures qui font des courses : la
		// loger dans une verticale aurait obligé l'autre à lire la base de sa
		// voisine pour afficher un nom de propriétaire.
		//
		// Les VÉHICULES, eux, restent dans leur verticale : une moto de
		// livraison et une berline VTC n'ont ni les mêmes champs ni les mêmes
		// lecteurs.
		staff.NewHandler(staffSvc).Mount(r, authMW)
		fleetHandler := fleet.NewHandler(fleetSvc)
		fleetHandler.Mount(r, authMW)
		fleetHandler.MountService(r, middleware.Service(cfg.ServiceToken))
		// ⚠️ LA SURFACE DE SERVICE DU SOCLE. Ces routes portent les pouvoirs
		// d'une verticale — débiter un portefeuille, ouvrir un compte — et
		// n'ont aucun sens pour une personne. Elles sont gardées par le secret
		// partagé, et rassemblées en un seul endroit pour être auditables.
		serviceapi.NewHandler(userSvc, tokenSvc, notifySvc, paymentSvc,
			backOffice{tokens: tokenRepo, payments: paymentRepo}).
			Mount(r, middleware.Service(cfg.ServiceToken))
	})

	// Le consommateur démarre avec l'API et s'arrête avec elle.
	//
	// ⚠️ Une panne de la file ne doit PAS empêcher l'API de servir : elle
	// tient les comptes et les portefeuilles de toute la plateforme. On
	// journalise, fort, et on continue — un socle qui refuse de démarrer parce
	// qu'une file d'annonces est indisponible ferait tomber les deux métiers
	// pour un rappel différé.
	asynqSrv := asynq.NewServer(asynqRedis, asynq.Config{
		Concurrency: 4,
		Logger:      asynqSlog{},
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(jobs.TypeRefPaid, callback.NewWorker(verticals).HandleRefPaid)
	if err := asynqSrv.Start(mux); err != nil {
		logger.Error("core: payment callbacks will NOT be delivered — job queue unavailable",
			"error", err, "hint", "check REDIS_URI")
	} else {
		defer asynqSrv.Shutdown()
		logger.Info("core: payment callback worker started", "concurrency", 4)
	}

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
			// Le cas de très loin le plus fréquent est un droit de lecture : le
			// conteneur tourne sous l'utilisateur `app` (UID 10001), pas root,
			// et une clé déposée en 600 root:root lui est fermée. Sans cette
			// indication on cherche du côté de Firebase, où il n'y a rien.
			logger.Error("FCM: compte de service illisible",
				"path", cfg.FCMServiceAccountFile,
				"conseil", "le conteneur lit sous l'UID 10001 : chown 10001:10001 sur le fichier hôte",
				"error", err)
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

// userAccounts adapte le service des comptes à ce que le module de staff lit.
//
// ⚠️ La traduction vit ICI, au câblage, et pas dans l'un des deux modules :
// c'est ce qui permet à `internal/staff` de tester ses règles de portée sans
// annuaire, et à `internal/user` d'ignorer qu'un staff existe.
type userAccounts struct{ svc *user.Service }

func (a userAccounts) AccountByID(ctx context.Context, id string) (*staff.AccountRow, error) {
	row, err := a.svc.AccountByID(ctx, id)
	if err != nil || row == nil {
		return nil, err
	}
	out := staffRow(*row)
	return &out, nil
}

func (a userAccounts) AccountsByIDs(ctx context.Context, ids []string) ([]staff.AccountRow, error) {
	rows, err := a.svc.AccountsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]staff.AccountRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, staffRow(r))
	}
	return out, nil
}

func staffRow(r user.AccountRow) staff.AccountRow {
	return staff.AccountRow{
		ID: r.ID, Role: r.Role, Name: r.Name, Phone: r.Phone, Email: r.Email, Status: r.Status,
	}
}

// enqueueRefPaid met en file l'annonce d'un paiement abouti.
//
// ⚠️ La ROUTE (quelle verticale prévenir) n'est PAS résolue ici mais dans le
// consommateur. Sinon une verticale mal configurée ferait échouer le webhook
// du prestataire — ce que la file existe précisément pour éviter. Le
// consommateur, lui, journalise et abandonne sans reprise : une configuration
// absente le restera au vingtième essai.
func enqueueRefPaid(ctx context.Context, client *asynq.Client, purpose, refID, paymentID string) error {
	if client == nil {
		return fmt.Errorf("callback: no job queue configured, cannot notify the vertical")
	}
	task, err := jobs.NewTask(jobs.TypeRefPaid, jobs.RefPaidPayload{
		Purpose: purpose, RefID: refID, PaymentID: paymentID,
	})
	if err != nil {
		return err
	}
	// ⚠️ L'identifiant de tâche est DÉRIVÉ du paiement. Deux webhooks du
	// prestataire pour le même paiement produisent la même tâche, et Asynq
	// refuse la seconde : la verticale n'est prévenue qu'une fois dans le cas
	// courant. Elle reste idempotente pour le cas où la reprise dépasse la
	// fenêtre d'unicité — une file « au moins une fois » ne promet rien de
	// plus.
	if _, err := client.EnqueueContext(ctx, task,
		asynq.TaskID("ref-paid:"+paymentID),
		asynq.MaxRetry(20),
		asynq.Retention(retainFailedCallbacks),
	); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			slog.InfoContext(ctx, "callback: this payment is already queued for its vertical",
				"payment_id", paymentID, "purpose", purpose)
			return nil
		}
		return fmt.Errorf("callback: enqueue ref-paid: %w", err)
	}
	return nil
}

// retainFailedCallbacks garde les tâches abouties assez longtemps pour qu'on
// puisse répondre à « ce paiement a-t-il bien été annoncé ? » le lendemain.
const retainFailedCallbacks = 72 * time.Hour

// asynqSlog fait passer les journaux de la file par `log/slog`, comme tout le
// reste du service.
//
// Sans lui, la file écrit dans son propre format : deux formats de journal
// dans un même flux, et un agrégateur qui n'en indexe qu'un.
type asynqSlog struct{}

func (asynqSlog) Debug(args ...any) { slog.Debug(fmt.Sprint(args...)) }
func (asynqSlog) Info(args ...any)  { slog.Info(fmt.Sprint(args...)) }
func (asynqSlog) Warn(args ...any)  { slog.Warn(fmt.Sprint(args...)) }
func (asynqSlog) Error(args ...any) { slog.Error(fmt.Sprint(args...)) }
func (asynqSlog) Fatal(args ...any) { slog.Error(fmt.Sprint(args...)) }

// chain compose des middlewares dans l'ordre où on les lit.
//
// `chain(a, b)` applique `a` PUIS `b` — l'ordre compte : la vérification de
// portée lit ce que l'authentification a posé dans le contexte, et l'inverse
// refuserait tout le monde.
func chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}

// onlyUnder n'applique un middleware qu'aux chemins portant le préfixe.
//
// Le préfixe est ABSOLU (`/api/v1/admin/`) parce que ce middleware est posé
// sur le groupe authentifié, où `r.URL.Path` est complet. Un préfixe relatif
// n'aurait jamais correspondu, et le garde serait resté décoratif — sans
// qu'aucun test ne le dise, puisque les tests montent les handlers sans le
// préfixe de version.
func onlyUnder(prefix string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		guarded := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, prefix) {
				guarded.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
