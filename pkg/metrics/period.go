// Package metrics porte le VOCABULAIRE des tableaux de bord chiffrés :
// une PÉRIODE demandée par la console, et la forme du rapport qu'on lui rend.
//
// ⚠️ IL EST AU SOCLE EXPRÈS. Les courses et les commandes se mesurent avec
// les mêmes mots — combien, combien ça rapporte, en combien de temps, où,
// quand, qui — et chaque verticale possède ses propres données. Si chacune
// avait inventé sa forme, la console aurait deux écrans de métriques au lieu
// d'un, et « le taux de réussite » n'aurait pas voulu dire la même chose des
// deux côtés.
//
// Ce paquet ne calcule RIEN et ne lit aucune base : il dit ce qu'on demande
// et ce qu'on rend. L'agrégation appartient à qui détient les données.
package metrics

import (
	"net/http"
	"strings"
	"time"
)

// Granularités d'une série temporelle.
const (
	GranHour  = "hour"
	GranDay   = "day"
	GranWeek  = "week"
	GranMonth = "month"
)

// Period est la fenêtre mesurée : un début inclus, une fin EXCLUE, et le pas
// des séries.
//
// ⚠️ Fin EXCLUE, toujours. « Du 1er au 30 » se lit de deux façons et les deux
// se défendent ; un intervalle mi-ouvert n'en a qu'une, et deux périodes
// consécutives ne comptent alors jamais deux fois le même jour.
type Period struct {
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Granularity string    `json:"granularity"`
	// Label nomme le préréglage demandé (`7d`, `month`, `custom`…) — la
	// console le renvoie tel quel dans ses titres.
	Label string `json:"label"`
}

// Duration est la longueur de la fenêtre.
func (p Period) Duration() time.Duration { return p.To.Sub(p.From) }

// Previous rend la période PRÉCÉDENTE de même longueur, celle à laquelle on
// compare.
//
// De même longueur et immédiatement avant : c'est la seule comparaison qui ne
// demande aucune explication. Comparer à « la même période l'an dernier »
// serait parfois plus juste, mais suppose une saisonnalité que ce marché n'a
// pas encore montrée — on ne l'invente pas.
func (p Period) Previous() Period {
	d := p.Duration()
	return Period{From: p.From.Add(-d), To: p.From, Granularity: p.Granularity, Label: p.Label}
}

// Buckets rend les débuts de tranche de la période, dans l'ordre. C'est le
// SQUELETTE des séries : une tranche sans donnée doit valoir zéro, pas
// disparaître — une courbe à trous se lit comme une baisse qui n'a pas eu
// lieu.
func (p Period) Buckets() []time.Time {
	out := make([]time.Time, 0, 64)
	for t := p.Truncate(p.From); t.Before(p.To); t = p.Next(t) {
		out = append(out, t)
		if len(out) > 5000 { // garde-fou : une fenêtre absurde ne noie pas la réponse
			break
		}
	}
	return out
}

// Truncate ramène un instant au début de sa tranche.
func (p Period) Truncate(t time.Time) time.Time {
	t = t.UTC()
	switch p.Granularity {
	case GranHour:
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	case GranWeek:
		// La semaine commence LUNDI : c'est la semaine de travail d'ici, et
		// le dimanche de `time.Weekday` en tête aurait coupé les week-ends
		// en deux.
		d := (int(t.Weekday()) + 6) % 7
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return t.AddDate(0, 0, -d)
	case GranMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
}

// Next rend le début de la tranche suivante.
func (p Period) Next(t time.Time) time.Time {
	switch p.Granularity {
	case GranHour:
		return t.Add(time.Hour)
	case GranWeek:
		return t.AddDate(0, 0, 7)
	case GranMonth:
		return t.AddDate(0, 1, 0)
	default:
		return t.AddDate(0, 0, 1)
	}
}

// presets sont les fenêtres qu'on demande d'un clic, en jours.
var presets = map[string]int{
	"today": 1, "7d": 7, "14d": 14, "30d": 30, "90d": 90, "180d": 180, "365d": 365,
}

// PeriodFromRequest lit la fenêtre demandée.
//
//	?period=7d|30d|…       préréglage (défaut 30d)
//	?from=&to=             bornes ISO 8601 — l'un ou l'autre suffit
//	?granularity=hour|day|week|month
//
// La granularité est CHOISIE POUR L'APPELANT quand il ne la donne pas :
// vingt-quatre points sur une journée, un par jour sur un trimestre, un par
// semaine au-delà. Laisser la console la deviner aurait donné trois cent
// soixante-cinq colonnes larges d'un pixel le jour où quelqu'un demande un an.
//
// ⚠️ Toutes les bornes sont en UTC, comme tout le reste de la plateforme.
// L'Afrique de l'Ouest est à UTC+0 : « aujourd'hui » y est le même jour.
func PeriodFromRequest(r *http.Request) Period {
	q := r.URL.Query()
	now := time.Now().UTC()
	p := Period{Label: strings.TrimSpace(q.Get("period"))}

	from, okFrom := parseTime(q.Get("from"))
	to, okTo := parseTime(q.Get("to"))
	switch {
	case okFrom && okTo:
		p.From, p.To, p.Label = from, to, "custom"
	case okFrom:
		p.From, p.To, p.Label = from, now, "custom"
	case okTo:
		p.From, p.To, p.Label = to.AddDate(0, 0, -30), to, "custom"
	default:
		days, ok := presets[p.Label]
		if !ok {
			days, p.Label = 30, "30d"
		}
		// Fin EXCLUE à demain minuit : la journée en cours COMPTE. Arrêter
		// à l'instant présent aurait fait plonger la dernière colonne de
		// toutes les courbes à chaque consultation du matin.
		p.To = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
		p.From = p.To.AddDate(0, 0, -days)
	}
	if !p.To.After(p.From) {
		p.To = p.From.Add(24 * time.Hour)
	}
	p.Granularity = granularity(q.Get("granularity"), p.Duration())
	return p
}

func granularity(asked string, d time.Duration) string {
	switch asked {
	case GranHour, GranDay, GranWeek, GranMonth:
		return asked
	}
	switch days := d.Hours() / 24; {
	case days <= 2:
		return GranHour
	case days <= 92:
		return GranDay
	case days <= 400:
		return GranWeek
	default:
		return GranMonth
	}
}

func parseTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
