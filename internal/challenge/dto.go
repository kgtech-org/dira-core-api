package challenge

import (
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// Response est un objectif tel que la CONSOLE le lit.
type Response struct {
	ID       string `json:"id"`
	Country  string `json:"country,omitempty"`
	Audience string `json:"audience"`
	Metric   string `json:"metric"`
	// MetricLabel : la mesure en clair. ⚠️ SERVIE, parce qu'un écran qui
	// afficherait `rides_done` ferait traduire chaque console à sa façon — et
	// la nôtre le fait déjà pour les pièces de conformité et les motifs
	// d'annulation.
	MetricLabel string `json:"metric_label"`
	Target      int    `json:"target"`
	// Money dit que la cible est un MONTANT. Sans lui, un écran afficherait
	// « 20 000 fois » au lieu de « 20 000 F ».
	Money       bool   `json:"money"`
	RewardXOF   int    `json:"reward_xof"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Window      Window `json:"window"`
	Repeat      string `json:"repeat"`
	Status      string `json:"status"`
	SeriesID    string `json:"series_id,omitempty"`

	Limits   promo.Limits   `json:"limits"`
	Counters promo.Counters `json:"counters"`
	// RemainingXOF : ce qu'il reste dans l'enveloppe. ⚠️ `-1` veut dire « pas
	// de plafond » et NON « rien » : les confondre afficherait « 0 F restants »
	// sur un objectif sans budget, c'est-à-dire l'inverse de la vérité.
	RemainingXOF int `json:"remaining_xof"`
	// Winners, WinnersLeft : combien ont gagné, et combien de places restent.
	// `WinnersLeft` vaut `-1` sans limite de gagnants.
	Winners     int `json:"winners"`
	WinnersLeft int `json:"winners_left"`
	// Missed : combien ont ATTEINT LA CIBLE SANS ÊTRE PAYÉS.
	//
	// ⚠️ SERVI EN TÊTE DE LA FICHE, parce que c'est un aveu : si ce nombre
	// n'est pas zéro, des promesses ont été faites et non tenues, et
	// l'exploitation doit décider — payer à la main, ou doter l'enveloppe.
	Missed int `json:"missed"`

	CreatedAt  time.Time  `json:"created_at"`
	LaunchedAt *time.Time `json:"launched_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// MineResponse est un objectif tel que LA PERSONNE le voit.
//
// ⚠️ ELLE NE PORTE NI L'ENVELOPPE NI LE BUDGET. Ce que l'entreprise a provisionné
// n'est pas l'affaire de quelqu'un qui joue ; lui montrer « 240 000 F de budget »
// invite à calculer combien d'autres ont déjà gagné, et transforme un objectif
// en course aux places. Ce qu'on lui doit, c'est SON avancement et le temps
// qu'il lui reste.
type MineResponse struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	MetricLabel string `json:"metric_label"`
	Target      int    `json:"target"`
	Money       bool   `json:"money"`
	RewardXOF   int    `json:"reward_xof"`
	// Value : où il en est. Toujours posé, même à zéro.
	Value int `json:"value"`
	// Percent : l'avancement en pourcentage, BORNÉ À 100.
	//
	// ⚠️ SERVI PLUTÔT QUE CALCULÉ À L'ÉCRAN, et borné ici : une barre à 130 %
	// sur un objectif dépassé a l'air d'un bug, et chaque application l'aurait
	// bornée à sa façon.
	Percent int       `json:"percent"`
	EndsAt  time.Time `json:"ends_at"`
	// Reached, PaidXOF : gagné, et payé.
	//
	// ⚠️ DEUX CHAMPS, PARCE QUE CE SONT DEUX MOMENTS. « Vous avez réussi » doit
	// s'afficher dès le franchissement, même si le versement prend une minute —
	// attendre l'argent pour annoncer la victoire ferait douter quelqu'un qui a
	// compté ses courses.
	Reached bool `json:"reached"`
	PaidXOF int  `json:"paid_xof,omitempty"`
	// Pending : gagné, pas encore payé. ⚠️ À DIRE EN CLAIR (« bonus en cours de
	// versement ») : le silence se lit comme un refus.
	Pending bool `json:"pending"`
}

func toResponse(c *Challenge, winners, missed int) Response {
	m, _ := LookupMetric(c.Metric)
	out := Response{
		ID: c.ID.Hex(), Country: c.Country, Audience: c.Audience,
		Metric: c.Metric, MetricLabel: m.Label, Money: m.Money,
		Target: c.Target, RewardXOF: c.RewardXOF,
		Title: c.Title, Description: c.Description,
		Window: c.Window, Repeat: c.Repeat, Status: c.Status,
		Limits: c.Limits, Counters: c.Counters,
		RemainingXOF: c.Limits.Remaining(c.Counters),
		Winners:      winners,
		WinnersLeft:  -1,
		Missed:       missed,
		CreatedAt:    c.CreatedAt, LaunchedAt: c.LaunchedAt, EndedAt: c.EndedAt,
	}
	if c.SeriesID != nil {
		out.SeriesID = c.SeriesID.Hex()
	}
	if c.Limits.MaxUses > 0 {
		out.WinnersLeft = max(0, c.Limits.MaxUses-c.Counters.Uses())
	}
	return out
}

func toMine(c *Challenge, p *Progress) MineResponse {
	m, _ := LookupMetric(c.Metric)
	out := MineResponse{
		ID: c.ID.Hex(), Title: c.Title, Description: c.Description,
		MetricLabel: m.Label, Money: m.Money,
		Target: c.Target, RewardXOF: c.RewardXOF, EndsAt: c.Window.To,
	}
	if p != nil {
		out.Value = p.Value
		out.Reached = p.ReachedAt != nil
		out.PaidXOF = p.PaidXOF
		out.Pending = p.ReachedAt != nil && p.PaidAt == nil
	}
	if c.Target > 0 {
		out.Percent = min(100, out.Value*100/c.Target)
	}
	return out
}
