package challenge

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// LE FRANCHISSEMENT DE LA CIBLE — le chemin par lequel l'argent sort.

// --- la doublure de persistance -----------------------------------------

type memStore struct {
	challenges map[primitive.ObjectID]*Challenge
	progress   map[string]*Progress
	// claimFails simule une enveloppe pleine sans avoir à la remplir.
	claimFails bool
}

func newStore() *memStore {
	return &memStore{
		challenges: map[primitive.ObjectID]*Challenge{},
		progress:   map[string]*Progress{},
	}
}

func key(c, u primitive.ObjectID) string { return c.Hex() + ":" + u.Hex() }

func (m *memStore) Insert(_ context.Context, c *Challenge) error {
	if c.ID.IsZero() {
		c.ID = primitive.NewObjectID()
	}
	cp := *c
	m.challenges[c.ID] = &cp
	return nil
}
func (m *memStore) ByID(_ context.Context, id primitive.ObjectID) (*Challenge, error) {
	c, ok := m.challenges[id]
	if !ok {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}
func (m *memStore) Save(_ context.Context, c *Challenge) error {
	cp := *c
	m.challenges[c.ID] = &cp
	return nil
}

// ⚠️ LA DOUBLURE APPLIQUE LE MÊME FILTRE QUE `listQuery`, STATUT COMPRIS.
// Ma première version ignorait `f.Status` : une série ARRÊTÉE revenait dans le
// balayage, qui lui fabriquait des occurrences. Le test l'a attrapé — et c'est
// bien la doublure qui mentait, pas le code. Un faux qui filtre moins que la
// base fait passer au vert exactement ce qu'on voulait interdire.
func (m *memStore) List(_ context.Context, f Filter, _ int) ([]Challenge, error) {
	var out []Challenge
	for _, c := range m.challenges {
		if f.Audience != "" && c.Audience != f.Audience {
			continue
		}
		switch {
		case f.Live:
			if c.Status != StatusLive || !c.Window.Contains(f.At) {
				continue
			}
		case f.Status != "":
			if c.Status != f.Status {
				continue
			}
		}
		out = append(out, *c)
	}
	return out, nil
}

// ⚠️ LA DOUBLURE APPLIQUE LA DÉDUPLICATION, elle aussi. Un faux qui l'ignorerait
// ferait passer au vert un compteur qui compte deux fois la même course — et
// c'est précisément le défaut que la référence existe pour empêcher.
func (m *memStore) AddProgress(_ context.Context, cid, uid primitive.ObjectID, countryCode, ref string, delta int, now time.Time) (*Progress, error) {
	k := key(cid, uid)
	p, ok := m.progress[k]
	if !ok {
		p = &Progress{ChallengeID: cid, UserID: uid, Country: countryCode}
		m.progress[k] = p
	}
	for _, r := range p.Refs {
		if r == ref {
			return nil, nil
		}
	}
	p.Refs = append(p.Refs, ref)
	p.Value += delta
	p.UpdatedAt = now
	cp := *p
	return &cp, nil
}

func (m *memStore) ClaimSlot(_ context.Context, c *Challenge, _ time.Time) (bool, error) {
	if m.claimFails {
		return false, nil
	}
	cur := m.challenges[c.ID]
	if cur.Limits.MaxUses > 0 && cur.Counters.Uses() >= cur.Limits.MaxUses {
		return false, nil
	}
	if cur.Limits.BudgetXOF > 0 && cur.Counters.Committed()+c.RewardXOF > cur.Limits.BudgetXOF {
		return false, nil
	}
	cur.Counters.UsesReserved++
	cur.Counters.AmountReserved += c.RewardXOF
	return true, nil
}

func (m *memStore) SettleSlot(_ context.Context, id primitive.ObjectID, reward int, _ time.Time) error {
	c := m.challenges[id]
	c.Counters.UsesReserved--
	c.Counters.AmountReserved -= reward
	c.Counters.UsesSpent++
	c.Counters.AmountSpent += reward
	return nil
}
func (m *memStore) ReleaseSlot(_ context.Context, id primitive.ObjectID, reward int, _ time.Time) error {
	c := m.challenges[id]
	c.Counters.UsesReserved--
	c.Counters.AmountReserved -= reward
	c.Counters.UsesReleased++
	return nil
}
func (m *memStore) MarkReached(_ context.Context, cid, uid primitive.ObjectID, now time.Time) (bool, error) {
	p := m.progress[key(cid, uid)]
	if p == nil || p.ReachedAt != nil {
		return false, nil
	}
	p.ReachedAt = &now
	return true, nil
}
func (m *memStore) MarkPaid(_ context.Context, cid, uid primitive.ObjectID, amount int, now time.Time) error {
	p := m.progress[key(cid, uid)]
	p.PaidAt, p.PaidXOF = &now, amount
	return nil
}
func (m *memStore) MarkMissed(_ context.Context, cid, uid primitive.ObjectID, _ time.Time) error {
	m.progress[key(cid, uid)].Missed = true
	return nil
}
func (m *memStore) ClaimOwed(_ context.Context, uid primitive.ObjectID, now time.Time) ([]Progress, error) {
	var out []Progress
	for _, p := range m.progress {
		if p.UserID != uid || p.ReachedAt == nil || p.PaidAt != nil || p.Missed {
			continue
		}
		before := *p
		p.PaidAt = &now
		out = append(out, before)
	}
	return out, nil
}

func (m *memStore) ProgressOf(_ context.Context, uid primitive.ObjectID, ids []primitive.ObjectID) (map[primitive.ObjectID]Progress, error) {
	out := map[primitive.ObjectID]Progress{}
	for _, id := range ids {
		if p, ok := m.progress[key(id, uid)]; ok {
			out[id] = *p
		}
	}
	return out, nil
}
func (m *memStore) Winners(_ context.Context, cid primitive.ObjectID, _ int) ([]Progress, error) {
	var out []Progress
	for _, p := range m.progress {
		if p.ChallengeID == cid && p.ReachedAt != nil {
			out = append(out, *p)
		}
	}
	return out, nil
}

// --- la bourse et le grand livre, doublés -------------------------------

type memPurse struct {
	paid map[string]int
	keys map[string]bool
	err  error
}

func newPurse() *memPurse {
	return &memPurse{paid: map[string]int{}, keys: map[string]bool{}}
}

func (p *memPurse) CreditBonus(_ context.Context, userID string, amount int, _, k string) error {
	if p.err != nil {
		return p.err
	}
	// ⚠️ LA DOUBLURE EST IDEMPOTENTE PAR CLÉ, comme le portefeuille réel : sans
	// cela, un test ne distinguerait pas « payé une fois » de « payé deux fois
	// avec la même clé », et c'est tout l'enjeu.
	if p.keys[k] {
		return nil
	}
	p.keys[k] = true
	p.paid[userID] += amount
	return nil
}

type memLedger struct{ paid map[string]int }

func newLedger() *memLedger { return &memLedger{paid: map[string]int{}} }

func (l *memLedger) CreditBonus(_ context.Context, userID string, amount int, _, _ string) error {
	l.paid[userID] += amount
	return nil
}

type memNotifier struct{ sent []string }

func (n *memNotifier) Notify(_ context.Context, userID, k string, _, _ map[string]string) {
	n.sent = append(n.sent, userID+":"+k)
}

// --- le décor ------------------------------------------------------------

const driverID = "6b0f000000000000000000d1"
const clientID = "6b0f000000000000000000c1"

type fixture struct {
	svc    *Service
	store  *memStore
	purse  *memPurse
	ledger *memLedger
	notif  *memNotifier
	ctx    context.Context
	at     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := newStore()
	svc := NewService(store)
	purse, ledger, notif := newPurse(), newLedger(), &memNotifier{}
	svc.SetPurse(purse)
	svc.SetLedger(ledger)
	svc.SetNotifier(notif)
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return at }
	return &fixture{
		svc: svc, store: store, purse: purse, ledger: ledger, notif: notif,
		ctx: country.WithCountry(context.Background(), "TG", country.SourceClaims),
		at:  at,
	}
}

