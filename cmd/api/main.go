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
	"github.com/kgtech-org/dira-core-api/internal/auditlog"
	"github.com/kgtech-org/dira-core-api/internal/callback"
	"github.com/kgtech-org/dira-core-api/internal/challenge"
	"github.com/kgtech-org/dira-core-api/internal/config"
	"github.com/kgtech-org/dira-core-api/internal/country"
	"github.com/kgtech-org/dira-core-api/internal/equipment"
	"github.com/kgtech-org/dira-core-api/internal/faults"
	"github.com/kgtech-org/dira-core-api/internal/finance"
	"github.com/kgtech-org/dira-core-api/internal/fleet"
	"github.com/kgtech-org/dira-core-api/internal/indexes"
	"github.com/kgtech-org/dira-core-api/internal/marker"
	"github.com/kgtech-org/dira-core-api/internal/notify"
	"github.com/kgtech-org/dira-core-api/internal/payment"
	"github.com/kgtech-org/dira-core-api/internal/promocode"
	"github.com/kgtech-org/dira-core-api/internal/rating"
	"github.com/kgtech-org/dira-core-api/internal/serviceapi"
	"github.com/kgtech-org/dira-core-api/internal/sos"
	"github.com/kgtech-org/dira-core-api/internal/staff"
	"github.com/kgtech-org/dira-core-api/internal/token"
	"github.com/kgtech-org/dira-core-api/internal/upload"
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
	"github.com/kgtech-org/dira-core-api/pkg/obs"
	"github.com/kgtech-org/dira-core-api/pkg/session"
	"github.com/kgtech-org/dira-core-api/pkg/storage"
)

// version est gravée à la compilation (`-X main.version=…`). ⚠️ Elle part
// dans chaque ligne de journal et dans `dira_build_info` : sans elle, devant
// une panne, on ne sait pas si l'on regarde le code qui tourne ou celui
// d'avant-hier.
var version = "dev"

