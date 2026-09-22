package metrics

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPresetCountsTheDayInProgress(t *testing.T) {
	r := httptest.NewRequest("GET", "/admin/metrics?period=7d", nil)
	p := PeriodFromRequest(r)
	assert.Equal(t, GranDay, p.Granularity)
	assert.Equal(t, 7*24*time.Hour, p.Duration())
	// La journée en cours compte : la fin est demain minuit, sinon la
	// dernière colonne plongerait à chaque consultation du matin.
	assert.True(t, p.To.After(time.Now().UTC()), "la fenêtre englobe l'heure présente")
}

func TestTheGranularityFollowsTheWindow(t *testing.T) {
	for _, tc := range []struct{ period, want string }{
		{"today", GranHour},
		{"7d", GranDay},
		{"90d", GranDay},
		{"180d", GranWeek},
		{"365d", GranWeek},
	} {
		p := PeriodFromRequest(httptest.NewRequest("GET", "/m?period="+tc.period, nil))
		assert.Equal(t, tc.want, p.Granularity, tc.period)
	}
	// Mais l'appelant garde le dernier mot.
	p := PeriodFromRequest(httptest.NewRequest("GET", "/m?period=365d&granularity=month", nil))
	assert.Equal(t, GranMonth, p.Granularity)
}

func TestACustomWindowWins(t *testing.T) {
	p := PeriodFromRequest(httptest.NewRequest("GET", "/m?period=7d&from=2026-01-01&to=2026-01-08", nil))
	assert.Equal(t, "custom", p.Label)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), p.From)
	assert.Equal(t, 7*24*time.Hour, p.Duration())
}

// Une fenêtre à l'envers ou vide ne doit pas rendre une division par zéro
// plus loin : on la referme sur un jour.
func TestAnEmptyWindowIsRepaired(t *testing.T) {
	p := PeriodFromRequest(httptest.NewRequest("GET", "/m?from=2026-01-08&to=2026-01-01", nil))
	assert.True(t, p.To.After(p.From))
}

func TestThePreviousWindowTouchesThisOne(t *testing.T) {
	p := PeriodFromRequest(httptest.NewRequest("GET", "/m?from=2026-01-08&to=2026-01-15", nil))
	prev := p.Previous()
	assert.Equal(t, p.From, prev.To, "elles se touchent, sans trou ni recouvrement")
	assert.Equal(t, p.Duration(), prev.Duration())
}

// Le squelette : une tranche sans donnée vaut zéro et garde sa place. Une
// courbe à trous se lit comme une baisse qui n'a pas eu lieu.
func TestAnEmptyBucketIsAZeroNotAHole(t *testing.T) {
	p := Period{
		From:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		To:          time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		Granularity: GranDay,
	}
	pts := Fill(p, map[time.Time]float64{
		time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC): 12,
	})
	require.Len(t, pts, 4)
	assert.Equal(t, []float64{0, 0, 12, 0}, []float64{pts[0].V, pts[1].V, pts[2].V, pts[3].V})
}

func TestTheWeekStartsOnMonday(t *testing.T) {
	p := Period{Granularity: GranWeek}
	sunday := time.Date(2026, 1, 4, 15, 0, 0, 0, time.UTC) // un dimanche
	assert.Equal(t, time.Date(2025, 12, 29, 0, 0, 0, 0, time.UTC), p.Truncate(sunday))
}

// Un classement tronqué doit se sommer : le reste est regroupé, sinon on
// soustrait de tête et on se trompe.
func TestTheRestIsGroupedNotDropped(t *testing.T) {
	items := []Slice{{Value: 10}, {Value: 8}, {Value: 5}, {Value: 3}, {Value: 1}}
	top := TopN(items, 2, "Autres")
	require.Len(t, top, 3)
	assert.Equal(t, float64(9), top[2].Value, "5 + 3 + 1")
	assert.Equal(t, Sum(items), Sum(top))
}

func TestARateOnNothingIsNotAHundredPercent(t *testing.T) {
	assert.Equal(t, float64(0), Ratio(0, 0))
	assert.Equal(t, float64(50), Ratio(1, 2))
}

func TestTheHeatmapPlacesMondayFirst(t *testing.T) {
	h := NewHeatmap("demand", "Demande")
	monday9 := time.Date(2026, 1, 5, 9, 30, 0, 0, time.UTC)
	h.Add(monday9, 3)
	assert.Equal(t, float64(3), h.Values[0][9])
	assert.Equal(t, float64(3), h.Max)
	sunday23 := time.Date(2026, 1, 11, 23, 0, 0, 0, time.UTC)
	h.Add(sunday23, 1)
	assert.Equal(t, float64(1), h.Values[6][23])
}

// La variation n'est pas calculable sans référence — et une flèche fausse
// vaut moins que pas de flèche.
func TestNoDeltaWithoutAReference(t *testing.T) {
	_, ok := KPI{Value: 10}.Delta()
	assert.False(t, ok)
	d, ok := KPI{Value: 110, Previous: Prev(100)}.Delta()
	require.True(t, ok)
	assert.InDelta(t, 10, d, 0.001)
}
