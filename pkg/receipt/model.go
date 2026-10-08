// Package receipt fabrique les REÇUS et les RELEVÉS que les clients
// téléchargent — en PDF, prêts à être envoyés à un employeur ou à un
// comptable.
//
// ⚠️ AU SOCLE, ET NON DANS CHAQUE VERTICALE. Une course et une commande ne se
// ressemblent pas, mais un reçu, si : un numéro, une date, deux parties, un
// détail de prix qui s'additionne, un moyen de paiement. Deux rendus auraient
// donné deux mises en page, deux façons d'arrondir et deux endroits où oublier
// la monnaie du pays — et c'est précisément sur un reçu qu'un écart se voit et
// se réclame.
//
// ⚠️ LE PAQUET NE CONNAÎT NI COURSE NI COMMANDE, et c'est ce qui le rend
// partageable : chaque verticale remplit un `Document` avec ce qu'elle sait, et
// le socle le DESSINE. Faire l'inverse — un paquet qui saurait lire une course —
// aurait obligé le socle à dépendre d'une verticale.
package receipt

import "time"

// Kind dit ce qu'on imprime. Il change le titre et rien d'autre : la structure
// d'un reçu de course et celle d'un reçu de commande sont la même.
const (
	KindRide      = "ride"
	KindOrder     = "order"
	KindStatement = "statement"
)

// Document est un reçu, tel qu'une verticale le décrit.
type Document struct {
	Kind string
	// Number est ce que le client citera au support. ⚠️ PAS l'identifiant
	// technique entier : personne ne dicte vingt-quatre caractères
	// hexadécimaux au téléphone. Les six derniers suffisent à retrouver une
	// opération, et c'est déjà ce que les écrans affichent.
	Number string
	// IssuedAt est l'instant d'ÉMISSION du document, distinct de la date de
	// l'opération : un reçu retéléchargé six mois plus tard ne raconte pas une
	// autre course, mais il a bien été imprimé ce jour-là.
	IssuedAt time.Time
	// OccurredAt est la date de l'opération elle-même.
	OccurredAt time.Time
	// Country et Currency : la monnaie du PAYS DE L'OPÉRATION.
	//
	// ⚠️ PAS CELLE DU COMPTE. Un passager togolais qui prend une course à
	// Dakar a payé en francs CFA du Sénégal ; à Conakry, en francs guinéens.
	// Un reçu qui formaterait avec la monnaie du compte écrirait le bon nombre
	// derrière le mauvais symbole — et c'est un document qu'on présente.
	Country      string
	CurrencyCode string
	// CurrencySymbol est ce qu'on écrit après le montant (« F CFA », « FG »).
	CurrencySymbol string
	// Decimals : 0 pour les francs, 2 pour le cedi ou le naira, où l'entier
	// stocké est en centièmes.
	Decimals int

	// Parties : qui a payé, qui a servi.
	Parties []Party
	// Journey : le trajet, quand il y en a un. C'est ici que vit la DISTANCE
	// RÉELLEMENT PARCOURUE.
	Journey *Journey
	// Lines est le détail du prix. Il doit s'additionner jusqu'à Total — c'est
	// vérifié avant le rendu, voir `Check`.
	Lines []Line
	// TotalLabel et Total : ce que la personne a payé.
	TotalLabel string
	Total      int
	// Payment : comment, et si l'argent est arrivé.
	Payment Payment
	// Notes : ce qu'il faut savoir en lisant ce document — l'origine de la
	// distance, une remise, une dette. Imprimées en bas, en petit.
	Notes []string

	// Rows et RowCols : les lignes d'un RELEVÉ (plusieurs opérations). Vides
	// sur un reçu unitaire.
	Rows    []Row
	RowCols []Column
	// Period : la fenêtre d'un relevé.
	PeriodFrom, PeriodTo time.Time
	// Summary : les totaux d'un relevé — nombre d'opérations, distance, somme.
	Summary []Line
}