// live écrit un objectif lancé.
func (f *fixture) live(t *testing.T, c *Challenge) *Challenge {
	t.Helper()
	out, err := f.svc.Create(f.ctx, "admin", c)
	require.NoError(t, err)
	out, err = f.svc.Launch(f.ctx, out.ID.Hex())
	require.NoError(t, err)
	return out
}

func driverGoal() *Challenge {
	return &Challenge{
		Audience: ForDriver, Metric: MetricRidesDone, Target: 3,
		RewardXOF: 5000, Title: "3 courses", Window: week(),
	}
}

// --- ce qui compte -------------------------------------------------------

// ⚠️ LA CIBLE FRANCHIE PAIE UNE FOIS, ET UNE SEULE. Le chemin est « prendre une
// place → noter le franchissement → verser → noter le versement » ; chaque
// étape est idempotente, et ce test le vérifie de bout en bout en comptant
// au-delà de la cible.
func TestCrossingTheTargetPaysExactlyOnce(t *testing.T) {
	f := newFixture(t)
	f.live(t, driverGoal())

	for _, ref := range []string{"r1", "r2", "r3", "r4", "r5"} {
		require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, ref, 1))
	}
	// ⚠️ LE CHAUFFEUR EST PAYÉ PAR LE GRAND LIVRE, pas par le portefeuille :
	// son argent vit dans les courses. Créditer le portefeuille lui aurait versé
	// un bonus qu'il ne voit jamais.
	assert.Equal(t, 5000, f.ledger.paid[driverID])
	assert.Zero(t, f.purse.paid[driverID])
	assert.Len(t, f.notif.sent, 1, "une seule annonce")
}

