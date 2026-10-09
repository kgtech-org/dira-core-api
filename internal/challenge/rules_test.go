package challenge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

func week() Window {
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	return Window{From: from, To: from.AddDate(0, 0, 7)}
}

func sane() *Challenge {
	return &Challenge{
		Audience: ForDriver, Metric: MetricRidesDone, Target: 20,
		RewardXOF: 5000, Title: "20 courses cette semaine", Repeat: RepeatWeekly,
		Window: week(),
	}
}

// ⚠️⚠️ LE TEST QUI COMPTE LE PLUS DE CE FICHIER, ET IL N'EST PAS TECHNIQUE.
//
// Un bonus sur le nombre de courses dans une fenêtre courte pousse des gens à
// conduire fatigués — « encore trois et j'ai les 5 000 F », à vingt-trois
// heures, après onze heures de volant. Ce n'est pas une hypothèse : c'est le
// mécanisme par lequel ce genre de prime a tué des chauffeurs sur d'autres
// plateformes. On ne peut pas empêcher quelqu'un de se fatiguer ; on peut
// refuser d'ÉCRIRE l'objectif qui le lui demande.
func TestATargetThatAsksForUnsafeHoursIsRefused(t *testing.T) {
	c := sane()
	// 12 courses par jour × 7 jours = 84, la borne.
	c.Target = 84
	require.NoError(t, Check(c), "84 sur une semaine passe, c'est déjà beaucoup")

	c.Target = 85
	err := Check(c)
	require.Error(t, err)
	meta := apperr.From(err).Meta
	assert.Equal(t, []string{"target", "window"}, meta["fields"])
	assert.Equal(t, 84, meta["max_target"])

	// ⚠️ ET LA SORTIE EST PROPOSÉE : la MÊME cible sur deux semaines passe.
	// Même coût pour l'entreprise, une semaine de sommeil pour la personne.
	c.Window.To = c.Window.From.AddDate(0, 0, 14)
	assert.NoError(t, Check(c), "allonger la fenêtre est la bonne réponse")
}

// Chaque mesure a sa propre borne : un livreur fait plus de courses courtes
// qu'un chauffeur de longues.
func TestEachMetricHasItsOwnDailyBound(t *testing.T) {
	rides, _ := LookupMetric(MetricRidesDone)
	deliveries, _ := LookupMetric(MetricDeliveriesDone)
	assert.Greater(t, deliveries.MaxPerDay, rides.MaxPerDay,
		"un livreur enchaîne plus de courses courtes qu'un chauffeur de longues")

	// ⚠️ « JOURS TRAVAILLÉS » EST BORNÉ À UN PAR JOUR, ce qui est une évidence
	// arithmétique — et c'est justement ce qui la rend sûre : on ne peut pas
	// demander plus de jours qu'il n'y en a.
	days, _ := LookupMetric(MetricDaysActive)
	assert.Equal(t, 1, days.MaxPerDay)
	assert.Error(t, CheckTarget(days, 8, week()), "huit jours dans une semaine")
	assert.NoError(t, CheckTarget(days, 5, week()))
}

// ⚠️ LES MESURES CLIENT N'ONT PAS DE BORNE DE SÉCURITÉ, et c'est volontaire :
// un client qui commande trop ne se met pas en danger physique. Une cible
// absurde y reste malhonnête, mais c'est la console qui l'arrête, pas une règle.
func TestClientMetricsHaveNoSafetyBound(t *testing.T) {
	spent, _ := LookupMetric(MetricAmountSpent)
	assert.Zero(t, spent.MaxPerDay)
	assert.NoError(t, CheckTarget(spent, 500_000, week()))
}

// ⚠️⚠️ TROIS MESURES SONT REFUSÉES EXPRÈS, ET LE REFUS PORTE SA RAISON. Elles
// sont toutes les trois TECHNIQUEMENT DISPONIBLES : les heures en ligne sont
// journalisées, le taux d'acceptation est calculé depuis la 4.55.0, la durée
// réelle est mesurée. Répondre « mesure inconnue » aurait envoyé quelqu'un
// chercher une faute de frappe, puis demander à un développeur de l'ajouter.
func TestTheThreeRefusedMetricsSayWhyTheyAreRefused(t *testing.T) {
	for key := range Forbidden {
		c := sane()
		c.Metric = key
		err := Check(c)
		require.Error(t, err, key)
		msg := err.Error()
		assert.Contains(t, msg, "refused on purpose", key)
		assert.NotContains(t, msg, "unknown", key)
		// La raison voyage dans la réponse, pour l'écran qui l'affiche.
		assert.NotEmpty(t, apperr.From(err).Meta["reason"], key)
	}
	// Et les trois nommées, pour que leur disparition se voie.
	assert.Contains(t, Forbidden, "hours_online")
	assert.Contains(t, Forbidden, "acceptance_rate")
	assert.Contains(t, Forbidden, "fastest_rides")
}

