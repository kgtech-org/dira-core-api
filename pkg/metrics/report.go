package metrics

import (
	"math"
	"sort"
	"time"
)

// Unit dit COMMENT lire un nombre. La console formate d'après elle — une
// valeur nue laisserait deviner si 4500 est un montant, une minute ou un
// pourcentage, et quelqu'un se tromperait.
const (
	UnitCount   = "count"   // entier simple
	UnitMoney   = "money"   // plus petite unité monétaire, devise dans le rapport
	UnitPercent = "percent" // 0..100
	UnitMinutes = "minutes"
	UnitKm      = "km"
	UnitRating  = "rating" // 0..5
)

// KPI est UN chiffre de tête, avec celui de la période précédente à côté.
//
// ⚠️ LA COMPARAISON EST SERVIE, PAS CALCULÉE PAR LA CONSOLE. Le sens d'une
// variation dépend de l'indicateur : +10 % de commandes est bon, +10 %
// d'annulations ne l'est pas. `HigherIsBetter` le dit ici, une fois, plutôt
// que dans une table de correspondance côté navigateur que personne ne
// penserait à compléter en ajoutant un indicateur.
type KPI struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	// Previous est la même mesure sur la période précédente. ABSENT quand
	// elle n'a pas de sens (un effectif à l'instant T, par exemple) : mieux
	// vaut pas de flèche qu'une flèche fausse.
	Previous *float64 `json:"previous,omitempty"`
	// HigherIsBetter oriente la couleur de la variation.
	HigherIsBetter bool `json:"higher_is_better"`
	// Hint est la précision qu'on lit sous le chiffre quand elle existe
	// (« objectif 95 % », « 128 sur 134 »).
	Hint string `json:"hint,omitempty"`
}

// Delta rend la variation en pourcentage, et si elle est calculable.
func (k KPI) Delta() (float64, bool) {
	if k.Previous == nil || *k.Previous == 0 {
		return 0, false
	}
	return (k.Value - *k.Previous) / math.Abs(*k.Previous) * 100, true
}

// Point est une tranche de temps et ce qu'on y a mesuré.
type Point struct {
	T time.Time `json:"t"`
	V float64   `json:"v"`
}

// Series est une courbe. Plusieurs séries d'un même `Stack` se superposent.
type Series struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	// Kind dit comment la dessiner : `line`, `area`, `bar`.
	Kind   string  `json:"kind"`
	Color  string  `json:"color,omitempty"`
	Points []Point `json:"points"`
}

// Slice est une part d'une répartition — un statut, un moyen de paiement,
// une ville, une heure de la journée.
type Slice struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	// Secondary porte la seconde mesure quand la répartition en a une
	// (le montant à côté du nombre, par exemple).
	Secondary float64 `json:"secondary,omitempty"`
}

// Breakdown est une répartition nommée.
type Breakdown struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	// SecondaryUnit accompagne `Slice.Secondary` quand elle est servie.
	SecondaryUnit string  `json:"secondary_unit,omitempty"`
	Items         []Slice `json:"items"`
	// Total est la somme servie à part : une répartition tronquée aux dix
	// premiers ne se somme pas, et un pourcentage calculé sur ce qu'on voit
	// mentirait.
	Total float64 `json:"total"`
}

// Ranked est une ligne de classement — une enseigne, un chauffeur, un client.
type Ranked struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Sub    string  `json:"sub,omitempty"`
	Value  float64 `json:"value"`
	Amount float64 `json:"amount,omitempty"`
	// Rating et RatingCount vont ensemble, comme partout ailleurs.
	Rating      float64 `json:"rating,omitempty"`
	RatingCount int     `json:"rating_count,omitempty"`
}

// Leaderboard est un classement nommé.
type Leaderboard struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Unit       string   `json:"unit"`
	AmountUnit string   `json:"amount_unit,omitempty"`
	Items      []Ranked `json:"items"`
}