// ⚠️ LA MÊME COURSE COMPTÉE DEUX FOIS NE VAUT QU'UNE. Une verticale réessaie —
// c'est tout l'intérêt de sa file hors ligne —, et sans déduplication un
// objectif à 3 se gagnerait à 2.
func TestTheSameEventCountedTwiceAdvancesOnce(t *testing.T) {
	f := newFixture(t)
	c := f.live(t, driverGoal())

	for range 5 {
		require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, "r1", 1))
	}
	uid, _ := primitive.ObjectIDFromHex(driverID)
	got, err := f.store.ProgressOf(f.ctx, uid, []primitive.ObjectID{c.ID})
	require.NoError(t, err)
	assert.Equal(t, 1, got[c.ID].Value, "cinq fois la même course : un point")
	assert.Nil(t, got[c.ID].ReachedAt)
	assert.Zero(t, f.ledger.paid[driverID])
}

// ⚠️⚠️ LA CIBLE FRANCHIE SANS ENVELOPPE EST UN AVEU, PAS UN SILENCE. Une
// promesse a été faite et non tenue : l'avancement le DIT (`missed`), pour que
// l'exploitation décide — payer à la main, ou augmenter l'enveloppe. Le cacher
// aurait transformé un engagement en loterie silencieuse.
func TestReachingTheTargetWithAnEmptyEnvelopeIsRecordedAsMissedNotSilent(t *testing.T) {
	f := newFixture(t)
	g := driverGoal()
	// Une seule place.
	g.Limits = promo.Limits{MaxUses: 1}
	c := f.live(t, g)

	// Le premier gagne.
	for _, ref := range []string{"a1", "a2", "a3"} {
		require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, ref, 1))
	}
	assert.Equal(t, 5000, f.ledger.paid[driverID])

	// Le second franchit la cible et ne trouve plus de place.
	const second = "6b0f000000000000000000d2"
	for _, ref := range []string{"b1", "b2", "b3"} {
		require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, second, ref, 1))
	}
	uid, _ := primitive.ObjectIDFromHex(second)
	got, err := f.store.ProgressOf(f.ctx, uid, []primitive.ObjectID{c.ID})
	require.NoError(t, err)
	require.NotNil(t, got[c.ID].ReachedAt, "il a bien atteint la cible")
	assert.True(t, got[c.ID].Missed, "et on le DIT")
	assert.Nil(t, got[c.ID].PaidAt)
	assert.Zero(t, f.ledger.paid[second])
}

