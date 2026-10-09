package challenge

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LA RÉCURRENCE — une série produit un objectif par période.

// ⚠️ LA FENÊTRE S'ALIGNE SUR LE CALENDRIER, pas sur la date de lancement. Une
// série hebdomadaire lancée un MERCREDI doit courir du lundi au lundi : sinon
// « cette semaine » ne veut pas dire la même chose pour la plateforme et pour
// la personne, et un chauffeur qui compte ses courses du lundi au dimanche se
// trompe sans comprendre pourquoi.
func TestTheWeeklyWindowStartsOnMondayWhateverTheLaunchDay(t *testing.T) {
	wednesday := time.Date(2026, 10, 14, 15, 30, 0, 0, time.UTC)
	w := NextWindow(RepeatWeekly, wednesday)
	assert.Equal(t, time.Monday, w.From.Weekday())
	assert.Equal(t, 12, w.From.Day(), "le lundi 12 octobre")
	assert.Equal(t, 7, w.Days())

	// ⚠️ ET LE DIMANCHE APPARTIENT À LA SEMAINE QUI SE TERMINE, pas à celle qui
	// commence. `time.Weekday()` met dimanche à zéro : le prendre tel quel
	// aurait fait basculer la semaine un jour trop tôt, et un chauffeur aurait
	// vu son compteur se remettre à zéro le dimanche matin.
	sunday := time.Date(2026, 10, 18, 23, 0, 0, 0, time.UTC)
	assert.Equal(t, w.From, NextWindow(RepeatWeekly, sunday).From,
		"le dimanche 18 est encore dans la semaine du lundi 12")
	monday := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 19, NextWindow(RepeatWeekly, monday).From.Day(),
		"et le lundi 19 ouvre la suivante")
}

func TestTheMonthlyWindowIsTheCalendarMonth(t *testing.T) {
	w := NextWindow(RepeatMonthly, time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC))
	assert.Equal(t, 1, w.From.Day())
	assert.Equal(t, time.October, w.From.Month())
	assert.Equal(t, time.November, w.To.Month())
	assert.Equal(t, 31, w.Days(), "octobre en a 31")
}

// ⚠️ UNE OCCURRENCE A SON PROPRE COMPTEUR ET SA PROPRE ENVELOPPE. Un compteur
// qui ne se remet pas à zéro n'est plus un objectif : « 20 courses cette
// semaine » deviendrait « 20 courses depuis toujours », gagné une fois par les
// anciens et inatteignable pour qui arrive au mois deux.
func TestEachOccurrenceStartsFromZeroWithAFreshEnvelope(t *testing.T) {
	f := newFixture(t)
	g := driverGoal()
	g.Repeat = RepeatWeekly
	g.Window = NextWindow(RepeatWeekly, f.at)
	model := f.live(t, g)
	// Le modèle a consommé son enveloppe.
	f.store.challenges[model.ID].Counters.UsesSpent = 3
	f.store.challenges[model.ID].Counters.AmountSpent = 15_000

	f.svc.RunDue(f.ctx)

	var occ *Challenge
	for _, c := range f.store.challenges {
		if c.SeriesID != nil && *c.SeriesID == model.ID {
			occ = c
		}
	}
	require.NotNil(t, occ, "une occurrence a été créée")
	assert.Zero(t, occ.Counters.UsesSpent, "compteur à zéro")
	assert.Zero(t, occ.Counters.AmountSpent)
	assert.Equal(t, model.Limits, occ.Limits, "la même enveloppe, à neuf")
	// ⚠️ LE PAYS EST RECOPIÉ : c'est le piège des programmations d'abonnement.
	// Une occurrence sans pays n'apparaît dans aucune console — chaque liste est
	// bornée par pays — et compterait pourtant, et paierait. Un objectif
	// invisible qui dépense.
	assert.Equal(t, model.Country, occ.Country)
	assert.NotEmpty(t, occ.Country)
	// ⚠️ ET ELLE NE SE RÉPÈTE PAS ELLE-MÊME, sinon le balayage produirait une
	// arborescence au lieu d'une suite.
	assert.Equal(t, RepeatNone, occ.Repeat)
}

// ⚠️ DEUX BALAYAGES NE CRÉENT PAS DEUX OCCURRENCES DE LA MÊME SEMAINE. Deux
// instances de l'API, un redémarrage : sans la comparaison de FENÊTRE,
// l'entreprise paierait deux fois ce qu'elle a budgété une.
func TestTwoSweepsInTheSameWeekCreateOneOccurrence(t *testing.T) {
	f := newFixture(t)
	g := driverGoal()
	g.Repeat = RepeatWeekly
	g.Window = NextWindow(RepeatWeekly, f.at)
	model := f.live(t, g)

	f.svc.RunDue(f.ctx)
	f.svc.RunDue(f.ctx)
	f.svc.RunDue(f.ctx)

	n := 0
	for _, c := range f.store.challenges {
		if c.SeriesID != nil && *c.SeriesID == model.ID {
			n++
		}
	}
	assert.Equal(t, 1, n)
}

// ⚠️ LE BALAYAGE NE RATTRAPE PAS LE PASSÉ. S'il n'a pas tourné pendant trois
// semaines, il ne crée pas trois semaines d'objectifs : personne n'aurait pu les
// jouer, et ils se gagneraient d'un coup avec des courses déjà faites.
func TestTheSweepNeverMaterialisesThePast(t *testing.T) {
	f := newFixture(t)
	g := driverGoal()
	g.Repeat = RepeatWeekly
	g.Window = NextWindow(RepeatWeekly, f.at)
	model := f.live(t, g)

	// Trois semaines plus tard, un seul balayage.
	later := f.at.AddDate(0, 0, 21)
	f.svc.now = func() time.Time { return later }
	f.svc.RunDue(f.ctx)

	for _, c := range f.store.challenges {
		if c.SeriesID == nil {
			continue
		}
		assert.True(t, c.Window.Contains(later),
			"la seule occurrence créée est celle de MAINTENANT")
	}
	n := 0
	for _, c := range f.store.challenges {
		if c.SeriesID != nil && *c.SeriesID == model.ID {
			n++
		}
	}
	assert.Equal(t, 1, n, "une occurrence, pas trois")
}

// Une série ARRÊTÉE ne produit plus rien.
func TestAnEndedSeriesProducesNothing(t *testing.T) {
	f := newFixture(t)
	g := driverGoal()
	g.Repeat = RepeatWeekly
	g.Window = NextWindow(RepeatWeekly, f.at)
	model := f.live(t, g)
	_, err := f.svc.End(f.ctx, model.ID.Hex())
	require.NoError(t, err)

	f.svc.RunDue(f.ctx)
	for _, c := range f.store.challenges {
		assert.Nil(t, c.SeriesID, "aucune occurrence")
	}
}

// Un objectif sans récurrence n'engendre rien : « une seule fois » veut dire
// une seule fois.
func TestAOneOffProducesNoOccurrence(t *testing.T) {
	f := newFixture(t)
	f.live(t, driverGoal())
	f.svc.RunDue(f.ctx)
	for _, c := range f.store.challenges {
		assert.Nil(t, c.SeriesID)
	}
}

func TestSeriesOfPointsAtTheModel(t *testing.T) {
	model := &Challenge{}
	model.ID = newID()
	assert.Equal(t, model.ID, seriesOf(model), "un modèle est sa propre série")
	id := model.ID
	occ := &Challenge{SeriesID: &id}
	occ.ID = newID()
	assert.Equal(t, model.ID, seriesOf(occ))
}

func newID() primitive.ObjectID { return primitive.NewObjectID() }
