package challenge

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// L'ENVELOPPE, ÉPROUVÉE EN DONNÉES — parce qu'un filtre MongoDB a l'air juste,
// répond sans erreur, et ne garde pas ce qu'il devait garder.

// doc fabrique un objectif tel que la base le porte.
func doc(status string, w Window, c promo.Counters) bson.M {
	return bson.M{
		"status": status,
		"window": bson.M{"from": w.From, "to": w.To},
		"counters": bson.M{
			"uses_reserved":       c.UsesReserved,
			"uses_spent":          c.UsesSpent,
			"amount_reserved_xof": c.AmountReserved,
			"amount_spent_xof":    c.AmountSpent,
		},
	}
}

// keeps évalue le filtre sur un document. Une évaluation minuscule, qui ne
// couvre que les opérateurs utilisés ici — assez pour attraper la seule chose
// qui compte : quel gagnant passe.
func keeps(t *testing.T, f bson.M, d bson.M) bool {
	t.Helper()
	if s, ok := f["status"]; ok && d["status"] != s {
		return false
	}
	win := d["window"].(bson.M)
	if m, ok := f["window.from"].(bson.M); ok {
		if win["from"].(time.Time).After(m["$lte"].(time.Time)) {
			return false
		}
	}
	if m, ok := f["window.to"].(bson.M); ok {
		if !win["to"].(time.Time).After(m["$gt"].(time.Time)) {
			return false
		}
	}
	for _, sub := range and(f) {
		if !evalExpr(t, sub["$expr"].(bson.M), d) {
			return false
		}
	}
	return true
}

func and(f bson.M) []bson.M {
	v, ok := f["$and"]
	if !ok {
		return nil
	}
	return v.([]bson.M)
}

// evalExpr évalue `$lte` / `$lt` sur une somme de champs et de constantes.
func evalExpr(t *testing.T, e bson.M, d bson.M) bool {
	t.Helper()
	for op, raw := range e {
		args := raw.(bson.A)
		left := sum(t, args[0], d)
		right := sum(t, args[1], d)
		switch op {
		case "$lte":
			return left <= right
		case "$lt":
			return left < right
		}
		t.Fatalf("opérateur non évalué : %s", op)
	}
	return true
}

func sum(t *testing.T, v any, d bson.M) int {
	t.Helper()
	switch x := v.(type) {
	case int:
		return x
	case bson.M:
		if add, ok := x["$add"]; ok {
			n := 0
			for _, a := range add.(bson.A) {
				n += sum(t, a, d)
			}
			return n
		}
		if nn, ok := x["$ifNull"]; ok {
			return sum(t, nn.(bson.A)[0], d)
		}
	case string:
		// `$counters.amount_spent_xof` → la valeur du document.
		c := d["counters"].(bson.M)
		key := x[len("$counters."):]
		if n, ok := c[key].(int); ok {
			return n
		}
		return 0
	}
	t.Fatalf("valeur non évaluée : %#v", v)
	return 0
}

func now() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }

// ⚠️⚠️ LE TEST QUI COMPTE : LE DERNIER GAGNANT QUE LE BUDGET PEUT PAYER PASSE,
// LE SUIVANT NON. Si cette ligne tombe, l'entreprise paie sans plafond et rien
// ne le signale avant le relevé bancaire.
func TestTheLastWinnerTheBudgetCanPayGetsThroughAndTheNextDoesNot(t *testing.T) {
	id := primitive.NewObjectID()
	w := week()
	// 10 000 F d'enveloppe, 5 000 F de bonus : deux gagnants, pas trois.
	limits := promo.Limits{BudgetXOF: 10_000}
	f := claimFilter(id, limits, 5000, now())

	assert.True(t, keeps(t, f, doc(StatusLive, w, promo.Counters{})),
		"le premier passe")
	assert.True(t, keeps(t, f, doc(StatusLive, w, promo.Counters{
		UsesSpent: 1, AmountSpent: 5000,
	})), "le second passe : 5 000 + 5 000 = 10 000, pile l'enveloppe")
	assert.False(t, keeps(t, f, doc(StatusLive, w, promo.Counters{
		UsesSpent: 2, AmountSpent: 10_000,
	})), "le troisième ne passe pas")
}