// ⚠️ UN VERSEMENT QUI ÉCHOUE REND LA PLACE, MAIS PAS LE DROIT. Sans la place
// rendue, l'enveloppe se bloque sur de l'argent intact ; sans le droit gardé,
// quelqu'un qui avait gagné disparaît parce qu'un appel a raté.
func TestAFailedPayoutKeepsTheRightAndFreesTheSlot(t *testing.T) {
	f := newFixture(t)
	g := driverGoal()
	g.Audience = ForClient
	g.Metric = MetricOrdersPlaced
	c := f.live(t, g)
	f.purse.err = assert.AnError

	for _, ref := range []string{"o1", "o2", "o3"} {
		require.NoError(t, f.svc.Report(f.ctx, ForClient, MetricOrdersPlaced, clientID, ref, 1))
	}
	uid, _ := primitive.ObjectIDFromHex(clientID)
	got, err := f.store.ProgressOf(f.ctx, uid, []primitive.ObjectID{c.ID})
	require.NoError(t, err)
	assert.NotNil(t, got[c.ID].ReachedAt, "le droit est acquis")
	assert.Nil(t, got[c.ID].PaidAt, "l'argent n'a pas bougé")
	// La place est rendue : l'enveloppe n'est pas bloquée.
	cur := f.store.challenges[c.ID]
	assert.Zero(t, cur.Counters.UsesReserved)
	assert.Equal(t, 1, cur.Counters.UsesReleased, "et l'échec se compte")
}

// Un client est payé par le PORTEFEUILLE du socle — c'est là que son argent
// vit, contrairement au chauffeur.
func TestAClientIsPaidThroughTheCoreWallet(t *testing.T) {
	f := newFixture(t)
	g := driverGoal()
	g.Audience, g.Metric = ForClient, MetricOrdersPlaced
	f.live(t, g)

	for _, ref := range []string{"o1", "o2", "o3"} {
		require.NoError(t, f.svc.Report(f.ctx, ForClient, MetricOrdersPlaced, clientID, ref, 1))
	}
	assert.Equal(t, 5000, f.purse.paid[clientID])
	assert.Zero(t, f.ledger.paid[clientID])
}

// ⚠️ DEUX OBJECTIFS SUR LA MÊME MESURE AVANCENT TOUS LES DEUX. « 3 courses
// cette semaine » et « 10 ce mois-ci » coexistent, et une course compte pour
// les deux : les faire s'exclure aurait obligé l'exploitation à choisir entre
// un objectif court et un long.
func TestTwoObjectivesOnTheSameMetricBothAdvance(t *testing.T) {
	f := newFixture(t)
	short := f.live(t, driverGoal())
	long := driverGoal()
	long.Target, long.Title = 5, "5 courses"
	longC := f.live(t, long)

	for _, ref := range []string{"r1", "r2", "r3"} {
		require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, ref, 1))
	}
	uid, _ := primitive.ObjectIDFromHex(driverID)
	got, err := f.store.ProgressOf(f.ctx, uid, []primitive.ObjectID{short.ID, longC.ID})
	require.NoError(t, err)
	assert.Equal(t, 3, got[short.ID].Value)
	assert.Equal(t, 3, got[longC.ID].Value)
	assert.NotNil(t, got[short.ID].ReachedAt, "le court est gagné")
	assert.Nil(t, got[longC.ID].ReachedAt, "le long continue")
}

// Un objectif en BROUILLON ne compte rien : sans cela, une faute de frappe
// serait déjà gagnée avant qu'on la voie.
func TestADraftCountsNothing(t *testing.T) {
	f := newFixture(t)
	c, err := f.svc.Create(f.ctx, "admin", driverGoal())
	require.NoError(t, err)

	require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, "r1", 1))
	uid, _ := primitive.ObjectIDFromHex(driverID)
	got, err := f.store.ProgressOf(f.ctx, uid, []primitive.ObjectID{c.ID})
	require.NoError(t, err)
	assert.Zero(t, got[c.ID].Value)
}

// ⚠️ UN OBJECTIF LANCÉ NE SE RÉÉCRIT PAS. Changer la cible pendant qu'il court
// changerait les règles sous les pieds de gens qui jouent déjà : un chauffeur à
// 18 sur 20 verrait soudain 30.
func TestALiveObjectiveCannotBeRewritten(t *testing.T) {
	f := newFixture(t)
	c := f.live(t, driverGoal())
	harder := driverGoal()
	harder.Target = 30
	_, err := f.svc.Update(f.ctx, c.ID.Hex(), harder)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "draft")
}

