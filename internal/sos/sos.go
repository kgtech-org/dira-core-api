// Package sos porte LE BOUTON D'ALERTE : un chauffeur, un livreur ou un
// passager qui est en danger, et l'exploitation qui doit le savoir dans la
// seconde.
//
// ⚠️ CE MODULE N'A QU'UNE RÈGLE, ET TOUT LE RESTE EN DÉCOULE : UNE ALERTE NE SE
// PERD JAMAIS. Pas pour une position manquante, pas pour une course introuvable,
// pas pour un champ mal formé, pas pour un double appui. Partout ailleurs dans
// cette base, refuser une requête incomplète est la bonne réponse ; ici c'est la
// pire. Quelqu'un qui appuie sur ce bouton n'a pas le temps de corriger un
// formulaire, et un `422` sur un appel au secours est indéfendable.
//
// C'est pourquoi :
//   - AUCUN CHAMP N'EST OBLIGATOIRE. Pas même la position. « On ne sait pas où
//     il est » reste une alarme — et c'est justement celle qu'il faut traiter en
//     premier, pas celle qu'il faut jeter.
//   - L'ÉCRITURE DE L'ALERTE EST SÉPARÉE DE TOUT LE RESTE. Prévenir le staff,
//     écrire au journal d'audit, résoudre un nom : chacun peut échouer, aucun ne
//     fait échouer l'alerte.
//   - UN SEUL APPEL OUVERT PAR PERSONNE. Quelqu'un qui panique appuie cinq fois.
//     Cinq alertes dans la file, c'est quatre que l'exploitation croit être
//     d'autres gens — et pendant qu'elle les trie, personne ne va sur place.
//
// ⚠️ UNE FILE À PART, ALORS QU'ON VIENT DE DÉFENDRE L'INVERSE POUR LES
// INCIDENTS D'ANNULATION. La règle « une seule file, sinon la seconde se
// surveille moins » est bonne, et elle ne s'applique pas ici : un ticket de
// support est une CONVERSATION qui se traite dans l'heure ; une alerte SOS est
// une ALARME qui se traite dans la minute. Mêlées, l'alarme attend derrière un
// objet oublié dans un coffre. La file du support reste donc la file du
// support — et c'est l'alerte qui, en se refermant, peut y laisser le suivi.
package sos

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection est la collection MongoDB des alertes.
const Collection = "sos_alerts"

// CE QUI A DÉCLENCHÉ L'ALERTE.
//
// ⚠️ LA SOURCE EST GARDÉE PARCE QU'ELLE CHANGE LA LECTURE. Un bouton pressé est
// une décision ; un choc détecté est une mesure, et une mesure se trompe. Les
// confondre ferait traiter un nid-de-poule comme une agression — ou, bien pire,
// l'inverse : un opérateur qui a appris que « la moitié sont des faux » finit
// par ouvrir le vrai avec deux minutes de retard.
const (
	// SourceButton : la personne a appuyé. Rien à interpréter.
	SourceButton = "button"
	// SourceShake : elle a secoué l'appareil.
	//
	// ⚠️ LA SECOUSSE EXISTE POUR LE CAS OÙ ON NE PEUT PAS REGARDER L'ÉCRAN.
	// Téléphone dans une poche, main sur le volant, quelqu'un à côté qui ne doit
	// pas voir : c'est précisément quand viser un bouton est impossible qu'on en
	// a le plus besoin.
	SourceShake = "shake"
	// SourceCrash : l'appareil a mesuré un choc violent.
	SourceCrash = "crash"
	// SourceVoice : un mot-clé prononcé.
	SourceVoice = "voice"
)

// Sources est la liste admise, dans l'ordre où la console les montre.
var Sources = []string{SourceButton, SourceShake, SourceCrash, SourceVoice}

// ValidSource dit si la source est connue. Vide = `button` (voir Normalise).
func ValidSource(s string) bool {
	for _, k := range Sources {
		if k == s {
			return true
		}
	}
	return false
}

// Detected dit si la source est une MESURE et non un geste — ce qui décide
// qu'une confirmation humaine était attendue.
func Detected(s string) bool {
	return s == SourceShake || s == SourceCrash || s == SourceVoice
}

// L'ÉTAT D'UNE ALERTE. Trois, pas plus : qui est seul, qui est pris, qui est
// fini.
const (
	// StatusOpen : personne ne s'en occupe encore. C'est l'état qui doit faire
	// du bruit.
	StatusOpen = "open"
	// StatusAcknowledged : un opérateur NOMMÉ l'a prise.
	//
	// ⚠️ IL EXISTE POUR QUE DEUX OPÉRATEURS N'APPELLENT PAS LE MÊME CHAUFFEUR
	// PENDANT QUE LE SUIVANT N'EST APPELÉ PAR PERSONNE. Sans cet état, une file
	// de trois alertes et deux opérateurs se traite deux fois et pas une
	// troisième.
	StatusAcknowledged = "acknowledged"
	// StatusClosed : terminé, avec un dénouement.
	StatusClosed = "closed"
)

