package receipt

// LES MOTS DU DOCUMENT — en français et en anglais.
//
// ⚠️ ICI ET NON DANS `pkg/i18n`. Ce dernier traduit les REFUS de l'API, chargés
// depuis les fichiers `locales/` de chaque service : un paquet de rendu qui en
// dépendrait ne pourrait plus dessiner une page sans qu'un traducteur soit
// branché, et c'est exactement ce qui arrive dans un test ou dans une tâche de
// fond. Une table de vingt mots embarquée rend le paquet utilisable seul.
//
// ⚠️ ET LES MOTS DE STRUCTURE SEULEMENT. Le libellé d'une ligne de prix
// (« Attente », « Supplément nuit ») vient de la VERTICALE, déjà traduit : elle
// seule sait ce qu'elle facture. Ce paquet ne traduit que ce qu'il imprime
// lui-même — les en-têtes, « Total », les types d'arrêt.

type labels struct {
	receiptRide, receiptOrder, statement string
	issuedAt, number, operationOf        string
	journey, distance, planned, duration string
	approach, source, tracked, estimated string
	pickup, stop, dest                   string
	detail, amount, total                string
	payment, paid, pendingPayment, cash  string
	period, operations, totalDistance    string
	page, generatedBy                    string
}

var dict = map[string]labels{
	"fr": {
		receiptRide: "Reçu de course", receiptOrder: "Reçu de commande",
		statement: "Relevé d'activité",
		issuedAt:  "Émis le", number: "Reçu n°", operationOf: "Opération du",
		journey: "Trajet", distance: "Distance parcourue", planned: "estimée au devis",
		duration: "Durée", approach: "Approche du chauffeur",
		source: "Source", tracked: "mesurée sur le trajet réel",
		estimated: "estimée — le suivi n'a rien enregistré",
		pickup:    "Départ", stop: "Arrêt", dest: "Arrivée",
		detail: "Détail", amount: "Montant", total: "Total",
		payment: "Paiement", paid: "payé", pendingPayment: "en attente de paiement",
		cash:   "payé en espèces au chauffeur",
		period: "Période", operations: "Opérations", totalDistance: "Distance totale",
		page: "Page", generatedBy: "Document émis par Dira",
	},
	"en": {
		receiptRide: "Ride receipt", receiptOrder: "Order receipt",
		statement: "Activity statement",
		issuedAt:  "Issued on", number: "Receipt no.", operationOf: "Operation of",
		journey: "Journey", distance: "Distance travelled", planned: "quoted estimate",
		duration: "Duration", approach: "Driver approach",
		source: "Source", tracked: "measured on the actual route",
		estimated: "estimated — tracking recorded nothing",
		pickup:    "Pickup", stop: "Stop", dest: "Drop-off",
		detail: "Detail", amount: "Amount", total: "Total",
		payment: "Payment", paid: "paid", pendingPayment: "awaiting payment",
		cash:   "paid in cash to the driver",
		period: "Period", operations: "Operations", totalDistance: "Total distance",
		page: "Page", generatedBy: "Document issued by Dira",
	},
}

// lang rend les mots d'une langue, le français par défaut.
//
// ⚠️ LE FRANÇAIS ET NON L'ANGLAIS EN REPLI, contrairement à l'habitude des
// bibliothèques. Les cinq pays ouverts sont francophones : un reçu servi en
// anglais à Lomé parce qu'un en-tête manquait serait un document inutilisable
// pour celui qui le présente.
func lang(code string) labels {
	if l, ok := dict[code]; ok {
		return l
	}
	if len(code) > 2 {
		if l, ok := dict[code[:2]]; ok {
			return l
		}
	}
	return dict["fr"]
}

// SourceNote rend la phrase qui dit D'OÙ VIENT la distance imprimée.
//
// ⚠️ ELLE N'EST JAMAIS OMISE SUR UN DOCUMENT QUI PORTE UNE DISTANCE. « 11,4 km »
// présenté comme mesuré alors qu'il vient d'une estimation est la phrase d'un
// reçu qu'on ne peut plus défendre devant une réclamation — et c'est le cas
// courant, pas le cas rare : il suffit que le téléphone du chauffeur ait perdu
// le réseau.
func SourceNote(source, locale string) string {
	l := lang(locale)
	if source == "tracked" {
		return l.distance + " : " + l.tracked
	}
	return l.distance + " : " + l.estimated
}