// Party est une partie du document.
type Party struct {
	// Role est dit en clair : « Client », « Chauffeur », « Enseigne ».
	Role string
	Name string
	// Detail : la plaque et le modèle d'un véhicule, le téléphone d'une
	// enseigne — ce qui identifie sans ajouter une ligne.
	//
	// ⚠️ LA VERTICALE DÉCIDE CE QU'ELLE MET ICI, et elle doit le décider avec
	// la politique de confidentialité du pays. Ce paquet imprime ce qu'on lui
	// donne : il ne peut pas savoir qu'un numéro n'avait pas le droit de
	// sortir, et un reçu est un document qui circule.
	Detail string
}

// Journey est le trajet d'une opération.
type Journey struct {
	// Stops sont les points, dans l'ordre parcouru.
	Stops []Stop
	// DistanceM est la distance RÉELLEMENT PARCOURUE, celle du tracé.
	DistanceM int
	// PlannedDistanceM est celle du devis. Servie à côté, jamais à la place :
	// l'écart entre les deux est exactement ce qu'un litige examine.
	PlannedDistanceM int
	// DistanceSource dit d'où vient `DistanceM` : `tracked` (les positions
	// réelles, recalées sur la route) ou `planned` (le devis, quand le suivi
	// n'avait rien).
	//
	// ⚠️ IMPRIMÉ, PAS SOUS-ENTENDU. « 11,4 km » présenté comme mesuré alors
	// qu'il vient d'une estimation est la phrase d'un document qu'on ne peut
	// plus défendre. Voir `SourceNote`.
	DistanceSource string
	// DurationS est le temps passé dans le véhicule.
	DurationS int
	// ApproachDistanceM et ApproachDurationS : ce que l'agent a roulé POUR
	// VENIR. ⚠️ Jamais compté dans `DistanceM` — c'est ce qui permet de dire
	// « la course a fait 8 km » sans y ajouter les 6 km de l'approche.
	ApproachDistanceM int
	ApproachDurationS int
}

// Stop est un point du trajet.
type Stop struct {
	// Label est l'adresse telle que la personne l'a donnée ou choisie.
	Label string
	// Kind : `pickup`, `stop`, `dest` — traduit à l'impression.
	Kind string
	// At : l'heure de passage, quand elle est connue.
	At *time.Time
}

// Line est une ligne de détail de prix.
type Line struct {
	Label string
	// Detail : « 8,2 km × 150 F », « 12 min d'attente » — ce qui explique le
	// montant. C'est la seule chose qui évite la question au support.
	Detail string
	// Amount en plus petite unité. Négatif pour une remise.
	Amount int
	// Strong met la ligne en évidence (un sous-total).
	Strong bool
	// NoAmount : une ligne d'information, sans colonne de montant.
	NoAmount bool
}

// Payment dit comment l'opération a été payée.
type Payment struct {
	// Method : `cash`, `wallet`, `online`, `subscription`…
	Method string
	// Settled dit que l'argent est ARRIVÉ à la plateforme.
	//
	// ⚠️ FAUX N'EST PAS « IMPAYÉ ». Une course en espèces terminée n'est pas
	// réglée au sens de la plateforme — c'est l'agent qui a l'argent —, et le
	// reçu doit dire « payé en espèces » et non « en attente de paiement ». Le
	// rendu s'en charge ; cette nuance est la raison d'être du champ.
	Settled bool
	// Reference : la référence de l'opérateur de paiement, quand il y en a une.
	Reference string
}

// Row est une ligne de RELEVÉ : une opération résumée.
type Row struct {
	Cells []string
	// Amount est la colonne d'argent, formatée par le rendu.
	Amount int
}

// Column décrit une colonne de relevé.
type Column struct {
	Label string
	// Width en millimètres. Zéro = la colonne prend ce qui reste.
	Width float64
	Right bool
}