func main() {
	logger := obs.Logger("core", version)
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

	// LE REGISTRE DES APPAREILS — quel téléphone détient la session d'un
	// chauffeur ou d'un livreur. Voir `pkg/session`.
	//
	// ⚠️ LES MÊMES OPTIONS, UN AUTRE INDEX, ET C'EST TOUT L'INTÉRÊT. Le suivi
	// doit lire ce registre : c'est le seul moyen de faire cesser un flux de
	// positions venu d'un téléphone chassé sans que `dira-tracking` appelle le
	// socle à chaque poignée de main — ce qui aurait fait du socle le point de
	// panne unique de la mise en ligne. En copiant les options déjà analysées,
	// l'hôte, le mot de passe et le TLS restent ceux de ce déploiement : il n'y
	// a pas une seconde variable d'environnement à tenir en accord dans deux
	// dépôts, donc pas de jour où elles désignent deux serveurs différents et
	// où la chasse cesse silencieusement de valoir.
	sessionOpts := *redisOpts
	sessionOpts.DB = cfg.SessionsRedisDB
	sessionsRdb := redis.NewClient(&sessionOpts)
	defer func() { _ = sessionsRdb.Close() }()
	sessions := session.New(sessionsRdb, cfg.JWTRefreshTTL)
	logger.Info("core: registre des appareils (un seul appareil par chauffeur)",
		"redis_db", cfg.SessionsRedisDB, "ttl", cfg.JWTRefreshTTL,
		"note", "dira-tracking doit lire la MÊME base, sinon un téléphone chassé continue de pousser des positions")

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

	// Le stockage des fichiers envoyés. Le socle reste debout sans lui —
	// l'envoi répond 503 `storage_unavailable` — et le dit FORT : la console
	// ne peut alors poser aucune image, et un avertissement discret dans un
	// journal a déjà laissé un staging entier sans envoi pendant des jours.
	media, err := storage.New(ctx, storage.Config{
		Endpoint: cfg.MinioEndpoint, AccessKey: cfg.MinioAccessKey, SecretKey: cfg.MinioSecretKey,
		UseSSL: cfg.MinioUseSSL, Bucket: cfg.MinioBucket, PublicBaseURL: cfg.MinioPublicURL,
	})
	if err != nil {
		logger.Error("core: object storage unavailable — every upload will answer 503",
			"error", err, "endpoint", cfg.MinioEndpoint, "bucket", cfg.MinioBucket,
			"hint", "MINIO_ENDPOINT / MINIO_ACCESS_KEY / MINIO_SECRET_KEY / MINIO_BUCKET")
		media = nil
	}

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
	// LA COUCHE PAYS. Les pays ouverts vivent en base et se règlent depuis
	// la console ; le middleware les lit par un cache. Monté GLOBALEMENT,
	// avec le vérificateur de jetons : le pays du compte s'impose à
	// l'en-tête même sur une route publique. Voir `middleware.Country`.
	countrySvc := country.NewService(country.NewRepository(mongo), cfg.CountryDefault)
	countrySvc.Start(ctx)
	countryMW := middleware.Country(countrySvc, tokens)

	// UN SEUL APPAREIL PAR CHAUFFEUR : `WithSessions` refuse le jeton d'un
	// téléphone chassé, avec un code NOMMÉ — `session_superseded` — pour que
	// l'écran puisse dire ce qui s'est passé au lieu d'afficher « erreur de
	// connexion ». Ne voit que les chauffeurs et les livreurs : les autres
	// comptes n'ont pas d'appareil dans leur jeton.
	// ⚠️ LA CADENCE PAR COMPTE SE POSE ICI, ET NULLE PART AILLEURS. Elle a besoin
	// du compte, donc de `Auth` avant elle ; la cadence GLOBALE, elle, s'applique
	// avant toute authentification et ne peut compter que par adresse. Les deux
	// étages sont nécessaires : sans celui-ci, tout le monde était compté par
	// adresse, et chez un opérateur mobile — où des milliers d'abonnés sortent
	// par une poignée d'adresses publiques — un opérateur entier plafonnait à
	// 120 requêtes par minute. La branche « par compte » de `clientKey` existait
	// depuis toujours, et l'ordre des middlewares la rendait inatteignable.
	authMW := chain(middleware.Auth(tokens, middleware.WithSessions(sessions)),
		middleware.RateLimitAccount(rdb, cfg.RateLimitRPM),
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
	// LA COMPTABILITÉ : chaque mouvement du grand livre des portefeuilles
	// écrit son écriture en partie double, dans la même transaction. La
	// facturation par pays (jetons ou commission, quand débiter le client)
	// et le balayage d'intégrité vivent au même endroit.
	financeRepo := finance.NewRepository(mongo)
	if err := financeRepo.EnsureIndexes(ctx); err != nil {
		logger.Warn("finance: indexes not ensured", "error", err)
	}
	tokenRepo.SetJournal(finance.NewJournal(financeRepo, token.DefaultTokenPriceXOF))
	userRepo := user.NewRepository(mongo)
	userSvc := user.NewService(userRepo, tokens, tokenSvc)
	userSvc.SetDefaultCountry(cfg.CountryDefault)
	// LE FOND DE CARTE voyage avec le jeton : réglé par pays dans la console,
	// servi à la connexion ET à chaque rafraîchissement. ⚠️ Sans la seconde, une
	// clé changée n'atteindrait un chauffeur resté connecté que trente jours
	// plus tard, et la rotation — seule raison de servir la clé depuis le
	// serveur — ne servirait à rien.
	userSvc.SetBasemaps(countrySvc)
	// CE QUE LE PAYS DÉCIDE pour les applications, réglé depuis la console
	// (`PUT /admin/countries/{code}/security`) : le VERROU — proposé, imposé
	// ou rien, servi avec le jeton et au rafraîchissement — et le NOMBRE
	// D'APPAREILS qu'un compte ordinaire peut tenir, au-delà duquel la
	// session la plus silencieuse est déconnectée.
	userSvc.SetCountryPolicies(countrySvc)
	// LA PORTE PAR CODE — un client s'inscrit avec son téléphone et six
	// chiffres, sans mot de passe à choisir ni à retrouver.
	//
	// ⚠️ LE POIVRE EST LE SECRET JWT, et ce n'est pas un raccourci : il ne
	// doit pas vivre dans la base qu'il protège (voir `OTPCode.Hash`), et le
	// socle n'a pas d'autre secret déjà déployé partout. Un secret dédié
	// (`OTP_PEPPER`) se branchera ici sans rien changer d'autre.
	userSvc.EnableOTP(otpSender(logger, cfg.OTPSender), cfg.JWTSecret, user.OTPPolicy{
		TTL:          cfg.OTPTTL,
		Resend:       cfg.OTPResend,
		MaxAttempts:  cfg.OTPMaxAttempts,
		MaxPerWindow: cfg.OTPMaxPerHour,
	})
	// La résolution « dans quel pays suis-je ? » aligne le compte ; le repli
	// par adresse IP passe par un fournisseur HTTP réglable, mis en cache.
	countrySvc.SetAccounts(userSvc)
	countrySvc.SetIPLookup(&country.HTTPLookup{URL: cfg.CountryIPLookupURL, Field: cfg.CountryIPLookupField, Cache: rdb})

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
		payment.PurposeOrder: callback.New("food", cfg.FoodBaseURL, cfg.FoodCallbackToken),
		payment.PurposeRide:  callback.New("vtc", cfg.VTCBaseURL, cfg.VTCCallbackToken),
		// L'abonnement de courses va chez les COURSES, comme une course —
		// mais sous son propre nom, parce que son `ref_id` n'est pas une
		// course.
		payment.PurposeRideSubscription: callback.New("vtc", cfg.VTCBaseURL, cfg.VTCCallbackToken),
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
	// La langue choisie et les catégories coupées de chaque compte — et les
	// POPULATIONS des campagnes (tous les clients, tous les chauffeurs…).
	notifySvc.SetAccounts(userSvc)
	notifySvc.SetAudiences(userRepo)
	// ⚠️ ET DANS L'AUTRE SENS : la connexion PRÉVIENT quand elle chasse une
	// session. C'est la seule alerte que reçoit un chauffeur dont quelqu'un
	// d'autre utilise le compte — l'appareil chassé, lui, est peut-être éteint.
	// Réglé ici, après construction, pour la même raison que le staff : les
	// notifications ont besoin des comptes, et les comptes des notifications.
	userSvc.SetNotifier(notifySvc)
	// Les campagnes programmées partent à l'heure dite : un tic par
	// demi-minute, sûr sur plusieurs instances (la réservation est un
	// compare-and-set en base).
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tctx, cancel := context.WithTimeout(ctx, 25*time.Second)
				notifySvc.RunDueCampaigns(tctx)
				cancel()
			}
		}
	}()
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
	// LA SUPERVISION. ⚠️ Le compteur de requêtes s'installe APRÈS
	// `RequestID` (pour que la ligne de journal et la mesure parlent de la
	// même requête) et AVANT tout le reste, pour que ce qu'un refus de
	// cadence ou d'authentification rejette soit compté lui aussi : une
	// attaque qui se fait refuser mille fois par minute doit se VOIR.
	metrics := obs.NewMetrics("core", version)
	// Et la moitié SORTANTE : ce que le socle va demander aux cartes, et ce
	// qu'il rappelle aux verticales. ⚠️ Les ponts sont construits BIEN AVANT
	// cette ligne ; leur transport lit la couche au moment de l'appel, pas à la
	// construction — sans quoi l'ordre de ce fichier déciderait de ce qui se
	// mesure, et un déplacement de deux lignes ferait disparaître une courbe
	// sans message d'erreur.
	obs.SetDefault(metrics, logger)
	router.Use(middleware.RequestID)
	router.Use(metrics.Middleware)
	router.Use(middleware.Logger(logger))
	router.Use(middleware.Recoverer(metrics))
	router.Use(middleware.Language)
	router.Use(countryMW)
	router.Use(middleware.RateLimit(rdb, cfg.RateLimitRPM))

	// LES PANNES : capturées ici, rangées groupées, servies à la console.
	// ⚠️ La capture est ASYNCHRONE — une supervision qui ralentit ce qu'elle
	// observe finit par être coupée, et on devient aveugle pour de bon.
	faultRepo := faults.NewRepository(mongo)
	faultSvc := faults.NewService(ctx, faultRepo, version)
	metrics.SetSink(faultSvc)
	// Et TOUS les refus serveur passent par `httpx.Error` : posé là, un `5xx`
	// ne peut pas passer inaperçu.
	httpx.SetFaultSink(faultSvc)
	// Le nombre de pannes OUVERTES est une mesure : c'est elle qu'une alerte
	// surveille, et elle se lit au moment où on la regarde.
	metrics.Gauge("dira_faults_open", "Pannes ouvertes, par service concerné.", nil, func() float64 {
		read, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		counts, err := faultRepo.Counts(read)
		if err != nil {
			return 0
		}
		total := 0
		for _, n := range counts {
			total += n
		}
		return float64(total)
	})

	// ⚠️ `/metrics` N'EST PAS DERRIÈRE LA PASSERELLE. Celle-ci ne route que
	// `/api/v1/…` : cette adresse n'existe donc que sur le réseau interne,
	// là où le collecteur la lit. C'est voulu — l'exposition dit le nom des
	// routes, le nombre de comptes et la version qui tourne.
	router.Handle("/metrics", metrics.Handler())

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

	// LE MATÉRIEL — gilets, sacs, téléphones vendus, loués ou prêtés aux
	// livreurs et chauffeurs, et surtout COMMENT l'argent revient : sur le
	// solde Dira (livreurs, ici), retenu sur les gains, ou porté au grand
	// livre des courses par la verticale (`/internal/equipment/collect`).
	// Un balayage par minute vieillit les échéances, ouvre les périodes de
	// loyer, rappelle, prélève, prévient.
	financeSvc := finance.NewService(financeRepo, auditRec, cfg.FinanceJournalSince)
	financeSvc.SetStaffAlerter(staffAlerts{staff: staffSvc, notify: notifySvc})
	// Le balayage d'intégrité : les soldes recalculés, le journal vérifié,
	// à cadence fixe — et à la demande depuis la console.
	go financeSvc.RunEvery(ctx, cfg.FinanceIntegrityInterval)

	equipmentSvc := equipment.NewService(equipment.NewRepository(mongo), tokenSvc, userSvc)
	equipmentSvc.SetNotifier(notifySvc)
	equipmentSvc.SetStaffAlerter(staffAlerts{staff: staffSvc, notify: notifySvc})
	equipmentSvc.SetAuditor(auditRec)
	// La base des liens profonds du QR de remise — vide = lien relatif.
	equipmentSvc.SetHandoverLinkBase(cfg.AppLinkBase)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tctx, cancel := context.WithTimeout(ctx, 50*time.Second)
				equipmentSvc.RunDue(tctx)
				cancel()
			}
		}
	}()
	// LE BOUTON D'ALERTE — voir `internal/sos`.
	//
	// ⚠️ AU SOCLE, et pas dans une verticale : un passager de course et un
	// client de livraison appuient sur le même bouton, et l'exploitation doit
	// les voir dans la même file. Le socle ne résout pas la course — il garde
	// la référence, et c'est la console qui compose.
	sosRepo := sos.NewRepository(mongo)
	if err := sosRepo.EnsureIndexes(ctx); err != nil {
		// ⚠️ ON DÉMARRE QUAND MÊME, et c'est délibéré : sans index, la file est
		// lente ; sans service, il n'y a pas de bouton d'alerte du tout.
		logger.Error("sos: index non créé — la file sera lente", "error", err)
	}
	sosSvc := sos.NewService(sosRepo)
	sosSvc.SetAccounts(userSvc)
	sosSvc.SetStaffAlerts(staffAlerts{staff: staffSvc, notify: notifySvc})
	sosSvc.SetPolicy(sosPolicy{countries: countrySvc})
	sosSvc.SetAuditor(auditRec)

	// LES OBJECTIFS À ATTEINDRE — voir `internal/challenge`.
	//
	// ⚠️ AU SOCLE, parce que les trois publics y sont : un chauffeur
	// (courses), un livreur (livraisons) et un client (les deux) participent
	// aux mêmes opérations, le bonus sort du même argent, et l'exploitation
	// doit pouvoir répondre à « combien devons-nous en bonus ce mois-ci ? » en
	// UN endroit.
	challengeRepo := challenge.NewRepository(mongo)
	if err := challengeRepo.EnsureIndexes(ctx); err != nil {
		// ⚠️ ON DÉMARRE QUAND MÊME pour les lectures, MAIS ON CRIE : l'index
		// unique de l'avancement est ce qui tient « une seule ligne par
		// personne et par objectif ». Sans lui, deux appels simultanés d'une
		// verticale créent deux avancements à mi-chemin, et l'objectif ne se
		// gagne jamais.
		logger.Error("challenge: INDEX UNIQUE NON CRÉÉ — les avancements peuvent se dédoubler", "error", err)
	}
	challengeSvc := challenge.NewService(challengeRepo)
	challengeSvc.SetPurse(tokenSvc)
	challengeSvc.SetNotifier(notifySvc)
	challengeSvc.SetAuditor(auditRec)
	// ⚠️ LE GRAND LIVRE DES COURSES N'EST PAS BRANCHÉ ICI, et il faut le dire :
	// un chauffeur VTC n'a pas de portefeuille au socle, son argent vit dans la
	// verticale. Tant que `SetLedger` n'est pas câblé, un bonus de CHAUFFEUR
	// n'est pas versé — le droit est enregistré, la place est rendue, et le
	// journal le crie. Les bonus de CLIENT et de LIVREUR, eux, passent par le
	// portefeuille et fonctionnent.
	go func() {
		// Le balayage des séries périodiques : il matérialise l'occurrence de
		// la période courante. ⚠️ À l'heure, pas à la minute — une occurrence
		// hebdomadaire n'a pas besoin d'être créée à la seconde près, et un
		// balayage trop fréquent lirait toutes les séries pour rien.
		challengeSvc.RunDue(ctx)
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				challengeSvc.RunDue(tctx)
				cancel()
			}
		}
	}()

	staffSvc.SetAuditor(auditRec)
	// ⚠️ Réglé APRÈS construction, pour casser le cycle : le staff a besoin
	// des comptes, et les comptes ont besoin des portées.
	userSvc.SetStaffEntitlements(staffEntitlements{svc: staffSvc})
	// UN SEUL APPAREIL PAR CHAUFFEUR : le registre partagé, et le journal.
	// ⚠️ Sans l'entrée d'audit, « pourquoi ai-je été déconnecté ? » reste sans
	// réponse : le compte porte l'appareil COURANT, jamais l'histoire.
	userSvc.SetSessions(sessions)
	userSvc.SetAuditor(auditRec)
	countrySvc.SetAuditor(auditRec)

	// LA SUPPRESSION D'UN COMPTE — fermeture immédiate, effacement de
	// l'identité après le délai de grâce. Voir `internal/user/erasure.go`.
	//
	// ⚠️ LE PORTEFEUILLE EST BRANCHÉ EN PREMIER, et il n'est pas facultatif en
	// pratique : sans lui, un client supprimerait son compte avec son solde
	// Dira Cash dedans, et cet argent serait détruit sans écriture.
	userSvc.SetBalances(tokenSvc)
	userSvc.SetInbox(notifySvc)
	// ⚠️ LA PHOTO DE PROFIL EST UN FICHIER, pas un champ : sans ce câblage, le
	// visage d'une personne effacée reste dans le bucket et plus rien ne le
	// désigne.
	if media != nil {
		userSvc.SetFiles(media)
	}
	userSvc.SetPushDevices(notifySvc)
	userSvc.SetErasureAnnouncer(erasureAnnouncer{client: asynqClient})
	userSvc.SetErasureGrace(cfg.AccountErasureGrace)
	// Le balayage qui efface ce qui est arrivé à terme. À cadence lente :
	// personne n'attend à la minute un effacement prévu trente jours plus tôt.
	//
	// ⚠️ IL TOURNE SUR CHAQUE INSTANCE, et c'est sans danger : `erase` ne fait
	// rien d'un compte déjà anonymisé, et la liste des comptes dus est bornée.
	// Deux instances qui balaient en même temps font le même travail deux
	// fois, pas deux fois le travail.
	go func() {
		t := time.NewTicker(cfg.AccountErasureSweep)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tctx, cancel := context.WithTimeout(ctx, time.Minute)
				if n, err := userSvc.EraseDue(tctx, 200); err != nil {
					logger.Error("core: the erasure sweep failed — closed accounts keep their identity", "error", err)
				} else if n > 0 {
					logger.Info("core: accounts erased", "count", n)
				}
				cancel()
			}
		}
	}()

	router.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		user.NewHandler(userSvc).Mount(r, authMW)
		countryHandler := country.NewHandler(countrySvc)
		countryHandler.Mount(r, authMW)
		countryHandler.MountService(r, middleware.Service(cfg.ServiceToken))
		// LES MARQUEURS DE CARTE hors véhicules — livreur, client, marchand,
		// arrêt — lus par toute application qui dessine une carte, réglés
		// depuis la console à côté des modes de véhicule.
		marker.NewHandler(marker.NewService(mongo, auditRec)).Mount(r, authMW)
		// LES CODES PROMO de toute la plateforme — campagnes, influenceurs,
		// parrainage. ⚠️ AU SOCLE parce qu'un code doit être unique partout et
		// porter UNE seule enveloppe : le code d'un influenceur vaut sur une
		// course ET sur une commande. Voir `internal/promocode`.
		promoSvc := promocode.NewService(promocode.NewRepository(mongo), auditRec)
		promoSvc.SetCredits(promoCredits{tokens: tokenSvc})
		promoSvc.SetReferral(promoReferral{countries: countrySvc})
		promoH := promocode.NewHandler(promoSvc)
		promoH.Mount(r, authMW)
		promoH.MountService(r, middleware.Service(cfg.ServiceToken))
		// L'ENVOI DE FICHIERS, pour tout rôle connecté : avatar, véhicule,
		// document de conformité, et les objets des verticales (plat, point de
		// vente, enseigne, vidéo de feed, bannière). Une porte, une règle.
		upload.NewHandler(media).Mount(r, authMW)
		// ⚠️ LA GESTION DES COMPTES PAR LA CONSOLE — ouvrir, corriger,
		// supprimer. Le module existait, ses trois routes aussi, et rien ne
		// les montait : « + Nouveau client » et « Éditer » répondaient 404
		// depuis la scission. Un handler écrit et jamais monté se lit, à la
		// relecture, comme une route qui existe.
		adminUsers := user.NewAdminUsers(userRepo, tokenSvc, auditRec)
		adminUsers.SetDefaultCountry(cfg.CountryDefault)
		// ⚠️ `DELETE /admin/users/{id}` NE DÉTRUIT PLUS LA LIGNE : il ferme le
		// compte et programme l'effacement de l'identité, comme la demande de
		// la personne elle-même. Détruire la ligne cassait toutes les courses
		// et commandes qui la désignent — voir `internal/user/erasure.go`.
		adminUsers.SetErasure(userSvc)
		adminUsers.Mount(r, authMW)
		// LE JOURNAL D'AUDIT de toute la plateforme, lu ici et nulle part
		// ailleurs : les verticales y écrivent par la surface de service.
		auditlog.NewHandler(auditRec).Mount(r, authMW)
		faults.NewHandler(faultRepo).Mount(r, authMW)
		equipment.NewHandler(equipmentSvc).Mount(r, authMW)
		// LE BOUTON D'ALERTE : `POST /sos` pour qui est en danger,
		// `/admin/sos` pour l'exploitation qui doit le voir dans la seconde.
		sos.NewHandler(sosSvc).Mount(r, authMW)
		// LES OBJECTIFS : `/me/challenges` pour qui joue, `/admin/challenges`
		// pour l'exploitation qui les écrit et les lance.
		challenge.NewHandler(challengeSvc).Mount(r, authMW)
		financeH := finance.NewHandler(financeSvc)
		financeH.Mount(r, authMW)
		financeH.MountInternal(r, middleware.Service(cfg.ServiceToken))
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
		// Les listes de la console (tous les portefeuilles, tous les
		// mouvements du pays), avec le nom des titulaires qui sont des comptes.
		tokenHandler.SetBackOffice(tokenRepo, userSvc)
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
		internalAPI := serviceapi.NewHandler(userSvc, tokenSvc, notifySvc, paymentSvc,
			backOffice{tokens: tokenRepo, payments: paymentRepo})
		// Les alertes du STAFF : la verticale dit quoi et pour quel pays, le
		// socle trouve à qui — les membres dont le périmètre couvre la
		// verticale.
		internalAPI.SetStaff(staffSvc)
		internalAPI.SetJournal(auditRec)
		internalAPI.SetEquipment(equipmentSvc)
		// Les objectifs : la verticale pousse ses faits comptés, et tire ce
		// qu'on doit à un chauffeur — qui n'a pas de portefeuille ici.
		internalAPI.SetChallenges(challengeSvc)
		// ⚠️ LE BUCKET N'EST OUVERT QU'ICI. Quand un compte est effacé, la
		// photo de son permis vit chez les COURSES et chez la LIVRAISON, dans
		// des collections que le socle ne connaît pas : c'est à elles de dire
		// quelles images, et à nous de les retirer.
		if media != nil {
			internalAPI.SetFiles(media)
		}
		// ⚠️ QUI VOIT QUOI DE QUI, réglé par pays et par métier. Sans ce
		// câblage, la porte de divulgation rend les défauts du métier : les
		// applications marchent, mais la console ne commande plus rien — et
		// c'est le genre de fil manquant qu'on ne découvre qu'en se demandant
		// pourquoi un réglage « ne prend pas ».
		internalAPI.SetPrivacy(countrySvc)
		internalAPI.Mount(r, middleware.Service(cfg.ServiceToken))
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
	cbWorker := callback.NewWorker(verticals)
	mux.HandleFunc(jobs.TypeRefPaid, cbWorker.HandleRefPaid)
	mux.HandleFunc(jobs.TypeAccountErased, cbWorker.HandleAccountErased)
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
		out = append(out, fcm.Message{Token: m.Token, Title: m.Title, Body: m.Body, Data: m.Data, DataOnly: m.DataOnly, TTL: m.TTL})
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
		ID: r.ID, Role: r.Role, Name: r.Name, Phone: r.Phone, Email: r.Email, Status: r.Status, Country: r.Country,
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