// ⚠️ UNE RÉFÉRENCE VIDE EST REFUSÉE. Sans elle, il n'y a pas de déduplication,
// et un appel réessayé paierait un bonus pour rien.
func TestAnEventWithoutAReferenceIsRefused(t *testing.T) {
	f := newFixture(t)
	f.live(t, driverGoal())
	err := f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, "", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "counting twice")
}

// ⚠️⚠️ UN CHAUFFEUR VTC N'EST PAS PAYÉ PAR LE SOCLE, ET SON BONUS N'EST PAS
// PERDU. Il n'a pas de portefeuille ici : son argent vit dans le grand livre des
// courses, et le socle ne peut pas appeler une verticale. Le bonus reste donc
// DÛ — la place de l'enveloppe est TENUE, le droit est enregistré —, et la
// verticale vient le chercher. C'est le chemin des cautions de matériel.
func TestADriverBonusStaysOwedUntilTheVerticalCollectsIt(t *testing.T) {
	f := newFixture(t)
	// Pas de grand livre branché : le cas du déploiement réel.
	f.svc.SetLedger(nil)
	c := f.live(t, driverGoal())

	for _, ref := range []string{"r1", "r2", "r3"} {
		require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, ref, 1))
	}
	uid, _ := primitive.ObjectIDFromHex(driverID)
	got, err := f.store.ProgressOf(f.ctx, uid, []primitive.ObjectID{c.ID})
	require.NoError(t, err)
	require.NotNil(t, got[c.ID].ReachedAt, "le droit est acquis")
	assert.Nil(t, got[c.ID].PaidAt, "et l'argent n'a pas encore bougé")
	assert.False(t, got[c.ID].Missed, "ce n'est PAS un manqué : l'enveloppe a payé")
	// ⚠️ LA PLACE EST TENUE : l'argent est engagé, et l'enveloppe doit le
	// savoir. La rendre aurait laissé un second gagnant la prendre sur un
	// budget déjà dépensé.
	assert.Equal(t, 1, f.store.challenges[c.ID].Counters.UsesReserved)
	// ⚠️ ET LA PERSONNE EST PRÉVENUE QUAND MÊME. Attendre l'argent pour
	// annoncer la victoire ferait douter quelqu'un qui a compté ses courses.
	assert.Len(t, f.notif.sent, 1)

	// La verticale vient chercher.
	owed, err := f.svc.ClaimOwed(f.ctx, driverID)
	require.NoError(t, err)
	require.Len(t, owed, 1)
	assert.Equal(t, 5000, owed[0].AmountXOF)
	assert.Equal(t, "3 courses", owed[0].Title)
	// La place passe de « promis » à « dépensé ».
	cur := f.store.challenges[c.ID]
	assert.Zero(t, cur.Counters.UsesReserved)
	assert.Equal(t, 1, cur.Counters.UsesSpent)

	// ⚠️ ET UN SECOND APPEL NE REND RIEN : une verticale réessaie, et
	// reprendre ce qu'elle a déjà encaissé paierait deux fois.
	again, err := f.svc.ClaimOwed(f.ctx, driverID)
	require.NoError(t, err)
	assert.Empty(t, again)
}

// ⚠️ UN MANQUÉ N'EST PAS DÛ. La cible a été franchie sans enveloppe pour la
// payer, et c'est à l'exploitation de trancher : le servir à la verticale ferait
// payer par elle un bonus que le socle a refusé.
func TestAMissedTargetIsNotOwedToTheVertical(t *testing.T) {
	f := newFixture(t)
	f.svc.SetLedger(nil)
	g := driverGoal()
	g.Limits = promo.Limits{MaxUses: 0, BudgetXOF: 1_000_000}
	f.live(t, g)
	f.store.claimFails = true

	for _, ref := range []string{"r1", "r2", "r3"} {
		require.NoError(t, f.svc.Report(f.ctx, ForDriver, MetricRidesDone, driverID, ref, 1))
	}
	owed, err := f.svc.ClaimOwed(f.ctx, driverID)
	require.NoError(t, err)
	assert.Empty(t, owed, "un manqué attend une décision humaine, pas un versement")
}