// ⚠️ LE PROMIS COMPTE AUTANT QUE LE DÉPENSÉ. Un bonus franchi dont le versement
// est en cours a déjà engagé l'argent : ne compter que le dépensé laisserait
// mille personnes franchir une enveloppe de dix pendant que les versements
// partent.
func TestWhatIsPromisedCountsAsMuchAsWhatIsSpent(t *testing.T) {
	f := claimFilter(primitive.NewObjectID(), promo.Limits{BudgetXOF: 10_000}, 5000, now())
	assert.False(t, keeps(t, f, doc(StatusLive, week(), promo.Counters{
		UsesReserved: 2, AmountReserved: 10_000,
	})), "deux bonus promis remplissent l'enveloppe, même si rien n'est encore versé")
}

// Le nombre de gagnants borne aussi, indépendamment du budget — c'est ainsi
// qu'on annonce « les 100 premiers ».
func TestTheWinnerCountBoundsToo(t *testing.T) {
	f := claimFilter(primitive.NewObjectID(), promo.Limits{MaxUses: 2}, 5000, now())
	assert.True(t, keeps(t, f, doc(StatusLive, week(), promo.Counters{UsesSpent: 1})))
	assert.False(t, keeps(t, f, doc(StatusLive, week(), promo.Counters{UsesSpent: 2})))
}

// Sans limite, rien ne barre : zéro veut dire « pas de plafond », comme partout
// dans `pkg/promo`.
func TestNoLimitsMeansNoCeiling(t *testing.T) {
	f := claimFilter(primitive.NewObjectID(), promo.Limits{}, 5000, now())
	require.Empty(t, and(f), "aucune clause d'enveloppe")
	assert.True(t, keeps(t, f, doc(StatusLive, week(), promo.Counters{
		UsesSpent: 10_000, AmountSpent: 50_000_000,
	})))
}

// ⚠️ LA FENÊTRE EST DANS LE FILTRE, et c'est ce qui rend une verticale qui
// rejoue inoffensive : sa file hors ligne peut remettre une course trois jours
// plus tard, l'objectif terminé ne paiera pas.
func TestAnObjectiveWhoseWindowHasPassedPaysNothing(t *testing.T) {
	f := claimFilter(primitive.NewObjectID(), promo.Limits{}, 5000, now())
	past := Window{
		From: now().AddDate(0, 0, -20),
		To:   now().AddDate(0, 0, -13),
	}
	assert.False(t, keeps(t, f, doc(StatusLive, past, promo.Counters{})))
	future := Window{From: now().AddDate(0, 0, 3), To: now().AddDate(0, 0, 10)}
	assert.False(t, keeps(t, f, doc(StatusLive, future, promo.Counters{})))
}

// Un objectif non lancé ne paie pas, et un objectif arrêté non plus.
func TestOnlyALiveObjectivePays(t *testing.T) {
	f := claimFilter(primitive.NewObjectID(), promo.Limits{}, 5000, now())
	for _, st := range []string{StatusDraft, StatusEnded} {
		assert.False(t, keeps(t, f, doc(st, week(), promo.Counters{})), st)
	}
}

// ⚠️ LE VERSEMENT DÉPLACE LA LIGNE DE « PROMIS » À « DÉPENSÉ », il ne la
// duplique pas : sans la décrémentation, l'enveloppe compterait chaque bonus
// deux fois et se fermerait à la moitié des gagnants annoncés.
func TestSettlingMovesTheLineRatherThanAddingOne(t *testing.T) {
	inc := settleUpdate(5000, now())["$inc"].(bson.M)
	assert.Equal(t, -1, inc["counters.uses_reserved"])
	assert.Equal(t, -5000, inc["counters.amount_reserved_xof"])
	assert.Equal(t, 1, inc["counters.uses_spent"])
	assert.Equal(t, 5000, inc["counters.amount_spent_xof"])
}

// ⚠️ ET UN ÉCHEC REND LA PLACE, sinon l'enveloppe se bloque sur de l'argent
// intact : au bout de quelques pannes, l'objectif n'accepterait plus personne.
// `uses_released` est gardé pour être LU — il ne compte dans aucune limite, et
// c'est ce qui permet de voir qu'on a eu des échecs.
func TestAFailedPayoutGivesTheSlotBack(t *testing.T) {
	inc := releaseUpdate(5000, now())["$inc"].(bson.M)
	assert.Equal(t, -1, inc["counters.uses_reserved"])
	assert.Equal(t, -5000, inc["counters.amount_reserved_xof"])
	assert.Equal(t, 1, inc["counters.uses_released"])
	assert.NotContains(t, inc, "counters.uses_spent", "rien n'a été dépensé")
}