// erasureAnnouncer met en file le fait qu'un compte est effacé, pour que
// chaque verticale purge ce qu'elle seule détient.
//
// ⚠️ EN FILE, ET SANS JAMAIS FAIRE ÉCHOUER L'EFFACEMENT. Quand cette méthode
// est appelée, l'identité est DÉJÀ partie du socle : rendre une erreur ne la
// ramènerait pas, et refuser l'effacement parce qu'une file est indisponible
// rendrait le droit à l'effacement dépendant de Redis.
type erasureAnnouncer struct{ client *asynq.Client }

func (a erasureAnnouncer) AccountErased(ctx context.Context, userID, phone string) {
	if a.client == nil {
		slog.ErrorContext(ctx, "core: no job queue — the verticals will NOT purge this erased account",
			"user_id", userID, "hint", "check REDIS_URI")
		return
	}
	task, err := jobs.NewTask(jobs.TypeAccountErased,
		jobs.AccountErasedPayload{UserID: userID, Phone: phone})
	if err != nil {
		slog.ErrorContext(ctx, "core: account-erased task not built", "user_id", userID, "error", err)
		return
	}
	// ⚠️ L'identifiant de tâche est DÉRIVÉ DU COMPTE, et la reprise est
	// longue : un compte effacé le reste, et la purge chez les verticales peut
	// attendre la fin d'un redéploiement.
	//
	// ⚠️ MAIS LA RÉTENTION EST COURTE — une heure, pas les 72 h des autres
	// annonces. Cette charge porte un NUMÉRO DE TÉLÉPHONE, parce qu'une
	// verticale garde des traces classées dessus ; le laisser dormir trois
	// jours dans Redis après avoir promis de l'effacer serait contredire la
	// promesse dans la file qui l'exécute.
	if _, err := a.client.EnqueueContext(ctx, task,
		asynq.TaskID("account-erased:"+userID),
		asynq.MaxRetry(20),
		asynq.Retention(jobs.AccountErasedRetention),
	); err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			return
		}
		slog.ErrorContext(ctx, "core: account-erased not queued — the verticals keep what this person wrote",
			"user_id", userID, "error", err)
	}
}

