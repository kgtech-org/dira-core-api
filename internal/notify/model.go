package notify

import (
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collections MongoDB.
const (
	CollectionTemplates = "message_templates"
	CollectionDevices   = "push_devices"
)

// Plateformes d'un appareil.
const (
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
	PlatformWeb     = "web"
)

// Text est un gabarit dans UNE langue.
type Text struct {
	Title string `bson:"title" json:"title"`
	Body  string `bson:"body" json:"body"`
}

// Template est un message, dans toutes ses langues.
//
// Un document PAR CLÉ, avec les langues à l'intérieur, plutôt qu'un document
// par (clé, langue) : ce qu'un exploitant veut voir, c'est une phrase et ses
// traductions côte à côte. Les répartir sur plusieurs documents obligerait à
// les recoller à chaque lecture, et laisserait une traduction s'écarter des
// autres sans que rien ne le montre.
type Template struct {
	ID  primitive.ObjectID `bson:"_id,omitempty"`
	Key string             `bson:"key"`
	// Locales : code de langue → texte. Le français doit y être : c'est le
	// repli, et un gabarit qui ne l'a pas ne peut rien servir.
	Locales map[string]Text `bson:"locales"`
	// Description dit à l'exploitant QUAND ce message part. Sans elle, une
	// liste de clés techniques ne se relit pas.
	Description string `bson:"description,omitempty"`
	// Enabled permet de couper un message sans le supprimer. Le supprimer
	// ferait repartir le gabarit compilé par défaut à la lecture suivante —
	// donc le contraire de ce qu'on voulait.
	Enabled   bool      `bson:"enabled"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Device est un téléphone joignable.
type Device struct {
	ID     primitive.ObjectID `bson:"_id,omitempty"`
	UserID primitive.ObjectID `bson:"user_id"`
	// Token est le jeton FCM. UNIQUE : réinstaller l'application en donne un
	// nouveau, mais changer de compte sur le MÊME téléphone garde l'ancien —
	// et sans unicité, l'ex-utilisateur continuerait de recevoir les
	// notifications du nouveau.
	Token    string `bson:"token"`
	Platform string `bson:"platform,omitempty"`
	// Locale est la langue de l'APPAREIL. Elle prime sur celle du compte :
	// c'est celle de l'écran que la personne a sous les yeux.
	Locale     string    `bson:"locale,omitempty"`
	CreatedAt  time.Time `bson:"created_at"`
	LastSeenAt time.Time `bson:"last_seen_at"`
	// DisabledAt marque un jeton que FCM a déclaré mort. On le GARDE, daté,
	// au lieu de l'effacer : le support doit pouvoir répondre « votre
	// téléphone ne reçoit plus depuis le 3 mars » plutôt que « je ne vois
	// aucun appareil ».
	DisabledAt *time.Time `bson:"disabled_at,omitempty"`
	// DisabledReason est ce que FCM a répondu.
	DisabledReason string `bson:"disabled_reason,omitempty"`
}

// Clés des messages émis par la plateforme.
//
// Des CONSTANTES et non des chaînes libres : une clé mal orthographiée à
// l'appel ne se voit qu'à l'exécution, au moment précis où la notification
// aurait dû partir.
const (
	KeyOrderConfirmed = "order_confirmed"
	KeyOrderPreparing = "order_preparing"
	KeyOrderReady     = "order_ready"
	KeyOrderAssigned  = "order_assigned"
	KeyOrderDelivered = "order_delivered"
	KeyOrderCancelled = "order_cancelled"
	KeyChatMessage    = "chat_message"
	KeyDriverCall     = "driver_call"
	KeyDispatchFailed = "dispatch_failed"
	// KeyMerchantNewOrder prévient un point de vente qu'une commande vient de
	// tomber. Sans lui, un marchand ne recevait RIEN : tous les autres
	// gabarits visent le client ou le livreur, et il fallait qu'il regarde
	// son écran pour s'en apercevoir.
	KeyMerchantNewOrder = "merchant_new_order"
	// Les COURSES PROGRAMMÉES (dira-vtc-api) : le passager est prévenu
	// avant que l'appel ne parte, quand il part, et quand il n'a pas pu
	// partir — une course qui devait partir à 7 h et qui ne part pas est
	// pire qu'une course jamais programmée.
	KeyRideScheduledSoon    = "ride_scheduled_soon"
	KeyRideScheduledStarted = "ride_scheduled_started"
	KeyRideScheduledFailed  = "ride_scheduled_failed"
)

// defaults sont les gabarits COMPILÉS, servis tant que la base n'en porte pas.
//
// Ils existent pour que la plateforme notifie dès le premier démarrage, sans
// qu'un exploitant ait à saisir vingt textes avant que quoi que ce soit ne
// parte. Toute modification en base les remplace — ils ne sont qu'un plancher.
var defaults = map[string]Template{
	KeyOrderConfirmed: {
		Key:         KeyOrderConfirmed,
		Description: "La commande est confirmée et payée.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Commande confirmée", Body: "Votre commande [order_ref] est confirmée. Le restaurant va la préparer."},
			LocaleEN: {Title: "Order confirmed", Body: "Your order [order_ref] is confirmed. The restaurant will start preparing it."},
		},
	},
	KeyOrderPreparing: {
		Key:         KeyOrderPreparing,
		Description: "Le marchand a commencé la préparation.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "En préparation", Body: "Votre commande [order_ref] est en préparation."},
			LocaleEN: {Title: "Being prepared", Body: "Your order [order_ref] is being prepared."},
		},
	},
	KeyOrderReady: {
		Key:         KeyOrderReady,
		Description: "La commande est prête, on cherche un livreur.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Commande prête", Body: "Votre commande [order_ref] est prête. Nous cherchons un livreur."},
			LocaleEN: {Title: "Order ready", Body: "Your order [order_ref] is ready. We are finding a driver."},
		},
	},
	KeyOrderAssigned: {
		Key:         KeyOrderAssigned,
		Description: "Un livreur a pris la course.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Livreur en route", Body: "Un livreur a pris votre commande [order_ref] et se rend au restaurant."},
			LocaleEN: {Title: "Driver on the way", Body: "A driver took your order [order_ref] and is heading to the restaurant."},
		},
	},
	KeyOrderDelivered: {
		Key:         KeyOrderDelivered,
		Description: "La commande est livrée.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Commande livrée", Body: "Bon appétit ! Notez votre commande [order_ref] en quelques secondes."},
			LocaleEN: {Title: "Order delivered", Body: "Enjoy! Rate your order [order_ref] in a few seconds."},
		},
	},
	KeyOrderCancelled: {
		Key:         KeyOrderCancelled,
		Description: "La commande est annulée.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Commande annulée", Body: "Votre commande [order_ref] a été annulée."},
			LocaleEN: {Title: "Order cancelled", Body: "Your order [order_ref] has been cancelled."},
		},
	},
	KeyChatMessage: {
		Key: KeyChatMessage,
		// ⚠️ Ni nom ni numéro : le canal existe pour que le client et le
		// livreur se parlent SANS échanger leurs coordonnées. Une
		// notification qui nommerait l'expéditeur défairait cela sur l'écran
		// verrouillé.
		Description: "Message reçu dans la conversation de la commande. Ne nomme JAMAIS l'expéditeur.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Nouveau message", Body: "[body]"},
			LocaleEN: {Title: "New message", Body: "[body]"},
		},
	},
	KeyDriverCall: {
		Key:         KeyDriverCall,
		Description: "Une course est proposée au livreur. Il a quelques secondes pour répondre.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Nouvelle course", Body: "[distance_km] km · [token_cost] jeton(s)[cash_line]. Répondez vite."},
			LocaleEN: {Title: "New delivery", Body: "[distance_km] km · [token_cost] token(s)[cash_line]. Answer quickly."},
		},
	},
	KeyMerchantNewOrder: {
		Key:         KeyMerchantNewOrder,
		Description: "Une commande vient de tomber sur ce point de vente — message au MARCHAND.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Nouvelle commande", Body: "Commande [order_ref] · [items] article(s) · [amount] FCFA pour vous."},
			LocaleEN: {Title: "New order", Body: "Order [order_ref] · [items] item(s) · [amount] FCFA for you."},
		},
	},
	KeyRideScheduledSoon: {
		Key:         KeyRideScheduledSoon,
		Description: "Course programmée : l'appel du chauffeur part dans quelques minutes — message au PASSAGER.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Votre course part bientôt", Body: "Départ de [pickup] à [time] : nous appelons un chauffeur dans [minutes] min. Soyez prêt."},
			LocaleEN: {Title: "Your ride is coming up", Body: "Pickup at [pickup] at [time]: we will call a driver in [minutes] min. Please be ready."},
		},
	},
	KeyRideScheduledStarted: {
		Key:         KeyRideScheduledStarted,
		Description: "Course programmée : l'appel du chauffeur est parti — message au PASSAGER.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Nous cherchons votre chauffeur", Body: "Votre course de [time] depuis [pickup] est lancée : un chauffeur arrive bientôt."},
			LocaleEN: {Title: "Finding your driver", Body: "Your [time] ride from [pickup] has started: a driver will be on the way shortly."},
		},
	},
	KeyRideScheduledFailed: {
		Key:         KeyRideScheduledFailed,
		Description: "Course programmée : l'appel n'a pas pu partir (paiement, itinéraire) — message au PASSAGER.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Course non lancée", Body: "Votre course de [time] depuis [pickup] n'a pas pu partir : [reason]. Commandez-la à la main."},
			LocaleEN: {Title: "Ride not started", Body: "Your [time] ride from [pickup] could not start: [reason]. Please order it manually."},
		},
	},
	KeyDispatchFailed: {
		Key:         KeyDispatchFailed,
		Description: "Aucun livreur n'a pris la course — message au CLIENT.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Recherche en cours", Body: "Nous cherchons encore un livreur pour votre commande [order_ref]. Merci de patienter."},
			LocaleEN: {Title: "Still searching", Body: "We are still looking for a driver for your order [order_ref]. Thanks for waiting."},
		},
	},
}

// provided déclare, par clé, les variables que la PLATEFORME sait remplir.
//
// C'est la liste de référence de l'écran d'administration, et surtout le
// garde-fou de l'enregistrement : une variable absente d'ici rendrait du VIDE
// à chaque envoi, définitivement. Personne n'écrit `[store_nom]` en le
// sachant — on le découvre sur le téléphone d'un client, des semaines plus
// tard. Le refus à la saisie est le seul moment où cela se corrige.
var provided = map[string][]string{
	KeyOrderConfirmed: {"order_ref"},
	KeyOrderPreparing: {"order_ref"},
	KeyOrderReady:     {"order_ref"},
	KeyOrderAssigned:  {"order_ref"},
	KeyOrderDelivered: {"order_ref"},
	KeyOrderCancelled: {"order_ref"},
	// ⚠️ `body` et RIEN d'autre. Le canal existe pour que le client et le
	// livreur se parlent sans échanger leurs coordonnées : offrir un
	// `sender_name` ici défairait cela sur l'écran verrouillé.
	KeyChatMessage:      {"body"},
	KeyDriverCall:       {"distance_km", "token_cost", "cash_line"},
	KeyDispatchFailed:   {"order_ref"},
	KeyMerchantNewOrder: {"order_ref", "items", "amount"},
}

// Provided rend les variables que la plateforme remplit pour une clé.
func Provided(key string) []string {
	out := append([]string(nil), provided[key]...)
	sort.Strings(out)
	return out
}

// unknownVariables rend les variables d'un texte que la plateforme ne remplit
// jamais — celles qui rendraient du vide à chaque envoi.
func unknownVariables(key string, parts ...string) []string {
	known := map[string]bool{}
	for _, v := range provided[key] {
		known[v] = true
	}
	var out []string
	for _, v := range Variables(parts...) {
		if !known[v] {
			out = append(out, v)
		}
	}
	return out
}

// Defaults rend une copie des gabarits compilés, pour l'amorçage et l'écran
// d'administration.
func Defaults() []Template {
	out := make([]Template, 0, len(defaults))
	for _, t := range defaults {
		copied := t
		copied.Locales = map[string]Text{}
		for loc, txt := range t.Locales {
			copied.Locales[loc] = txt
		}
		out = append(out, copied)
	}
	return out
}