// ⚠️ UNE MESURE N'EST PAS PROPOSÉE À TOUS LES PUBLICS. « Courses terminées » à
// un client est absurde, « montant dépensé » à un chauffeur l'est autant — il
// en encaisse, il n'en dépense pas.
func TestAMetricBelongsToItsAudience(t *testing.T) {
	c := sane()
	c.Audience = ForClient
	c.Metric = MetricRidesDone
	err := Check(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not apply to client")
	// Et la réponse dit ce qui EST proposable : sans quoi la console affiche un
	// refus et laisse l'opérateur deviner.
	allowed := apperr.From(err).Meta["allowed"].([]string)
	assert.Contains(t, allowed, MetricOrdersPlaced)
	assert.NotContains(t, allowed, MetricRidesDone)

	assert.Len(t, MetricsFor(ForDriver), 2)
	assert.Len(t, MetricsFor(ForClient), 3)
	for _, m := range MetricsFor(ForClient) {
		assert.NotEqual(t, MetricDaysActive, m.Key,
			"« jours travaillés » n'a pas de sens pour un client")
	}
}

// ⚠️ UNE ENVELOPPE QUI NE PAIE PERSONNE EST UN PIÈGE : elle s'affiche comme une
// offre, et le premier gagnant passe déjà le plafond. Mieux vaut le refuser à
// l'écriture que de le découvrir dans les réclamations.
func TestABudgetThatCannotPayOneWinnerIsRefused(t *testing.T) {
	c := sane()
	c.Limits = promo.Limits{BudgetXOF: 4000}
	c.RewardXOF = 5000
	assert.Error(t, Check(c))

	c.Limits = promo.Limits{BudgetXOF: 5000}
	assert.NoError(t, Check(c), "exactement un gagnant, c'est un choix valable")

	// Pas de budget = pas de plafond, comme partout ailleurs dans `pkg/promo`.
	c.Limits = promo.Limits{}
	assert.NoError(t, Check(c))
}

// Un objectif sans bonus est une annonce, pas un objectif — et une annonce se
// fait avec une campagne de notification.
func TestAnObjectiveWithoutABonusIsRefused(t *testing.T) {
	c := sane()
	c.RewardXOF = 0
	err := Check(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "announcement, not a challenge")
}

// ⚠️ LE TITRE EST EXIGÉ. C'est ce que la personne lit, et ce qui donne envie
// d'essayer : « 5 courses avant dimanche, 2 000 F pour vous » se lit, pas
// `rides_done >= 5`. Un objectif sans titre serait affiché avec sa clé
// technique.
func TestTheTitleIsRequiredBecauseItIsWhatMakesSomeoneTry(t *testing.T) {
	c := sane()
	c.Title = "   "
	Normalise(c)
	err := Check(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title")
}

// ⚠️ UNE FENÊTRE DE SIX MOIS N'EST PLUS UN OBJECTIF : personne ne se souvient
// de ce qu'il doit faire, l'avancement n'avance visiblement plus, et la ligne de
// bonus qu'on provisionne vieillit dans les comptes. C'est un programme de
// fidélité — un autre objet.
func TestAWindowLongerThanThreeMonthsIsRefused(t *testing.T) {
	c := sane()
	c.Target = 5
	c.Window.To = c.Window.From.AddDate(0, 0, MaxDays+1)
	err := Check(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loyalty programme")
}

// La mise en forme comble ce qui peut l'être sans rien juger.
func TestNormaliseFillsWhatItCan(t *testing.T) {
	c := &Challenge{Audience: "  DRIVER ", Metric: " RIDES_DONE", Title: "  x  "}
	Normalise(c)
	assert.Equal(t, ForDriver, c.Audience)
	assert.Equal(t, MetricRidesDone, c.Metric)
	assert.Equal(t, "x", c.Title)
	assert.Equal(t, RepeatNone, c.Repeat, "sans récurrence : une seule fois")
	assert.Equal(t, StatusDraft, c.Status, "jamais lancé par défaut")
}