// promoCredits crédite le solde d'un PARRAIN quand son filleul a vraiment
// commandé.
//
// ⚠️ UN CRÉDIT OFFERT, et pas de l'argent versé : il passe par `promo_xof` du
// portefeuille — dépensé avant l'argent réel, et non remboursable. Un
// parrainage payé en argent réel serait retirable en espèces, et le parrainage
// deviendrait un distributeur.
type promoCredits struct{ tokens *token.Service }

// promoReferral dit au registre des codes CE QUE DONNE le parrainage dans le
// pays de l'opération — les montants réglés depuis la console.
//
// ⚠️ L'ADAPTATEUR TRADUIT, il ne décide pas. Les deux paquets portent chacun
// leur `ReferralPolicy` / `Referral` : celui du pays parce que c'est un réglage
// d'installation, celui des codes parce que c'est ce dont le tirage a besoin.
// Faire importer l'un par l'autre aurait fait d'un réglage d'exploitation une
// dépendance du registre d'argent, dans un sens ou dans l'autre.
type promoReferral struct{ countries *country.Service }

func (p promoReferral) ReferralOf(ctx context.Context, code string) promocode.ReferralPolicy {
	r := p.countries.ReferralOf(ctx, code)
	return promocode.ReferralPolicy{
		InviteeXOF: r.InviteeXOF, SponsorXOF: r.SponsorXOF,
		MaxSponsored: r.MaxSponsored, ValidDays: r.ValidDays,
	}
}

