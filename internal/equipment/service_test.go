package equipment

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// --- doublures ---------------------------------------------------------------

type memStore struct {
	items     map[primitive.ObjectID]*Item
	contracts map[primitive.ObjectID]*Contract
	settings  map[string]*Settings
}

func newMemStore() *memStore {
	return &memStore{items: map[primitive.ObjectID]*Item{}, contracts: map[primitive.ObjectID]*Contract{}, settings: map[string]*Settings{}}
}

func (m *memStore) InsertItem(_ context.Context, it *Item) error {
	it.ID = primitive.NewObjectID()
	cp := *it
	m.items[it.ID] = &cp
	return nil
}
func (m *memStore) ItemByID(_ context.Context, id primitive.ObjectID) (*Item, error) {
	it, ok := m.items[id]
	if !ok {
		return nil, errItemNotFound
	}
	cp := *it
	return &cp, nil
}
func (m *memStore) ListItems(_ context.Context, onlyActive bool) ([]Item, error) {
	var out []Item
	for _, it := range m.items {
		if onlyActive && !it.Active {
			continue
		}
		out = append(out, *it)
	}
	return out, nil
}
func (m *memStore) SaveItem(_ context.Context, it *Item) error {
	cp := *it
	m.items[it.ID] = &cp
	return nil
}
func (m *memStore) AdjustStock(_ context.Context, id primitive.ObjectID, delta int) error {
	it := m.items[id]
	if it == nil || !it.TrackStock {
		return nil
	}
	if it.Stock+delta < 0 {
		return apperr.Conflict("equipment_out_of_stock", "out of stock")
	}
	it.Stock += delta
	return nil
}
func (m *memStore) InsertContract(_ context.Context, c *Contract) error {
	c.ID = primitive.NewObjectID()
	c.CreatedAt = time.Now()
	if c.Schedule == nil {
		c.Schedule = []Line{}
	}
	if c.Payments == nil {
		c.Payments = []Payment{}
	}
	cp := *c
	m.contracts[c.ID] = &cp
	return nil
}
func (m *memStore) ContractByID(_ context.Context, id primitive.ObjectID) (*Contract, error) {
	c, ok := m.contracts[id]
	if !ok {
		return nil, errContractNotFound
	}
	cp := *c
	return &cp, nil
}
func (m *memStore) SaveContract(_ context.Context, c *Contract) error {
	cp := *c
	m.contracts[c.ID] = &cp
	return nil
}
func (m *memStore) ListContracts(_ context.Context, f ContractFilter, limit int, _ string) ([]Contract, string, error) {
	var out []Contract
	for _, c := range m.contracts {
		if f.Status != "" && c.Status != f.Status {
			continue
		}
		out = append(out, *c)
	}
	return out, "", nil
}
func (m *memStore) ContractsOfUser(_ context.Context, userID primitive.ObjectID, onlyActive bool) ([]Contract, error) {
	var out []Contract
	for _, c := range m.contracts {
		if c.UserID != userID || (onlyActive && c.Status != StatusActive) {
			continue
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *memStore) ActiveContracts(_ context.Context) ([]Contract, error) {
	var out []Contract
	for _, c := range m.contracts {
		if c.Status == StatusActive {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (m *memStore) Settings(_ context.Context, code string) (*Settings, error) {
	if s, ok := m.settings[code]; ok {
		cp := *s
		return &cp, nil
	}
	d := DefaultSettings(code)
	return &d, nil
}
func (m *memStore) SaveSettings(_ context.Context, s *Settings) error {
	cp := *s
	m.settings[s.Country] = &cp
	return nil
}

// memPurse : le solde Dira d'un livreur.
type memPurse struct {
	balance map[string]int
	keys    map[string]bool
}

func (p *memPurse) ChargeEquipment(_ context.Context, ownerID string, amount int, partial bool, _ string, key string) (int, error) {
	if p.keys[key] {
		return 0, nil
	}
	p.keys[key] = true
	have := p.balance[ownerID]
	if have >= amount {
		p.balance[ownerID] -= amount
		return amount, nil
	}
	if !partial || have <= 0 {
		return 0, nil
	}
	p.balance[ownerID] = 0
	return have, nil
}
func (p *memPurse) RefundEquipment(_ context.Context, ownerID string, amount int, _ string, _ string) error {
	p.balance[ownerID] += amount
	return nil
}

type memAccounts struct{}

func (memAccounts) CountryOf(context.Context, string) (string, error) { return "TG", nil }
func (memAccounts) UserNames(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		out[id] = "Koffi"
	}
	return out, nil
}

type sent struct {
	to, key string
	vars    map[string]string
}
type memNotifier struct{ sent []sent }

func (n *memNotifier) Notify(_ context.Context, userID, key string, vars, _ map[string]string) {
	n.sent = append(n.sent, sent{userID, key, vars})
}

type memStaff struct{ keys []string }

func (m *memStaff) AlertStaff(_ context.Context, _, _, key string, _, _ map[string]string) {
	m.keys = append(m.keys, key)
}

func desk(t *testing.T) (*Service, *memStore, *memPurse, *memNotifier, *memStaff, context.Context) {
	t.Helper()
	store := newMemStore()
	purse := &memPurse{balance: map[string]int{}, keys: map[string]bool{}}
	svc := NewService(store, purse, memAccounts{})
	n := &memNotifier{}
	st := &memStaff{}
	svc.SetNotifier(n)
	svc.SetStaffAlerter(st)
	clock := day0
	svc.now = func() time.Time { return clock }
	ctx := country.WithCountry(context.Background(), "TG", country.SourceClaims)
	return svc, store, purse, n, st, ctx
}

func (s *Service) advance(d time.Duration) {
	now := s.now().Add(d)
	s.now = func() time.Time { return now }
}

const courierID = "6a9f000000000000000000d1"
const driverID = "6a9f000000000000000000d2"

func vest(t *testing.T, svc *Service, ctx context.Context) ItemResponse {
	t.Helper()
	it, err := svc.CreateItem(ctx, ItemInput{Kind: KindVest, Name: "Gilet Dira", SalePriceXOF: 6_000, RentalWeeklyXOF: 500, DepositXOF: 2_000, Stock: 3, TrackStock: true})
	require.NoError(t, err)
	return *it
}

// --- la vente à un livreur, retenue sur les gains ------------------------------

func TestASaleToACourierIsCollectedFromEarningsThenSettled(t *testing.T) {
	svc, store, purse, n, _, ctx := desk(t)
	it := vest(t, svc, ctx)
	purse.balance[courierID] = 0

	c, err := svc.CreateContract(ctx, "admin", ContractInput{
		UserID: courierID, Vertical: VerticalFood, ItemID: it.ID, Mode: ModeSale, Quantity: 1,
		Plan: &Plan{Schedule: ScheduleInstallments, Installments: 3, Period: PeriodWeekly, FirstDueDays: 7,
			CollectFromEarnings: true, EarningsPercent: 20, DepositRefundable: true},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusDraft, c.Status, "l'acceptation est exigée par défaut")
	assert.Equal(t, 6_000, c.PriceXOF)
	assert.Equal(t, 2_000, c.DepositXOF, "la caution de l'article")
	assert.Equal(t, KeyContractProposed, n.sent[len(n.sent)-1].key)

	// L'agent accepte, l'exploitation remet.
	_, err = svc.HandOver(ctx, "admin", c.ID)
	require.Error(t, err, "pas encore accepté")
	_, err = svc.Accept(ctx, courierID, c.ID)
	require.NoError(t, err)
	handed, err := svc.HandOver(ctx, "admin", c.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, handed.Status)
	assert.Len(t, handed.Schedule, 4, "caution + 3 échéances")
	assert.Equal(t, 8_000, handed.OutstandingXOF)
	assert.Equal(t, 2, store.items[mustID(it.ID)].Stock, "le stock a baissé")

	// Un gain de 1 500 : 20 % retenus = 300 (le solde vient d'être crédité).
	purse.balance[courierID] = 1_500
	col, err := svc.Collect(ctx, courierID, VerticalFood, 1_500, "order", "6a9f00000000000000000001")
	require.NoError(t, err)
	assert.Equal(t, 300, col.AmountXOF)
	assert.Equal(t, 1_200, purse.balance[courierID])
	require.Len(t, col.Lines, 1)
	assert.Equal(t, SourceEarnings, col.Lines[0].Source)
	// Rejouer le même gain ne retient pas deux fois.
	col, err = svc.Collect(ctx, courierID, VerticalFood, 1_500, "order", "6a9f00000000000000000001")
	require.NoError(t, err)
	assert.Equal(t, 0, col.AmountXOF)

	// Le solde paie tout le reste ; la vente est soldée.
	purse.balance[courierID] = 20_000
	paid, err := svc.Pay(ctx, courierID, c.ID, PayInput{AmountXOF: 50_000})
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, paid.Status)
	assert.Equal(t, 0, paid.OutstandingXOF)
	assert.Equal(t, 20_000-7_700, purse.balance[courierID], "jamais plus que ce qui est dû")
}

// --- la location à un chauffeur VTC, portée au grand livre ---------------------

func TestARentalToADriverIsCollectedByTheVerticalAndTheDepositComesBack(t *testing.T) {
	svc, _, _, n, _, ctx := desk(t)
	it := vest(t, svc, ctx)
	c, err := svc.CreateContract(ctx, "admin", ContractInput{
		UserID: driverID, Vertical: VerticalVTC, ItemID: it.ID, Mode: ModeRental, HandOverNow: true,
		Plan: &Plan{Period: PeriodWeekly, CollectFromEarnings: false, CollectFromWallet: true, DepositRefundable: true},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusActive, c.Status)
	assert.Equal(t, 500, c.PriceXOF, "le loyer hebdomadaire de l'article")
	assert.Equal(t, 2_500, c.DueXOF, "caution + première semaine, dues à la remise")

	// La verticale règle une course : ce qui est échu est porté au grand livre.
	col, err := svc.Collect(ctx, driverID, VerticalVTC, 3_000, "ride", "6a9f00000000000000000002")
	require.NoError(t, err)
	assert.Equal(t, 2_500, col.AmountXOF)
	assert.Equal(t, SourceLedger, col.Lines[0].Source)
	// Plus rien d'échu : la course suivante ne retient rien.
	col, err = svc.Collect(ctx, driverID, VerticalVTC, 3_000, "ride", "6a9f00000000000000000003")
	require.NoError(t, err)
	assert.Equal(t, 0, col.AmountXOF)

	// Une semaine plus tard, le balayage ouvre la période suivante.
	svc.advance(8 * 24 * time.Hour)
	svc.RunDue(ctx)
	got, err := svc.GetContract(ctx, c.ID)
	require.NoError(t, err)
	assert.Len(t, got.Schedule, 3)
	assert.Equal(t, 500, got.DueXOF)

	// Retour en bon état : la caution revient, rendue au grand livre à la
	// prochaine lecture de la verticale.
	ret, err := svc.Return(ctx, "admin", c.ID, ReturnInput{Condition: "bon état"})
	require.NoError(t, err)
	assert.Equal(t, StatusReturned, ret.Status)
	assert.Equal(t, 500, ret.OutstandingXOF, "la semaine entamée reste due")
	col, err = svc.Collect(ctx, driverID, VerticalVTC, 1_000, "ride", "6a9f00000000000000000004")
	require.NoError(t, err)
	assert.Equal(t, -2_000, col.AmountXOF, "caution rendue ; le loyer dû n'est plus retenu sur un contrat rendu")
	assert.Equal(t, KeyReturned, n.sent[len(n.sent)-1].key)
}

// --- le balayage : rappel, retard, pénalité, blocage, prélèvement ---------------

func TestTheSweepRemindsAgesChargesAndBlocks(t *testing.T) {
	svc, _, purse, n, staff, ctx := desk(t)
	it := vest(t, svc, ctx)
	_, err := svc.UpdateSettings(ctx, SettingsInput{RequireAcceptance: boolp(false)})
	require.NoError(t, err)
	c, err := svc.CreateContract(ctx, "admin", ContractInput{
		UserID: courierID, Vertical: VerticalFood, ItemID: it.ID, Mode: ModeSale, HandOverNow: true, DepositXOF: intp(0),
		Plan: &Plan{Schedule: ScheduleInstallments, Installments: 2, Period: PeriodWeekly, FirstDueDays: 7,
			CollectFromWallet: true, AllowPartial: true, ReminderDays: 2, GraceDays: 1, LateFeeXOF: 200, BlockAfterDays: 3},
	})
	require.NoError(t, err)
	n.sent = nil

	// J+5 : rappel, deux jours avant l'échéance.
	svc.advance(5 * 24 * time.Hour)
	svc.RunDue(ctx)
	require.NotEmpty(t, n.sent)
	assert.Equal(t, KeyDue, n.sent[0].key)
	n.sent = nil

	// J+7 : échue ; le solde n'a que 1 000 sur 3 000 → prélèvement partiel.
	svc.advance(2 * 24 * time.Hour)
	purse.balance[courierID] = 1_000
	svc.RunDue(ctx)
	got, _ := svc.GetContract(ctx, c.ID)
	assert.Equal(t, 1_000, got.PaidXOF)
	assert.Equal(t, 0, purse.balance[courierID])
	assert.Equal(t, KeyCharged, n.sent[0].key)
	n.sent = nil

	// J+9 : en retard, pénalité une fois, l'équipe prévenue.
	svc.advance(2 * 24 * time.Hour)
	svc.RunDue(ctx)
	got, _ = svc.GetContract(ctx, c.ID)
	assert.Equal(t, LineOverdue, got.Schedule[0].Status)
	assert.Equal(t, 200, got.Schedule[0].LateFeeXOF)
	assert.Equal(t, KeyOverdue, n.sent[0].key)
	assert.Contains(t, staff.keys, KeyStaffOverdue)
	assert.False(t, got.Blocked)
	n.sent = nil

	// J+11 : bloqué (grâce 1 + 3 jours).
	svc.advance(2 * 24 * time.Hour)
	svc.RunDue(ctx)
	got, _ = svc.GetContract(ctx, c.ID)
	assert.True(t, got.Blocked)
	st, err := svc.Standing(ctx, courierID)
	require.NoError(t, err)
	assert.True(t, st.Blocked)
	assert.Equal(t, 2_200, st.OverdueXOF)
	assert.Equal(t, KeyBlocked, n.sent[len(n.sent)-1].key)

	// Une remise de la pénalité et un encaissement lèvent tout.
	_, err = svc.RecordPayment(ctx, "admin", c.ID, PaymentInput{AmountXOF: 2_200, Source: SourceManual, Note: "espèces au bureau"})
	require.NoError(t, err)
	st, _ = svc.Standing(ctx, courierID)
	assert.False(t, st.Blocked)
}

// --- la demande depuis l'application ---------------------------------------------

func TestAnAgentCanRequestAnItemWhenTheCountryAllowsIt(t *testing.T) {
	svc, _, _, _, staff, ctx := desk(t)
	it := vest(t, svc, ctx)
	req, err := svc.Request(ctx, courierID, VerticalFood, RequestInput{ItemID: it.ID, Mode: ModeSale})
	require.NoError(t, err)
	assert.Equal(t, StatusRequested, req.Status)
	assert.Contains(t, staff.keys, KeyStaffRequested)
	// Qualifiée, elle devient un projet à accepter.
	q, err := svc.Qualify(ctx, "admin", req.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusDraft, q.Status)

	_, err = svc.UpdateSettings(ctx, SettingsInput{AgentCanRequest: boolp(false)})
	require.NoError(t, err)
	_, err = svc.Request(ctx, courierID, VerticalFood, RequestInput{ItemID: it.ID, Mode: ModeSale})
	require.Error(t, err)
	assert.Equal(t, "equipment_requests_closed", apperr.From(err).Code)
}

func TestSettingsBoundTheEarningsPercent(t *testing.T) {
	svc, _, _, _, _, ctx := desk(t)
	_, err := svc.UpdateSettings(ctx, SettingsInput{MaxEarningsPercent: intp(30)})
	require.NoError(t, err)
	it := vest(t, svc, ctx)
	_, err = svc.CreateContract(ctx, "admin", ContractInput{UserID: courierID, Vertical: VerticalFood, ItemID: it.ID, Mode: ModeSale,
		Plan: &Plan{Schedule: ScheduleInstallments, Installments: 2, CollectFromEarnings: true, EarningsPercent: 40}})
	require.Error(t, err)
	assert.Equal(t, []string{"plan.earnings_percent"}, apperr.From(err).Meta["fields"])
}

func boolp(b bool) *bool { return &b }
func intp(n int) *int    { return &n }
func mustID(s string) primitive.ObjectID {
	id, _ := primitive.ObjectIDFromHex(s)
	return id
}
