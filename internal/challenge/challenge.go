// Package challenge porte LES OBJECTIFS À ATTEINDRE : « faites 20 courses cette
// semaine, gagnez 5 000 F ». Pour les chauffeurs, les livreurs et les clients.
//
// ⚠️ IL NE S'APPELLE PAS `campaign`, ET C'EST DÉLIBÉRÉ. `notify.Campaign`
// existe déjà et désigne un ENVOI DE MESSAGES — « informer », pas
// « récompenser ». Deux choses nommées pareil dans la même base finissent par
// être confondues par quelqu'un qui cherche « la campagne de la semaine » et
// tombe sur un gabarit de notification. Un objectif se LANCE périodiquement
// comme une campagne, mais il coûte de l'argent et se gagne : c'est un autre
// objet.
//
// ⚠️ ET IL EST AU SOCLE, parce que les trois publics y sont. Un chauffeur
// (courses), un livreur (livraisons) et un client (commandes ET courses)
// participent aux mêmes opérations ; le bonus sort du même argent, et
// l'exploitation doit pouvoir répondre à « combien devons-nous en bonus ce
// mois-ci ? » en UN endroit. Deux implémentations auraient donné deux façons de
// compter — et on ne l'aurait découvert qu'en additionnant deux rapports qui ne
// tombent pas juste.
//
// L'ENVELOPPE est celle de `pkg/promo` : budget, nombre de gagnants, limite par
// personne. Ce paquet-là existe précisément pour que l'argent d'une opération
// commerciale se compte d'une seule façon ; en écrire une seconde ici aurait
// été la première erreur.
package challenge

import (
	"strings"
	"time"
)

// Collection est la collection MongoDB des objectifs.
const Collection = "challenges"

// CollectionProgress est celle des avancements — une ligne par personne et par
// objectif.
//
// ⚠️ UNE COLLECTION À PART, et non un tableau dans l'objectif. Un objectif
// national peut avoir dix mille participants : les porter dans le document
// l'aurait fait grossir sans limite, et chaque lecture de la liste des
// objectifs aurait traîné dix mille lignes d'avancement.
const CollectionProgress = "challenge_progress"

// QUI PARTICIPE.
//
// ⚠️ TROIS PUBLICS, ET LE PUBLIC DÉCIDE DE TOUT LE RESTE : ce qu'on peut
// compter, où va le bonus, et ce qui est dangereux à demander. Un objectif sans
// public serait un objectif dont on ne sait ni qui le voit ni comment on le
// paie.
const (
	// ForDriver : un chauffeur VTC.
	ForDriver = "driver"
	// ForCourier : un agent de livraison.
	ForCourier = "courier"
	// ForClient : un client — des DEUX métiers.
	ForClient = "client"
)

var audiences = []string{ForDriver, ForCourier, ForClient}

// CE QU'ON COMPTE.
//
// ⚠️ LA LISTE EST COURTE, ET SON ABSENCES COMPTENT AUTANT QUE SA CONTENANCE.
// Lisez `Forbidden` plus bas avant d'en ajouter : trois mesures évidentes ont
// été écartées parce qu'elles achètent exactement ce qu'on ne veut pas acheter.
const (
	// MetricRidesDone : courses TERMINÉES par un chauffeur.
	MetricRidesDone = "rides_done"
	// MetricDeliveriesDone : livraisons TERMINÉES par un livreur.
	MetricDeliveriesDone = "deliveries_done"
	// MetricDaysActive : jours où la personne a travaillé — au moins une
	// course ou une livraison terminée.
	//
	// ⚠️ C'EST LA MESURE LA PLUS SÛRE DU LOT, et celle qu'il faut proposer en
	// premier. Elle récompense la RÉGULARITÉ, pas le volume : « travaillez cinq
	// jours cette semaine » ne pousse personne à enchaîner quatorze heures,
	// alors que « faites 40 courses » pousse à les faire le dimanche soir en
	// étant épuisé.
	MetricDaysActive = "days_active"
	// MetricRidesTaken : courses prises par un CLIENT.
	MetricRidesTaken = "rides_taken"
	// MetricOrdersPlaced : commandes passées par un CLIENT.
	MetricOrdersPlaced = "orders_placed"
	// MetricAmountSpent : francs dépensés par un CLIENT, les deux métiers
	// confondus.
	MetricAmountSpent = "amount_spent"
)

// Metric décrit ce qu'une mesure compte, et pour qui.
type Metric struct {
	Key string
	// Label, en français — la langue de l'exploitation.
	Label string
	// For : les publics qui peuvent en recevoir un objectif.
	For []string
	// Money : la cible est un MONTANT, pas un compte. ⚠️ Change l'affichage
	// (« 20 000 F » et non « 20 000 fois ») et la validation.
	Money bool
	// MaxPerDay borne ce qu'on peut DEMANDER par jour de fenêtre — voir
	// `CheckTarget`. Zéro = pas de borne (une mesure sans risque humain).
	MaxPerDay int
}

// Metrics est la liste admise, dans l'ordre où la console les propose : la plus
// sûre en premier.
var Metrics = []Metric{
	{Key: MetricDaysActive, Label: "Jours travaillés", For: []string{ForDriver, ForCourier}, MaxPerDay: 1},
	{Key: MetricRidesDone, Label: "Courses terminées", For: []string{ForDriver}, MaxPerDay: 12},
	{Key: MetricDeliveriesDone, Label: "Livraisons terminées", For: []string{ForCourier}, MaxPerDay: 20},
	{Key: MetricRidesTaken, Label: "Courses prises", For: []string{ForClient}},
	{Key: MetricOrdersPlaced, Label: "Commandes passées", For: []string{ForClient}},
	{Key: MetricAmountSpent, Label: "Montant dépensé", For: []string{ForClient}, Money: true},
}