// Heatmap est la demande par JOUR DE SEMAINE × HEURE : sept lignes de
// vingt-quatre. C'est la seule vue qui répond à « quand faut-il des
// livreurs ? » — une courbe par jour l'aurait noyée dans sept traits.
type Heatmap struct {
	Key    string      `json:"key"`
	Label  string      `json:"label"`
	Rows   []string    `json:"rows"`
	Cols   []string    `json:"cols"`
	Values [][]float64 `json:"values"`
	Max    float64     `json:"max"`
}

// Report est ce que la console reçoit. Les deux verticales servent CETTE
// forme, et la console n'a qu'un écran.
//
// Les sections sont des LISTES, pas des champs fixes : une verticale qui n'a
// rien à dire sur les moyens de paiement n'en sert pas, et la console n'a
// aucun panneau vide à afficher. Ce qui n'est pas servi n'est pas dessiné.
type Report struct {
	Vertical string `json:"vertical"`
	Period   Period `json:"period"`
	Previous Period `json:"previous"`
	Currency string `json:"currency"`
	// GeneratedAt : une métrique se lit à l'heure où elle a été calculée.
	GeneratedAt  time.Time     `json:"generated_at"`
	KPIs         []KPI         `json:"kpis"`
	Series       []Series      `json:"series"`
	Breakdowns   []Breakdown   `json:"breakdowns"`
	Leaderboards []Leaderboard `json:"leaderboards"`
	Heatmaps     []Heatmap     `json:"heatmaps"`
	// Notes dit ce qui MANQUE et pourquoi, plutôt que de laisser croire à un
	// zéro. « Les notes ne sont pas encore servies » vaut mieux qu'une
	// moyenne de 0,0.
	Notes []string `json:"notes,omitempty"`
}

// Fill remplit une série sur le squelette de la période : une tranche sans
// donnée vaut ZÉRO et reste à sa place.
func Fill(p Period, byBucket map[time.Time]float64) []Point {
	buckets := p.Buckets()
	out := make([]Point, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, Point{T: b, V: byBucket[b]})
	}
	return out
}

// TopN trie décroissant et coupe. Le reste est REGROUPÉ sous « Autres »
// quand il reste quelque chose : un classement dont la somme ne fait pas le
// total invite à soustraire de tête, et c'est là qu'on se trompe.
func TopN(items []Slice, n int, otherLabel string) []Slice {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Value > items[j].Value })
	if len(items) <= n {
		return items
	}
	rest := Slice{Key: "other", Label: otherLabel}
	for _, s := range items[n:] {
		rest.Value += s.Value
		rest.Secondary += s.Secondary
	}
	out := append([]Slice{}, items[:n]...)
	if rest.Value > 0 {
		out = append(out, rest)
	}
	return out
}

// Sum additionne les parts d'une répartition.
func Sum(items []Slice) float64 {
	var total float64
	for _, s := range items {
		total += s.Value
	}
	return total
}

// Ratio rend a/b en pourcentage, zéro quand b vaut zéro. Un taux sur zéro
// course n'est pas 100 %, ni une division par zéro : c'est l'absence de taux.
func Ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b * 100
}

// Prev est le raccourci de la comparaison servie.
func Prev(v float64) *float64 { return &v }

// Weekdays nomme les lignes d'une carte de chaleur, lundi d'abord.
var Weekdays = []string{"lun", "mar", "mer", "jeu", "ven", "sam", "dim"}

// Hours nomme ses colonnes.
func Hours() []string {
	out := make([]string, 24)
	for h := range out {
		out[h] = time.Date(2000, 1, 1, h, 0, 0, 0, time.UTC).Format("15")
	}
	return out
}

// NewHeatmap rend une carte de chaleur vide, prête à être remplie.
func NewHeatmap(key, label string) Heatmap {
	values := make([][]float64, len(Weekdays))
	for i := range values {
		values[i] = make([]float64, 24)
	}
	return Heatmap{Key: key, Label: label, Rows: Weekdays, Cols: Hours(), Values: values}
}

// Add pose une mesure dans la case du jour et de l'heure (UTC).
func (h *Heatmap) Add(t time.Time, v float64) {
	t = t.UTC()
	row := (int(t.Weekday()) + 6) % 7
	col := t.Hour()
	h.Values[row][col] += v
	if h.Values[row][col] > h.Max {
		h.Max = h.Values[row][col]
	}
}
