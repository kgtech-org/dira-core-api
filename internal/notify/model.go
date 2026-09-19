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
	// KeyDeliveryExpired : aucun livreur en N minutes — la course est retirée
	// du pot commun, et c'est au MARCHAND de relancer la recherche. Sans ce
	// message, il attendrait devant un repas froid sans savoir que plus
	// personne n'est appelé.
	KeyDeliveryExpired = "delivery_expired"
	// KeyRideTipReceived : un passager a laissé un pourboire — message au
	// CHAUFFEUR. Un pourboire qui arrive sans un mot est un chiffre de plus
	// au relevé ; dit à l'instant, c'est ce qui fait la journée.
	KeyRideTipReceived = "ride_tip_received"
	// KeyRideRatePrompt : la course est terminée — le PASSAGER est invité à
	// noter. Envoyé à la fin de la course, pas à la fermeture de l'écran :
	// un passager qui a rangé son téléphone en descendant ne note jamais
	// sans ce rappel.
	KeyRideRatePrompt = "ride_rate_prompt"
	// LES ALERTES DU STAFF — ce que l'exploitation doit voir sans regarder
	// son écran : poussées aux membres dont le périmètre couvre la verticale
	// (`POST /internal/notifications/staff`). `staff_dispatch_failed` : une
	// course ou une livraison sans preneur ; `staff_document_submitted` :
	// une pièce déposée, à vérifier ; `staff_driver_pending` : un nouveau
	// chauffeur ou livreur attend sa validation.
	KeyStaffDispatchFailed    = "staff_dispatch_failed"
	KeyStaffDocumentSubmitted = "staff_document_submitted"
	KeyStaffDriverPending     = "staff_driver_pending"
	// Les CHANGEMENTS D'ÉTAT d'une course, au passager — ce qui réveille une
	// application en arrière-plan quand le socket du suivi ne l'atteint
	// plus. Chacun porte en données `type: ride_status`, `ride_id`, `status`
	// pour que l'application relise la course par HTTP.
	KeyRideAccepted       = "ride_accepted"
	KeyRideDriverOnTheWay = "ride_driver_on_the_way"
	KeyRideCancelled      = "ride_cancelled"
	// KeyRideCancelledByRider va au CHAUFFEUR : il roulait peut-être déjà
	// vers le point de départ.
	KeyRideCancelledByRider = "ride_cancelled_by_rider"
	// KeyRideSearchExhausted : la recherche de chauffeur s'est arrêtée sans
	// preneur — message au PASSAGER, avec de quoi relancer. La course n'est
	// pas annulée : c'est lui qui décide.
	KeyRideSearchExhausted = "ride_search_exhausted"
	// KeyDeliveryCancelled va au LIVREUR qui portait la course d'une commande
	// annulée : il est libre, et doit l'apprendre avant d'arriver au
	// restaurant.
	KeyDeliveryCancelled = "delivery_cancelled"
	// KeyRideStopsChanged va au CHAUFFEUR quand le trajet d'une course en
	// cours change — un arrêt ajouté ou retiré, et un prix recalculé. Il
	// relit la course : c'est elle qui porte la nouvelle destination.
	KeyRideStopsChanged = "ride_stops_changed"
	// KeyRideFareAdjusted va au PASSAGER pour le même changement : le
	// nouveau prix, et ce qui a été débité ou rendu.
	KeyRideFareAdjusted = "ride_fare_adjusted"
	// LE SUPPORT (`pkg/support`, monté par chaque verticale). Les clés sont
	// DÉCLARÉES là-bas — le paquet est importé par les verticales, qui ne
	// doivent pas tirer les gabarits avec lui — et recopiées ici ; un test
	// vérifie qu'aucune n'est orpheline. `lost_item_reported` va au
	// CHAUFFEUR : un passager a oublié quelque chose dans son véhicule ;
	// `lost_item_found` / `lost_item_not_found` au PASSAGER : sa réponse ;
	// `ticket_reply` et `ticket_resolved` à qui a ouvert le ticket.
	KeyLostItemReported      = "lost_item_reported"
	KeyLostItemFound         = "lost_item_found"
	KeyLostItemNotFound      = "lost_item_not_found"
	KeyTicketReply           = "ticket_reply"
	KeyTicketResolved        = "ticket_resolved"
	KeyStaffTicketOpened     = "staff_ticket_opened"
	KeyStaffLostItemAnswered = "staff_lost_item_answered"
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
	KeyRideTipReceived: {
		Key:         KeyRideTipReceived,
		Description: "Un passager a laissé un pourboire — message au CHAUFFEUR.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Pourboire reçu", Body: "[rider] vous a laissé [amount] F de pourboire. Merci pour cette course !"},
			LocaleEN: {Title: "Tip received", Body: "[rider] left you a [amount] F tip. Thank you for this ride!"},
		},
	},
	KeyStaffDispatchFailed: {
		Key:         KeyStaffDispatchFailed,
		Description: "STAFF — une course ou une livraison n'a trouvé personne : à attribuer ou relancer.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "[kind] sans preneur — [ref]", Body: "[place] · personne n'a accepté. À attribuer à la main ou à relancer."},
			LocaleEN: {Title: "[kind] unanswered — [ref]", Body: "[place] · nobody accepted. Assign by hand or relaunch."},
		},
	},
	KeyStaffDocumentSubmitted: {
		Key:         KeyStaffDocumentSubmitted,
		Description: "STAFF — une pièce (permis, assurance…) vient d'être déposée : à vérifier.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Pièce à vérifier", Body: "[who] a déposé : [document]."},
			LocaleEN: {Title: "Document to review", Body: "[who] submitted: [document]."},
		},
	},
	KeyStaffDriverPending: {
		Key:         KeyStaffDriverPending,
		Description: "STAFF — un nouveau chauffeur ou livreur attend sa validation.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Nouveau [kind] à valider", Body: "[who] a ouvert son profil et attend l'habilitation."},
			LocaleEN: {Title: "New [kind] to validate", Body: "[who] opened a profile and awaits approval."},
		},
	},
	KeyRideRatePrompt: {
		Key:         KeyRideRatePrompt,
		Description: "Course terminée : le PASSAGER est invité à noter son chauffeur.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Comment s'est passée votre course ?", Body: "Notez [driver] et, si vous le souhaitez, laissez-lui un pourboire."},
			LocaleEN: {Title: "How was your ride?", Body: "Rate [driver] and, if you like, leave a tip."},
		},
	},
	KeyRideAccepted: {
		Key:         KeyRideAccepted,
		Description: "Un chauffeur a pris la course — message au PASSAGER, avec le nom et la voiture.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Chauffeur trouvé", Body: "[driver] a pris votre course · [vehicle]."},
			LocaleEN: {Title: "Driver found", Body: "[driver] took your ride · [vehicle]."},
		},
	},
	KeyRideDriverOnTheWay: {
		Key:         KeyRideDriverOnTheWay,
		Description: "Le chauffeur roule vers le point de départ — message au PASSAGER.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Votre chauffeur arrive", Body: "[driver] est en route vers vous."},
			LocaleEN: {Title: "Your driver is coming", Body: "[driver] is on the way to you."},
		},
	},
	KeyRideCancelled: {
		Key:         KeyRideCancelled,
		Description: "La course a été annulée par le chauffeur ou la plateforme — message au PASSAGER.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Course annulée", Body: "Votre course a été annulée ([reason]). Commandez-en une autre."},
			LocaleEN: {Title: "Ride cancelled", Body: "Your ride was cancelled ([reason]). Please order another one."},
		},
	},
	KeyRideCancelledByRider: {
		Key:         KeyRideCancelledByRider,
		Description: "Le passager a annulé — message au CHAUFFEUR, qui roulait peut-être déjà.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Course annulée par le passager", Body: "Le passager a annulé la course ([reason]). Vous êtes de nouveau disponible."},
			LocaleEN: {Title: "Ride cancelled by the rider", Body: "The rider cancelled the ride ([reason]). You are available again."},
		},
	},
	KeyRideSearchExhausted: {
		Key:         KeyRideSearchExhausted,
		Description: "Aucun chauffeur n'a pris la course dans le délai — message au PASSAGER, qui peut relancer.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Aucun chauffeur disponible", Body: "Nous n'avons pas trouvé de chauffeur pour votre course depuis [pickup]. Relancez la recherche, ou annulez : vous serez remboursé."},
			LocaleEN: {Title: "No driver available", Body: "We could not find a driver for your ride from [pickup]. Relaunch the search, or cancel for a refund."},
		},
	},
	KeyRideStopsChanged: {
		Key:         KeyRideStopsChanged,
		Description: "Le trajet d'une course en cours a changé (arrêt ajouté ou retiré) — message au CHAUFFEUR, qui relit la course.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Trajet modifié", Body: "Le trajet de la course a changé : [stops] arrêts, nouvelle destination [dest]. Nouveau prix : [fare]."},
			LocaleEN: {Title: "Route changed", Body: "The ride's route has changed: [stops] stops, new destination [dest]. New fare: [fare]."},
		},
	},
	KeyRideFareAdjusted: {
		Key:         KeyRideFareAdjusted,
		Description: "Le prix d'une course en cours a été recalculé après un changement de trajet — message au PASSAGER, avec ce qui a été débité ou rendu.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Prix de la course ajusté", Body: "Votre trajet a changé : le prix passe à [fare]. [adjustment]"},
			LocaleEN: {Title: "Ride fare adjusted", Body: "Your route has changed: the fare is now [fare]. [adjustment]"},
		},
	},
	KeyLostItemReported: {
		Key:         KeyLostItemReported,
		Description: "SUPPORT — un passager signale un objet oublié dans le véhicule : message au CHAUFFEUR, qui répond depuis l'application.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Objet oublié dans votre véhicule", Body: "Un passager a oublié : [item] ([ref]). Vérifiez votre véhicule et répondez depuis l'application."},
			LocaleEN: {Title: "Item left in your vehicle", Body: "A passenger left behind: [item] ([ref]). Check your vehicle and answer from the app."},
		},
	},
	KeyLostItemFound: {
		Key:         KeyLostItemFound,
		Description: "SUPPORT — le chauffeur a retrouvé l'objet : message au PASSAGER, le support organise la restitution.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Objet retrouvé", Body: "Bonne nouvelle : votre [item] a été retrouvé. Le support vous contacte pour la restitution."},
			LocaleEN: {Title: "Item found", Body: "Good news: your [item] was found. Support will contact you to return it."},
		},
	},
	KeyLostItemNotFound: {
		Key:         KeyLostItemNotFound,
		Description: "SUPPORT — le chauffeur n'a pas retrouvé l'objet : message au PASSAGER, le ticket reste ouvert.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Objet non retrouvé", Body: "Le chauffeur n'a pas retrouvé votre [item]. Votre demande reste ouverte, le support la poursuit."},
			LocaleEN: {Title: "Item not found", Body: "The driver could not find your [item]. Your request stays open and support is following up."},
		},
	},
	KeyTicketReply: {
		Key:         KeyTicketReply,
		Description: "SUPPORT — quelqu'un a répondu sur un ticket qu'on a ouvert, ou qui nous concerne.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Réponse du support", Body: "Nouveau message sur votre demande [reference]."},
			LocaleEN: {Title: "Support replied", Body: "New message on your request [reference]."},
		},
	},
	KeyTicketResolved: {
		Key:         KeyTicketResolved,
		Description: "SUPPORT — la demande est close.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Demande traitée", Body: "Votre demande [reference] est close. Rouvrez-en une si le problème persiste."},
			LocaleEN: {Title: "Request resolved", Body: "Your request [reference] is closed. Open a new one if the problem persists."},
		},
	},
	KeyStaffTicketOpened: {
		Key:         KeyStaffTicketOpened,
		Description: "STAFF — un ticket vient d'être ouvert depuis une application.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "[kind] — [ref]", Body: "[who] a ouvert une demande."},
			LocaleEN: {Title: "[kind] — [ref]", Body: "[who] opened a request."},
		},
	},
	KeyStaffLostItemAnswered: {
		Key:         KeyStaffLostItemAnswered,
		Description: "STAFF — le chauffeur a répondu sur un objet perdu : à restituer, ou à poursuivre.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Objet perdu [answer] — [ref]", Body: "[who] a répondu : [item] [answer]."},
			LocaleEN: {Title: "Lost item [answer] — [ref]", Body: "[who] answered: [item] [answer]."},
		},
	},
	KeyDeliveryCancelled: {
		Key:         KeyDeliveryCancelled,
		Description: "La commande a été annulée sous le livreur qui la portait — message au LIVREUR.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Course annulée", Body: "La commande [order_ref] a été annulée. Vous êtes libre pour une autre course."},
			LocaleEN: {Title: "Delivery cancelled", Body: "Order [order_ref] was cancelled. You are free for another delivery."},
		},
	},
	KeyDeliveryExpired: {
		Key:         KeyDeliveryExpired,
		Description: "Aucun livreur en N minutes : la recherche est arrêtée — message au MARCHAND, qui doit relancer.",
		Enabled:     true,
		Locales: map[string]Text{
			LocaleFR: {Title: "Aucun livreur trouvé", Body: "Commande [order_ref] : aucun livreur en [minutes] min. La recherche est arrêtée — relancez-la depuis la commande quand vous êtes prêt."},
			LocaleEN: {Title: "No courier found", Body: "Order [order_ref]: no courier in [minutes] min. The search has stopped — restart it from the order when you are ready."},
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
	KeyChatMessage:    {"body"},
	KeyDriverCall:     {"distance_km", "token_cost", "cash_line"},
	KeyDispatchFailed: {"order_ref"},
	// Les courses programmées : le lieu de départ, l'heure locale, les
	// minutes avant l'appel, et la raison d'un échec.
	KeyRideScheduledSoon:      {"pickup", "time", "minutes"},
	KeyRideScheduledStarted:   {"pickup", "time"},
	KeyRideScheduledFailed:    {"pickup", "time", "reason"},
	KeyDeliveryExpired:        {"order_ref", "minutes"},
	KeyRideTipReceived:        {"rider", "amount"},
	KeyRideRatePrompt:         {"driver"},
	KeyStaffDispatchFailed:    {"kind", "ref", "place"},
	KeyStaffDocumentSubmitted: {"who", "document"},
	KeyStaffDriverPending:     {"kind", "who"},
	KeyRideAccepted:           {"driver", "vehicle"},
	KeyRideDriverOnTheWay:     {"driver"},
	KeyRideCancelled:          {"reason"},
	KeyRideCancelledByRider:   {"reason"},
	KeyDeliveryCancelled:      {"order_ref"},
	KeyRideSearchExhausted:    {"pickup"},
	KeyRideStopsChanged:       {"stops", "dest", "fare"},
	KeyRideFareAdjusted:       {"fare", "adjustment"},
	KeyMerchantNewOrder:       {"order_ref", "items", "amount"},
	KeyLostItemReported:       {"item", "ref"},
	KeyLostItemFound:          {"item"},
	KeyLostItemNotFound:       {"item"},
	KeyTicketReply:            {"reference"},
	KeyTicketResolved:         {"reference"},
	KeyStaffTicketOpened:      {"kind", "ref", "who"},
	KeyStaffLostItemAnswered:  {"ref", "who", "answer", "item"},
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