// Forbidden est CE QU'ON A REFUSÉ DE MESURER, avec la raison.
//
// ⚠️ ELLE EST DANS LE CODE, ET PAS DANS UN COMPTE RENDU DE RÉUNION. La question
// « pourquoi ne peut-on pas faire un objectif sur le taux d'acceptation ? »
// reviendra — elle revient toujours —, et la réponse doit être là où quelqu'un
// la cherche. Chacune de ces trois mesures est techniquement disponible : c'est
// un refus de PRODUIT, pas un manque.
var Forbidden = map[string]string{
	// Les heures en ligne SONT journalisées (`online_s`). Un objectif dessus
	// paierait quelqu'un pour garer sa voiture avec l'application ouverte — et,
	// pire, le laisserait dans le vivier sans vouloir de course : les appels
	// partiraient vers lui et mourraient sans réponse.
	"hours_online": "récompenser des heures paie l'attente, pas le travail — " +
		"et remplit le vivier de gens qui ne veulent pas de course",
	// Le taux d'acceptation est calculé depuis la 4.55.0. Un bonus dessus
	// achèterait les refus qu'on veut qu'un chauffeur se permette : décliner
	// parce qu'il est fatigué, parce que le départ est mal éclairé, parce que
	// le passager l'inquiète. On ne met pas de prime sur « ne dis jamais non ».
	"acceptance_rate": "mettre une prime sur « ne jamais refuser » achète les " +
		"refus qu'on veut qu'un chauffeur se permette (fatigue, départ peu sûr)",
	// La durée et la vitesse sont mesurées (`actual_duration_s`). Un objectif
	// dessus paierait la conduite rapide. Il n'y a pas de version prudente de
	// cette mesure.
	"fastest_rides": "payer la vitesse tue des gens ; il n'existe pas de " +
		"version prudente de cette mesure",
}

// LookupMetric rend une mesure connue.
func LookupMetric(key string) (Metric, bool) {
	for _, m := range Metrics {
		if m.Key == key {
			return m, true
		}
	}
	return Metric{}, false
}

// MetricsFor rend les mesures proposables à un public.
//
// ⚠️ DEUX LISTES PARCE QUE CE NE SONT PAS LES MÊMES FAITS. Proposer « courses
// terminées » à un client est absurde, et « montant dépensé » à un chauffeur
// l'est autant — il en encaisse, il n'en dépense pas. Une liste unique aurait
// fait un écran où les trois quarts des choix sont impossibles.
func MetricsFor(audience string) []Metric {
	out := make([]Metric, 0, len(Metrics))
	for _, m := range Metrics {
		for _, a := range m.For {
			if a == audience {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// ValidAudience dit si le public est connu.
func ValidAudience(a string) bool {
	for _, v := range audiences {
		if v == a {
			return true
		}
	}
	return false
}

// LA RÉCURRENCE — « une campagne périodique lancée ».
const (
	// RepeatNone : une seule fois, sur la fenêtre donnée.
	RepeatNone    = "none"
	RepeatWeekly  = "weekly"
	RepeatMonthly = "monthly"
)

var repeats = []string{RepeatNone, RepeatWeekly, RepeatMonthly}

// ValidRepeat dit si la récurrence est connue.
func ValidRepeat(r string) bool {
	for _, v := range repeats {
		if v == r {
			return true
		}
	}
	return false
}

// L'ÉTAT d'un objectif.
const (
	// StatusDraft : écrit, pas encore lancé. Personne ne le voit.
	StatusDraft = "draft"
	// StatusLive : lancé. Il compte, il s'affiche, il paie.
	StatusLive = "live"
	// StatusEnded : la fenêtre est passée, ou l'exploitation l'a arrêté.
	//
	// ⚠️ ARRÊTER N'EFFACE PAS CE QUI EST GAGNÉ. Quelqu'un qui a atteint la
	// cible avant l'arrêt a droit à son bonus : le lui retirer parce qu'on a
	// coupé l'offre serait indéfendable, et c'est le genre de décision qui se
	// sait en une journée.
	StatusEnded = "ended"
)

// Window est la fenêtre d'une OCCURRENCE.
type Window struct {
	From time.Time `bson:"from" json:"from"`
	To   time.Time `bson:"to" json:"to"`
}

// Days rend le nombre de jours de la fenêtre, au moins 1.
func (w Window) Days() int {
	d := int(w.To.Sub(w.From).Hours() / 24)
	if d < 1 {
		return 1
	}
	return d
}

// Contains dit si un instant tombe dans la fenêtre.
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.From) && t.Before(w.To)
}

// MaxDays borne la fenêtre d'une occurrence.
//
// ⚠️ QUATRE-VINGT-DIX JOURS, et c'est une borne de PRODUIT. Un objectif qui
// court six mois n'est plus un objectif : personne ne se souvient de ce qu'il
// doit faire, l'avancement n'avance visiblement plus, et la ligne de bonus
// qu'on provisionne vieillit dans les comptes. Au-delà, c'est un programme de
// fidélité — un autre objet.
const MaxDays = 90

// Normalise met un objectif en forme, sans juger de sa validité.
func Normalise(c *Challenge) {
	c.Audience = strings.ToLower(strings.TrimSpace(c.Audience))
	c.Metric = strings.ToLower(strings.TrimSpace(c.Metric))
	c.Repeat = strings.ToLower(strings.TrimSpace(c.Repeat))
	if c.Repeat == "" {
		c.Repeat = RepeatNone
	}
	c.Title = strings.TrimSpace(c.Title)
	c.Description = strings.TrimSpace(c.Description)
	if c.Status == "" {
		c.Status = StatusDraft
	}
}