// LE DÉNOUEMENT, à la fermeture.
//
// ⚠️ IL EST EXIGÉ, ET C'EST LE SEUL CHAMP DE CE MODULE QUI LE SOIT. Une alerte
// fermée sans dénouement ne dit pas si quelqu'un a été secouru : on ne peut ni
// compter les vraies, ni mesurer les fausses, ni voir qu'un même chauffeur en
// déclenche une par semaine. Fermer, c'est répondre.
const (
	// OutcomeReal : il se passait quelque chose.
	OutcomeReal = "real"
	// OutcomeFalseAlarm : fausse alerte.
	OutcomeFalseAlarm = "false_alarm"
	// OutcomeUnreachable : on n'a pas pu joindre la personne.
	//
	// ⚠️ CE N'EST PAS UNE FAUSSE ALERTE, et les ranger ensemble serait le pire
	// mensonge de cette liste : « injoignable » est le dénouement le plus
	// inquiétant de tous. Il se compte à part.
	OutcomeUnreachable = "unreachable"
	// OutcomeTest : un essai, une démonstration, une formation.
	OutcomeTest = "test"
)

// Outcomes est la liste admise.
var Outcomes = []string{OutcomeReal, OutcomeFalseAlarm, OutcomeUnreachable, OutcomeTest}

// ValidOutcome dit si le dénouement est connu.
func ValidOutcome(o string) bool {
	for _, k := range Outcomes {
		if k == o {
			return true
		}
	}
	return false
}

// QUI A FERMÉ.
const (
	ClosedByStaff = "staff"
	// ClosedByRaiser : la personne elle-même, « fausse alerte ».
	//
	// ⚠️ ET CE N'EST PAS UNE RAISON DE FAIRE DISPARAÎTRE L'ALERTE DE L'ÉCRAN.
	// Quelqu'un peut être CONTRAINT d'annuler — c'est le scénario même qu'on
	// cherche à couvrir. Une annulation quatre secondes après le déclenchement
	// est un signal, pas un non-événement : la console garde les annulées
	// récentes à l'écran (voir `RecentlyCancelledFor`).
	ClosedByRaiser = "raiser"
)

// Position est un point du parcours d'une alerte.
type Position struct {
	Lat       float64   `bson:"lat" json:"lat"`
	Lng       float64   `bson:"lng" json:"lng"`
	AccuracyM int       `bson:"accuracy_m,omitempty" json:"accuracy_m,omitempty"`
	At        time.Time `bson:"at" json:"at"`
}

// TrailMax borne le parcours gardé.
//
// ⚠️ ON GARDE LES DERNIERS, PAS LES PREMIERS. Ce qu'un opérateur cherche, c'est
// OÙ IL EST MAINTENANT ; l'historique n'est utile qu'après. Le point de
// DÉCLENCHEMENT, lui, est gardé à part (`RaisedAt`) et ne peut pas être poussé
// dehors par le défilement — c'est le seul point dont on soit sûr qu'il
// corresponde au moment du danger.
const TrailMax = 300

// Alert est UN appel au secours.
type Alert struct {
	ID     primitive.ObjectID `bson:"_id,omitempty"`
	UserID primitive.ObjectID `bson:"user_id"`
	Status string             `bson:"status"`
	Source string             `bson:"source"`
	// Confirmed : un humain a-t-il confirmé ?
	//
	// ⚠️ LE PIÈGE DE CE CHAMP, ET IL FAUT LE LIRE À L'ENVERS : `false` sur une
	// alerte DÉTECTÉE est PLUS grave, pas moins. Le compte à rebours s'est
	// écoulé sans que personne ne l'annule — après un choc violent, « personne
	// n'a annulé » veut souvent dire « personne ne POUVAIT annuler ». Une
	// console qui trierait les non confirmées en bas de la file mettrait
	// systématiquement les accidents les plus graves en dernier.
	Confirmed bool `bson:"confirmed"`
	// Country : le pays de l'OPÉRATION, posé à l'écriture. Il borne la file de
	// la console.
	Country string `bson:"country,omitempty"`
	// Vertical, RideID, DeliveryID : l'opération en cours, quand il y en a une.
	//
	// ⚠️ LE SOCLE NE LES RÉSOUT PAS, ET C'EST VOULU. Il ne peut pas appeler une
	// verticale (les annonces vont dans l'autre sens), et surtout il n'a pas à
	// le faire : c'est la CONSOLE qui compose, comme elle compose déjà le suivi
	// en direct depuis trois services. Ici on garde la référence — c'est elle
	// qui ouvre la course en un clic, et c'est tout ce dont un opérateur a
	// besoin.
	Vertical   string `bson:"vertical,omitempty"`
	RideID     string `bson:"ride_id,omitempty"`
	DeliveryID string `bson:"delivery_id,omitempty"`
	// Note : ce que la personne a tapé, si elle a eu le temps.
	Note string `bson:"note,omitempty"`
	// RaisedPos : le point du DÉCLENCHEMENT. Immuable.
	RaisedPos *Position  `bson:"raised_pos,omitempty"`
	Trail     []Position `bson:"trail,omitempty"`
	// Battery : la batterie au déclenchement, en pourcentage.
	//
	// ⚠️ UN DÉTAIL QUI DÉCIDE D'UN GESTE : à 4 %, l'opérateur sait que le
	// téléphone va s'éteindre et qu'il faut appeler MAINTENANT plutôt
	// qu'attendre une position plus précise. Zéro = non dit.
	Battery int `bson:"battery,omitempty"`

	CreatedAt      time.Time           `bson:"created_at"`
	UpdatedAt      time.Time           `bson:"updated_at"`
	AcknowledgedAt *time.Time          `bson:"acknowledged_at,omitempty"`
	AcknowledgedBy *primitive.ObjectID `bson:"acknowledged_by,omitempty"`
	ClosedAt       *time.Time          `bson:"closed_at,omitempty"`
	ClosedBy       string              `bson:"closed_by,omitempty"`
	ClosedByID     *primitive.ObjectID `bson:"closed_by_id,omitempty"`
	Outcome        string              `bson:"outcome,omitempty"`
	Resolution     string              `bson:"resolution,omitempty"`
}