func (c promoCredits) PromoCredit(ctx context.Context, userID string, amountXOF int, reason string) error {
	_, err := c.tokens.PromoByOperator(ctx, "", userID, amountXOF, "parrainage : "+reason)
	return err
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

// sosPolicy branche la politique d'alerte d'un pays sur le module d'alerte.
//
// ⚠️ UNE TRADUCTION, PAS UN RACCOURCI. Le module d'alerte déclare son propre
// `Settings` plutôt que d'importer celui du module pays : il ne doit pas
// dépendre de la forme qu'un réglage de console a prise. Le prix est cette
// recopie de six champs, qu'un test de forme garde honnête.
type sosPolicy struct{ countries *country.Service }

func (p sosPolicy) SOSSettings(ctx context.Context, code string) sos.Settings {
	s := p.countries.SOSOf(ctx, code)
	// ⚠️ AUCUN NUMÉRO NE PASSE PAR ICI, et c'est le point. Ce que cette
	// fonction sert part vers les APPLICATIONS : leur donner des numéros
	// finirait par un bouton d'appel, alors que le protocole veut que ce soit
	// le service client qui appelle. Les numéros vont à la console par
	// `EmergencyNumbers`, un chemin séparé.
	return sos.Settings{
		Button: s.Button, Shake: s.Shake, Crash: s.Crash, Voice: s.Voice,
		CountdownSeconds: s.CountdownSeconds, CallsBack: s.CallsBack,
	}
}

// EmergencyNumbers rend les numéros d'un pays à la CONSOLE — le catalogue,
// corrigé par le réglage de l'exploitation.
func (p sosPolicy) EmergencyNumbers(ctx context.Context, code string) ([]sos.Number, bool) {
	nums, confirmed := p.countries.EmergencyOf(ctx, code)
	out := make([]sos.Number, 0, len(nums))
	for _, n := range nums {
		out = append(out, sos.Number{Kind: n.Kind, Label: n.Label, Number: n.Number})
	}
	return out, confirmed
}

// staffAlerts prévient les membres du staff dont le périmètre couvre une
// verticale — le même chemin que `POST /internal/notifications/staff`, pour
// un module du socle.
type staffAlerts struct {
	staff  *staff.Service
	notify *notify.Service
}

func (a staffAlerts) AlertStaff(ctx context.Context, scope, country, key string, vars, data map[string]string) {
	ids, err := a.staff.Recipients(ctx, scope, country)
	if err != nil {
		return
	}
	for _, id := range ids {
		a.notify.Notify(ctx, id, key, vars, data)
	}
}

// otpSender choisit le canal de remise du code à usage unique.
//
// ⚠️ UN SEUL EXPÉDITEUR EXISTE AUJOURD'HUI — `echo` —, et il ne remet rien :
// il rend le code dans la réponse HTTP pour que les applications se câblent
// avant la passerelle. Tant que c'est lui qui sert, N'IMPORTE QUI CONNAISSANT
// UN NUMÉRO ENTRE DANS LE COMPTE : le démarrage le dit en ERROR, et non en
// WARN, parce qu'une ligne d'avertissement dans un journal de production ne
// réveille personne.
//
// `whatsapp` et `sms` sont refusés plutôt que silencieusement rabattus sur
// `echo` : régler une variable et croire les codes partis serait pire que
// l'absence de canal.
func otpSender(logger *slog.Logger, kind string) user.OTPSender {
	switch kind {
	case "echo":
		logger.Error("auth: OTP en mode ECHO — le code est rendu EN CLAIR dans la réponse ; aucune passerelle n'est câblée")
		return user.EchoSender{}
	default:
		logger.Error("auth: OTP_SENDER non implémenté, la porte par code reste FERMÉE", "sender", kind)
		return nil
	}
}