// RecentlyCancelledFor : combien de temps une alerte annulée PAR LA PERSONNE
// reste dans la file de la console.
//
// ⚠️ QUINZE MINUTES, ET NON ZÉRO. Une annulation peut être contrainte, et c'est
// le scénario que ce bouton existe pour couvrir. Les faire disparaître aurait
// rendu invisible exactement le cas le plus grave. Quinze minutes laissent le
// temps d'un appel de vérification sans encombrer l'écran.
const RecentlyCancelledFor = 15 * time.Minute

// Normalise met une alerte en forme SANS JAMAIS LA REFUSER.
//
// ⚠️ C'EST LA FONCTION CENTRALE DE CE FICHIER. Tout ce qu'une application peut
// envoyer de bancal — source inconnue, position à zéro, coordonnées
// impossibles, batterie à 300, note de dix mille signes — est corrigé ici, pas
// rejeté. Un appel au secours mal formé reste un appel au secours.
func Normalise(a *Alert) {
	a.Source = strings.ToLower(strings.TrimSpace(a.Source))
	if !ValidSource(a.Source) {
		// Une source inconnue devient `button` : c'est le cas le moins
		// interprétable, donc celui qui ne fait aucune promesse. L'inverse —
		// garder le mot inconnu — aurait fait passer l'alerte à travers tous
		// les comptages par source sans qu'on le voie.
		a.Source = SourceButton
	}
	// ⚠️ UN GESTE EST TOUJOURS CONFIRMÉ, quoi que dise l'application. Un bouton
	// pressé EST la confirmation ; accepter `confirmed: false` dessus aurait
	// laissé un client mal câblé faire passer de vraies alertes pour des
	// mesures que personne n'a validées.
	if !Detected(a.Source) {
		a.Confirmed = true
	}
	a.Vertical = strings.ToLower(strings.TrimSpace(a.Vertical))
	if a.Vertical != "vtc" && a.Vertical != "food" {
		a.Vertical = ""
	}
	a.Note = truncate(strings.TrimSpace(a.Note), 1000)
	if a.Battery < 0 || a.Battery > 100 {
		a.Battery = 0
	}
	if a.RaisedPos != nil && !Plausible(a.RaisedPos.Lat, a.RaisedPos.Lng) {
		// ⚠️ ON JETTE LA POSITION, PAS L'ALERTE. Un `0,0` au large du Ghana est
		// la valeur par défaut d'un capteur qui n'a pas encore fixé : l'afficher
		// enverrait un opérateur regarder le golfe de Guinée, ce qui est pire
		// que « position inconnue ».
		a.RaisedPos = nil
	}
}

// Plausible écarte les coordonnées qui ne désignent rien.
//
// ⚠️ ON NE VÉRIFIE PAS QUE LE POINT EST DANS LE PAYS, et c'est délibéré :
// quelqu'un peut être en danger juste après une frontière, et refuser sa
// position parce qu'elle tombe au Bénin serait absurde. On n'écarte que ce qui
// n'est pas une coordonnée — et le `0,0` du capteur qui n'a pas encore fixé,
// dont aucune de nos villes n'approche.
func Plausible(lat, lng float64) bool {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return false
	}
	return lat != 0 || lng != 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Découpe sur une frontière de rune : couper au milieu d'un caractère
	// accentué produirait du texte invalide dans la file de l'exploitation.
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}
