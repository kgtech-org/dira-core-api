package equipment

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Notification keys — gabarits dans `internal/notify`.
const (
	KeyContractProposed = "equipment_contract_proposed" // à accepter dans l'app
	KeyHandedOver       = "equipment_handed_over"       // remis, l'échéancier démarre
	KeyDue              = "equipment_due"               // rappel avant échéance
	KeyCharged          = "equipment_charged"           // prélevé / retenu
	KeyOverdue          = "equipment_overdue"           // en retard
	KeyBlocked          = "equipment_blocked"           // bloqué : plus de mise en ligne
	KeyReturned         = "equipment_returned"          // rendu, caution
	KeyStaffOverdue     = "staff_equipment_overdue"
	KeyStaffRequested   = "staff_equipment_requested"
)

var (
	errNotYours       = apperr.Forbidden("forbidden", "this contract is not yours")
	errModeNotAllowed = apperr.Conflict("equipment_mode_not_allowed", "this mode is not offered in this country")
	errBadTransition  = apperr.Conflict("equipment_bad_status", "this action does not apply to the contract in its current status")
	errNotOffered     = apperr.Conflict("equipment_not_offered", "this item is not offered under this mode")
	errNoRequest      = apperr.Forbidden("equipment_requests_closed", "requests from the app are not open in this country")
)

// Purse est le SOLDE DIRA des livreurs (le module token). Les chauffeurs VTC
// n'en ont pas ici : leur argent est au grand livre des courses, et c'est la
// verticale qui vient chercher ce qui est dû (`Collect`).
type Purse interface {
	ChargeEquipment(ctx context.Context, ownerID string, amountXOF int, allowPartial bool, contractID, key string) (int, error)
	RefundEquipment(ctx context.Context, ownerID string, amountXOF int, contractID, key string) error
}

// Notifier atteint l'agent et l'équipe.
type Notifier interface {
	Notify(ctx context.Context, userID, key string, vars, data map[string]string)
}

// StaffAlerter prévient les membres dont le périmètre couvre la verticale.
type StaffAlerter interface {
	AlertStaff(ctx context.Context, scope, country, key string, vars, data map[string]string)
}

// Accounts : ce que le socle sait d'une personne — son pays, son nom.
type Accounts interface {
	CountryOf(ctx context.Context, userID string) (string, error)
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
}

type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Store est ce que le service attend de la persistance — déclaré ici, côté
// consommateur, pour que les parcours d'argent se testent sans Mongo.
type Store interface {
	InsertItem(ctx context.Context, it *Item) error
	ItemByID(ctx context.Context, id primitive.ObjectID) (*Item, error)
	ListItems(ctx context.Context, onlyActive bool) ([]Item, error)
	SaveItem(ctx context.Context, it *Item) error
	AdjustStock(ctx context.Context, id primitive.ObjectID, delta int) error
	InsertContract(ctx context.Context, c *Contract) error
	ContractByID(ctx context.Context, id primitive.ObjectID) (*Contract, error)
	SaveContract(ctx context.Context, c *Contract) error
	ListContracts(ctx context.Context, f ContractFilter, limit int, cursor string) ([]Contract, string, error)
	ContractsOfUser(ctx context.Context, userID primitive.ObjectID, onlyActive bool) ([]Contract, error)
	ActiveContracts(ctx context.Context) ([]Contract, error)
	Settings(ctx context.Context, code string) (*Settings, error)
	SaveSettings(ctx context.Context, s *Settings) error
}

type Service struct {
	repo     Store
	purse    Purse
	notifier Notifier
	staff    StaffAlerter
	accounts Accounts
	auditor  Auditor
	now      func() time.Time
}

func NewService(repo Store, purse Purse, accounts Accounts) *Service {
	return &Service{repo: repo, purse: purse, accounts: accounts, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetNotifier(n Notifier)         { s.notifier = n }
func (s *Service) SetStaffAlerter(a StaffAlerter) { s.staff = a }
func (s *Service) SetAuditor(a Auditor)           { s.auditor = a }

func (s *Service) notify(ctx context.Context, userID, key string, vars, data map[string]string) {
	if s.notifier != nil {
		s.notifier.Notify(ctx, userID, key, vars, data)
	}
}

func (s *Service) alert(ctx context.Context, c *Contract, key string, vars map[string]string) {
	if s.staff != nil {
		s.staff.AlertStaff(ctx, c.Vertical, c.Country, key, vars, s.data(c))
	}
}

func (s *Service) record(ctx context.Context, action string, c *Contract, before any) {
	if s.auditor != nil {
		s.auditor.Record(ctx, action, "equipment_contract", c.ID.Hex(), before, auditView(c))
	}
}

func auditView(c *Contract) map[string]any {
	return map[string]any{"status": c.Status, "price_xof": c.PriceXOF, "deposit_xof": c.DepositXOF, "mode": c.Mode}
}

func (s *Service) data(c *Contract) map[string]string {
	return map[string]string{"type": "equipment", "contract_id": c.ID.Hex(), "vertical": c.Vertical}
}

func money(n int) string { return fmt.Sprintf("%d F", n) }

// --- settings ---

func (s *Service) GetSettings(ctx context.Context) (*SettingsResponse, error) {
	st, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := toSettings(st)
	return &out, nil
}

func (s *Service) UpdateSettings(ctx context.Context, req SettingsInput) (*SettingsResponse, error) {
	st, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if req.AllowedModes != nil {
		st.AllowedModes = req.AllowedModes
	}
	if req.AgentCanRequest != nil {
		st.AgentCanRequest = *req.AgentCanRequest
	}
	if req.RequireAcceptance != nil {
		st.RequireAcceptance = *req.RequireAcceptance
	}
	if req.MaxEarningsPercent != nil {
		st.MaxEarningsPercent = *req.MaxEarningsPercent
	}
	if req.DefaultDepositPercent != nil {
		st.DefaultDepositPercent = *req.DefaultDepositPercent
	}
	if req.StaffAlertOverdue != nil {
		st.StaffAlertOverdue = *req.StaffAlertOverdue
	}
	if req.Defaults != nil {
		if err := validatePlan(*req.Defaults, st.MaxEarningsPercent); err != nil {
			return nil, err
		}
		st.Defaults = *req.Defaults
	}
	if err := s.repo.SaveSettings(ctx, st); err != nil {
		return nil, apperr.Internal(err)
	}
	if s.auditor != nil {
		s.auditor.Record(ctx, "equipment.settings", "equipment_settings", st.Country, nil, st.Defaults)
	}
	out := toSettings(st)
	return &out, nil
}

func validatePlan(p Plan, maxPercent int) error {
	fail := func(field, reason string) error {
		return apperr.Validation("invalid plan").WithMeta(map[string]any{"fields": []string{field}, "reason": reason})
	}
	switch p.Schedule {
	case ScheduleUpfront, ScheduleInstallments, SchedulePerPeriod, ScheduleNone, "":
	default:
		return fail("plan.schedule", "unknown_schedule")
	}
	switch p.Period {
	case PeriodDaily, PeriodWeekly, PeriodBiweekly, PeriodMonthly, "":
	default:
		return fail("plan.period", "unknown_period")
	}
	if p.Installments < 0 || p.Installments > 60 {
		return fail("plan.installments", "out_of_range")
	}
	if p.EarningsPercent < 0 || p.EarningsPercent > 100 || (maxPercent > 0 && p.EarningsPercent > maxPercent) {
		return fail("plan.earnings_percent", "over_max")
	}
	if p.LateFeePercent < 0 || p.LateFeePercent > 100 {
		return fail("plan.late_fee_percent", "out_of_range")
	}
	for f, v := range map[string]int{
		"plan.earnings_fixed_xof": p.EarningsFixedXOF, "plan.min_left_xof": p.MinLeftXOF, "plan.daily_cap_xof": p.DailyCapXOF,
		"plan.weekly_cap_xof": p.WeeklyCapXOF, "plan.grace_days": p.GraceDays, "plan.late_fee_xof": p.LateFeeXOF,
		"plan.block_after_days": p.BlockAfterDays, "plan.reminder_days": p.ReminderDays, "plan.first_due_days": p.FirstDueDays,
	} {
		if v < 0 {
			return fail(f, "negative")
		}
	}
	return nil
}

// --- items ---

func (s *Service) ListItems(ctx context.Context, onlyActive bool) ([]ItemResponse, error) {
	items, err := s.repo.ListItems(ctx, onlyActive)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]ItemResponse, 0, len(items))
	for i := range items {
		out = append(out, toItem(&items[i]))
	}
	return out, nil
}

// Catalogue : ce qu'un agent peut demander — actif, dans son pays, pour sa
// verticale.
func (s *Service) Catalogue(ctx context.Context, vertical string) ([]ItemResponse, error) {
	items, err := s.repo.ListItems(ctx, true)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]ItemResponse, 0, len(items))
	for i := range items {
		if len(items[i].Audiences) > 0 && !contains(items[i].Audiences, vertical) {
			continue
		}
		out = append(out, toItem(&items[i]))
	}
	return out, nil
}

func (s *Service) CreateItem(ctx context.Context, req ItemInput) (*ItemResponse, error) {
	if req.DefaultPlan != nil {
		if err := validatePlan(*req.DefaultPlan, 0); err != nil {
			return nil, err
		}
	}
	it := &Item{Country: country.FromContext(ctx), Active: true}
	applyItem(it, req)
	if err := s.repo.InsertItem(ctx, it); err != nil {
		return nil, apperr.Internal(err)
	}
	out := toItem(it)
	return &out, nil
}

func (s *Service) UpdateItem(ctx context.Context, id string, req ItemInput) (*ItemResponse, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errItemNotFound
	}
	if req.DefaultPlan != nil {
		if err := validatePlan(*req.DefaultPlan, 0); err != nil {
			return nil, err
		}
	}
	it, err := s.repo.ItemByID(ctx, oid)
	if err != nil {
		return nil, err
	}
	applyItem(it, req)
	if err := s.repo.SaveItem(ctx, it); err != nil {
		return nil, err
	}
	out := toItem(it)
	return &out, nil
}

func applyItem(it *Item, req ItemInput) {
	it.Kind, it.Name, it.Description = req.Kind, strings.TrimSpace(req.Name), strings.TrimSpace(req.Description)
	it.Photos, it.Audiences = req.Photos, req.Audiences
	it.SalePriceXOF, it.DepositXOF = req.SalePriceXOF, req.DepositXOF
	it.RentalDailyXOF, it.RentalWeeklyXOF, it.RentalMonthlyXOF = req.RentalDailyXOF, req.RentalWeeklyXOF, req.RentalMonthlyXOF
	it.Stock, it.TrackStock = req.Stock, req.TrackStock
	if req.Active != nil {
		it.Active = *req.Active
	}
	it.DefaultPlan = req.DefaultPlan
}

// --- contracts : l'exploitation ---

func (s *Service) ListContracts(ctx context.Context, f ContractFilter, page httpx.Page) ([]ContractResponse, string, error) {
	items, next, err := s.repo.ListContracts(ctx, f, page.Limit, page.Cursor)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	out := s.responses(ctx, items, true)
	return out, next, nil
}

func (s *Service) responses(ctx context.Context, items []Contract, withNames bool) []ContractResponse {
	now := s.now()
	out := make([]ContractResponse, 0, len(items))
	var ids []string
	for i := range items {
		out = append(out, toContract(&items[i], now))
		ids = append(ids, items[i].UserID.Hex())
	}
	if withNames && s.accounts != nil && len(ids) > 0 {
		if names, err := s.accounts.UserNames(ctx, ids); err == nil {
			for i := range out {
				out[i].UserName = names[out[i].UserID]
			}
		}
	}
	return out
}

func (s *Service) GetContract(ctx context.Context, id string) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

func (s *Service) load(ctx context.Context, id string) (*Contract, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errContractNotFound
	}
	return s.repo.ContractByID(ctx, oid)
}

// priceOf rend le prix d'un article pour un mode et un plan : le prix de
// vente, ou le loyer de la période du plan.
func priceOf(it *Item, mode, period string) (int, bool) {
	switch mode {
	case ModeSale:
		return it.SalePriceXOF, it.SalePriceXOF > 0
	case ModeRental:
		switch period {
		case PeriodDaily:
			return it.RentalDailyXOF, it.RentalDailyXOF > 0
		case PeriodMonthly:
			return it.RentalMonthlyXOF, it.RentalMonthlyXOF > 0
		default:
			return it.RentalWeeklyXOF, it.RentalWeeklyXOF > 0
		}
	default:
		return 0, true
	}
}

// CreateContract : l'exploitation prépare un contrat pour une personne.
//
// Les réglages tombent en cascade — le contrat, puis l'article, puis le pays
// — et chaque montant absent vient de l'article : rien n'est laissé à
// deviner, tout peut être surchargé.
func (s *Service) CreateContract(ctx context.Context, actorID string, req ContractInput) (*ContractResponse, error) {
	uid, _ := primitive.ObjectIDFromHex(req.UserID)
	iid, _ := primitive.ObjectIDFromHex(req.ItemID)
	st, err := s.repo.Settings(ctx, country.FromContext(ctx))
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if !contains(st.AllowedModes, req.Mode) {
		return nil, errModeNotAllowed
	}
	it, err := s.repo.ItemByID(ctx, iid)
	if err != nil {
		return nil, err
	}
	// La personne existe, et elle est de ce pays.
	if s.accounts != nil {
		cc, err := s.accounts.CountryOf(ctx, req.UserID)
		if err != nil {
			return nil, err
		}
		if cc != "" && cc != country.FromContext(ctx) {
			return nil, apperr.Validation("this account belongs to another country").
				WithMeta(map[string]any{"fields": []string{"user_id"}, "reason": "other_country"})
		}
	}
	qty := req.Quantity
	if qty < 1 {
		qty = 1
	}
	plan := st.Defaults
	if it.DefaultPlan != nil {
		plan = merge(*it.DefaultPlan, plan)
	}
	if req.Plan != nil {
		plan = merge(*req.Plan, plan)
	}
	plan = fitPlanToMode(plan, req.Mode)
	if err := validatePlan(plan, st.MaxEarningsPercent); err != nil {
		return nil, err
	}
	unit, offered := priceOf(it, req.Mode, plan.Period)
	price := unit * qty
	if req.PriceXOF != nil {
		price = *req.PriceXOF
	} else if !offered {
		return nil, errNotOffered
	}
	deposit := it.DepositXOF * qty
	if deposit == 0 && st.DefaultDepositPercent > 0 && req.Mode != ModeSale {
		deposit = it.SalePriceXOF * qty * st.DefaultDepositPercent / 100
	}
	if req.DepositXOF != nil {
		deposit = *req.DepositXOF
	}
	now := s.now()
	c := &Contract{
		Country: country.FromContext(ctx), UserID: uid, Vertical: req.Vertical, ItemID: it.ID,
		ItemName: it.Name, ItemKind: it.Kind, Quantity: qty, Serial: strings.TrimSpace(req.Serial),
		Mode: req.Mode, Plan: plan, Status: StatusDraft, PriceXOF: price, DepositXOF: deposit,
		Notes: strings.TrimSpace(req.Notes), CreatedBy: actorID,
	}
	if !st.RequireAcceptance {
		c.Status = StatusAccepted
		c.AcceptedAt = &now
	}
	if err := s.repo.InsertContract(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	s.record(ctx, "equipment.create", c, nil)
	if req.HandOverNow {
		if st.RequireAcceptance {
			// Remis sans acceptation dans l'app : l'exploitation en prend la
			// responsabilité, et c'est écrit.
			c.Status = StatusAccepted
			c.AcceptedAt = &now
		}
		return s.handOver(ctx, actorID, c)
	}
	s.notify(ctx, c.UserID.Hex(), KeyContractProposed, map[string]string{"item": c.ItemName, "mode": modeLabel(c.Mode)}, s.data(c))
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// fitPlanToMode : un prêt n'a rien à échelonner, une location se paie par
// période, une vente ne se paie pas « par période ».
func fitPlanToMode(p Plan, mode string) Plan {
	switch mode {
	case ModeLoan:
		p.Schedule = ScheduleNone
	case ModeRental:
		p.Schedule = SchedulePerPeriod
		if p.Period == "" {
			p.Period = PeriodWeekly
		}
	case ModeSale:
		if p.Schedule == SchedulePerPeriod || p.Schedule == ScheduleNone {
			p.Schedule = ScheduleInstallments
		}
	}
	return p
}

func (s *Service) UpdateContract(ctx context.Context, actorID, id string, req ContractPatch) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	before := auditView(c)
	if req.Serial != nil {
		c.Serial = strings.TrimSpace(*req.Serial)
	}
	if req.Notes != nil {
		c.Notes = strings.TrimSpace(*req.Notes)
	}
	if req.Plan != nil {
		st, err := s.repo.Settings(ctx, c.Country)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		plan := fitPlanToMode(merge(*req.Plan, st.Defaults), c.Mode)
		if err := validatePlan(plan, st.MaxEarningsPercent); err != nil {
			return nil, err
		}
		// Le plan de recouvrement se règle à tout moment ; l'échéancier, lui,
		// est figé par la remise.
		c.Plan = plan
	}
	if req.PriceXOF != nil || req.DepositXOF != nil {
		if c.HandedAt != nil {
			return nil, apperr.Conflict("equipment_handed_over", "price and deposit are fixed once handed over")
		}
		if req.PriceXOF != nil {
			c.PriceXOF = *req.PriceXOF
		}
		if req.DepositXOF != nil {
			c.DepositXOF = *req.DepositXOF
		}
	}
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.update", c, before)
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// HandOver : l'article est remis, l'échéancier démarre, ce qui est dû à la
// remise (caution, comptant) est prélevé sur le solde si le plan le permet.
func (s *Service) HandOver(ctx context.Context, actorID, id string) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Status == StatusDraft || c.Status == StatusRequested {
		st, err := s.repo.Settings(ctx, c.Country)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		if st.RequireAcceptance && c.Status == StatusDraft {
			return nil, apperr.Conflict("equipment_not_accepted", "the agent has not accepted the terms yet")
		}
		if c.Status == StatusRequested {
			return nil, errBadTransition
		}
	}
	if c.Status != StatusAccepted && c.Status != StatusDraft {
		return nil, errBadTransition
	}
	return s.handOver(ctx, actorID, c)
}

func (s *Service) handOver(ctx context.Context, actorID string, c *Contract) (*ContractResponse, error) {
	now := s.now()
	before := auditView(c)
	if err := s.repo.AdjustStock(ctx, c.ItemID, -c.Quantity); err != nil {
		return nil, err
	}
	c.Status = StatusActive
	c.HandedAt = &now
	c.Schedule = buildSchedule(c, now)
	if c.Mode == ModeRental && c.Plan.Schedule == SchedulePerPeriod {
		next := periodAfter(now, c.Plan.Period)
		c.NextPeriodAt = &next
	}
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.hand_over", c, before)
	// Ce qui est dû le jour même, sur le solde — les livreurs seulement.
	s.chargeWallet(ctx, c, now)
	s.notify(ctx, c.UserID.Hex(), KeyHandedOver,
		map[string]string{"item": c.ItemName, "outstanding": money(c.Outstanding(now, false))}, s.data(c))
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// chargeWallet prélève sur le solde Dira ce qui est échu, quand le plan le
// permet et que la personne a un tel solde ici (livreurs). Enregistre le
// contrat après.
func (s *Service) chargeWallet(ctx context.Context, c *Contract, now time.Time) {
	if s.purse == nil || c.Vertical != VerticalFood || !c.Plan.CollectFromWallet || c.Status != StatusActive {
		return
	}
	due := c.Outstanding(now, true)
	if due <= 0 {
		return
	}
	key := fmt.Sprintf("equipment:%s:wallet:%s", c.ID.Hex(), now.Format("2006-01-02"))
	taken, err := s.purse.ChargeEquipment(ctx, c.UserID.Hex(), due, c.Plan.AllowPartial, c.ID.Hex(), key)
	if err != nil {
		slog.WarnContext(ctx, "equipment: wallet charge failed", "contract_id", c.ID.Hex(), "error", err)
		return
	}
	if taken <= 0 {
		return
	}
	s.addPayment(c, taken, SourceWallet, "", "", "", "", now)
	if err := s.repo.SaveContract(ctx, c); err != nil {
		slog.ErrorContext(ctx, "equipment: contract not saved after wallet charge", "contract_id", c.ID.Hex(), "error", err)
		return
	}
	s.notify(ctx, c.UserID.Hex(), KeyCharged, map[string]string{"item": c.ItemName, "amount": money(taken), "source": "solde Dira"}, s.data(c))
}

func (s *Service) addPayment(c *Contract, amount int, source, refKind, refID, note, actor string, at time.Time) {
	c.Payments = append(c.Payments, Payment{
		ID: primitive.NewObjectID(), At: at, AmountXOF: amount, Source: source,
		RefKind: refKind, RefID: refID, Note: note, ActorID: actor,
	})
	if amount > 0 {
		c.apply(amount, at)
		c.settle(at)
	}
}

// RecordPayment : l'exploitation encaisse (espèces, virement, mobile money
// constaté) ou remet.
func (s *Service) RecordPayment(ctx context.Context, actorID, id string, req PaymentInput) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusActive && c.Status != StatusReturned {
		return nil, errBadTransition
	}
	now := s.now()
	before := auditView(c)
	if req.Source == SourceWaiver {
		// Une remise s'applique aux lignes, sans argent.
		c.apply(req.AmountXOF, now)
		c.Payments = append(c.Payments, Payment{ID: primitive.NewObjectID(), At: now, AmountXOF: 0, Source: SourceWaiver, Note: req.Note, ActorID: actorID})
		c.settle(now)
	} else {
		s.addPayment(c, req.AmountXOF, req.Source, "", "", req.Note, actorID, now)
	}
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.payment", c, before)
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// Return : l'article revient. Location et prêt : la caution est rendue moins
// les dégâts ; ce qui reste dû reste dû.
func (s *Service) Return(ctx context.Context, actorID, id string, req ReturnInput) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusActive {
		return nil, errBadTransition
	}
	now := s.now()
	before := auditView(c)
	c.Status = StatusReturned
	c.ReturnedAt = &now
	c.ReturnCondition = strings.TrimSpace(req.Condition)
	c.DamageFeeXOF = req.DamageFeeXOF
	c.NextPeriodAt = nil
	if req.DamageFeeXOF > 0 {
		c.Schedule = append(c.Schedule, Line{N: len(c.Schedule) + 1, Kind: LineDamage, DueAt: now, AmountXOF: req.DamageFeeXOF, Status: LineDue})
	}
	refund := c.Plan.DepositRefundable
	if req.RefundDeposit != nil {
		refund = *req.RefundDeposit
	}
	refunded := 0
	if refund {
		paidDeposit := 0
		for _, l := range c.Schedule {
			if l.Kind == LineDeposit {
				paidDeposit += l.PaidXOF
			}
		}
		refunded = paidDeposit - req.DamageFeeXOF
		if refunded > 0 {
			key := fmt.Sprintf("equipment:%s:deposit-refund", c.ID.Hex())
			if c.Vertical == VerticalFood && s.purse != nil {
				if err := s.purse.RefundEquipment(ctx, c.UserID.Hex(), refunded, c.ID.Hex(), key); err != nil {
					return nil, err
				}
				c.Payments = append(c.Payments, Payment{ID: primitive.NewObjectID(), At: now, AmountXOF: -refunded, Source: SourceRefund, Note: "caution", ActorID: actorID})
			} else {
				// Chauffeur VTC : rendu au grand livre par la verticale, à sa
				// prochaine lecture (`Collect`).
				c.Payments = append(c.Payments, Payment{ID: primitive.NewObjectID(), At: now, AmountXOF: -refunded, Source: SourceRefund, Note: "caution", ActorID: actorID, Pending: true})
			}
		}
	}
	if err := s.repo.AdjustStock(ctx, c.ItemID, c.Quantity); err != nil {
		slog.WarnContext(ctx, "equipment: stock not restored", "contract_id", c.ID.Hex(), "error", err)
	}
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.return", c, before)
	vars := map[string]string{"item": c.ItemName, "refund": money(max(refunded, 0)), "owed": money(c.Outstanding(now, false))}
	s.notify(ctx, c.UserID.Hex(), KeyReturned, vars, s.data(c))
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

func (s *Service) Cancel(ctx context.Context, actorID, id, reason string) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	switch c.Status {
	case StatusRequested, StatusDraft, StatusAccepted:
	case StatusActive:
		// Un contrat vivant se clôt par un retour ou un défaut, pas une
		// annulation : l'article est dehors.
		return nil, errBadTransition
	default:
		return nil, errBadTransition
	}
	now := s.now()
	before := auditView(c)
	c.Status = StatusCancelled
	c.ClosedAt = &now
	if reason != "" {
		c.Notes = strings.TrimSpace(c.Notes + "\n" + reason)
	}
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.cancel", c, before)
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// Default : l'exploitation constate l'impayé et clôt.
func (s *Service) Default(ctx context.Context, actorID, id string) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusActive && c.Status != StatusReturned {
		return nil, errBadTransition
	}
	now := s.now()
	before := auditView(c)
	c.Status = StatusDefaulted
	c.ClosedAt = &now
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.default", c, before)
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// Waive : remettre une ligne entière (une pénalité, une échéance).
func (s *Service) Waive(ctx context.Context, actorID, id string, n int, note string) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	before := auditView(c)
	found := false
	for i := range c.Schedule {
		if c.Schedule[i].N == n && c.Schedule[i].owed() > 0 {
			c.Schedule[i].Status = LineWaived
			found = true
		}
	}
	if !found {
		return nil, apperr.NotFound("equipment_line_not_found", "no unpaid line with this number")
	}
	c.Payments = append(c.Payments, Payment{ID: primitive.NewObjectID(), At: now, Source: SourceWaiver, Note: note, ActorID: actorID})
	c.settle(now)
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.waive", c, before)
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// --- contracts : l'agent ---

func (s *Service) MyContracts(ctx context.Context, userID string) ([]ContractResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	items, err := s.repo.ContractsOfUser(ctx, uid, false)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return s.responses(ctx, items, false), nil
}

func (s *Service) loadMine(ctx context.Context, userID, id string) (*Contract, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.UserID.Hex() != userID {
		return nil, errNotYours
	}
	return c, nil
}

// Accept : l'agent accepte les conditions depuis l'application.
func (s *Service) Accept(ctx context.Context, userID, id string) (*ContractResponse, error) {
	c, err := s.loadMine(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusDraft {
		return nil, errBadTransition
	}
	now := s.now()
	c.Status = StatusAccepted
	c.AcceptedAt = &now
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.accept", c, nil)
	out := s.responses(ctx, []Contract{*c}, false)
	return &out[0], nil
}

// Pay : l'agent règle depuis son solde Dira (livreurs).
func (s *Service) Pay(ctx context.Context, userID, id string, req PayInput) (*ContractResponse, error) {
	c, err := s.loadMine(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusActive && c.Status != StatusReturned {
		return nil, errBadTransition
	}
	if c.Vertical != VerticalFood || s.purse == nil {
		return nil, apperr.Conflict("equipment_no_wallet", "this contract is settled on the rides ledger, not on a Dira balance")
	}
	now := s.now()
	amount := req.AmountXOF
	if owed := c.Outstanding(now, false); amount > owed {
		amount = owed
	}
	if amount <= 0 {
		return nil, apperr.Conflict("equipment_nothing_owed", "nothing is owed on this contract")
	}
	key := fmt.Sprintf("equipment:%s:pay:%d", c.ID.Hex(), now.UnixNano())
	taken, err := s.purse.ChargeEquipment(ctx, userID, amount, false, c.ID.Hex(), key)
	if err != nil {
		return nil, err
	}
	if taken == 0 {
		return nil, apperr.New("insufficient_funds", "your Dira balance does not cover this amount", 402)
	}
	s.addPayment(c, taken, SourceWallet, "", "", "", userID, now)
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	out := s.responses(ctx, []Contract{*c}, false)
	return &out[0], nil
}

// Request : l'agent demande un article — un contrat `requested` que
// l'exploitation qualifie.
func (s *Service) Request(ctx context.Context, userID, vertical string, req RequestInput) (*ContractResponse, error) {
	cc := country.FromContext(ctx)
	st, err := s.repo.Settings(ctx, cc)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if !st.AgentCanRequest {
		return nil, errNoRequest
	}
	if !contains(st.AllowedModes, req.Mode) {
		return nil, errModeNotAllowed
	}
	iid, _ := primitive.ObjectIDFromHex(req.ItemID)
	it, err := s.repo.ItemByID(ctx, iid)
	if err != nil {
		return nil, err
	}
	if !it.Active || (len(it.Audiences) > 0 && !contains(it.Audiences, vertical)) {
		return nil, errItemNotFound
	}
	plan := fitPlanToMode(st.Defaults, req.Mode)
	if it.DefaultPlan != nil {
		plan = fitPlanToMode(merge(*it.DefaultPlan, st.Defaults), req.Mode)
	}
	qty := req.Quantity
	if qty < 1 {
		qty = 1
	}
	unit, offered := priceOf(it, req.Mode, plan.Period)
	if !offered {
		return nil, errNotOffered
	}
	uid, _ := primitive.ObjectIDFromHex(userID)
	now := s.now()
	c := &Contract{
		Country: cc, UserID: uid, Vertical: vertical, ItemID: it.ID, ItemName: it.Name, ItemKind: it.Kind,
		Quantity: qty, Mode: req.Mode, Plan: plan, Status: StatusRequested,
		PriceXOF: unit * qty, DepositXOF: it.DepositXOF * qty, Notes: strings.TrimSpace(req.Note),
		RequestedAt: &now, CreatedBy: userID,
	}
	if err := s.repo.InsertContract(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	who := "un agent"
	if s.accounts != nil {
		if names, err := s.accounts.UserNames(ctx, []string{userID}); err == nil && names[userID] != "" {
			who = names[userID]
		}
	}
	s.alert(ctx, c, KeyStaffRequested, map[string]string{"who": who, "item": c.ItemName, "mode": modeLabel(c.Mode)})
	out := s.responses(ctx, []Contract{*c}, false)
	return &out[0], nil
}

// Qualify : l'exploitation transforme une demande en projet de contrat.
func (s *Service) Qualify(ctx context.Context, actorID, id string) (*ContractResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Status != StatusRequested {
		return nil, errBadTransition
	}
	st, err := s.repo.Settings(ctx, c.Country)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	now := s.now()
	c.Status = StatusDraft
	if !st.RequireAcceptance {
		c.Status = StatusAccepted
		c.AcceptedAt = &now
	}
	if err := s.repo.SaveContract(ctx, c); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.qualify", c, nil)
	s.notify(ctx, c.UserID.Hex(), KeyContractProposed, map[string]string{"item": c.ItemName, "mode": modeLabel(c.Mode)}, s.data(c))
	out := s.responses(ctx, []Contract{*c}, true)
	return &out[0], nil
}

// --- le recouvrement ---

// Collect est appelé quand un GAIN arrive pour une personne : ce que ses
// contrats retiennent dessus, et — pour les chauffeurs VTC, dont le solde est
// au grand livre de la verticale — ce qui est échu à porter au grand livre,
// moins ce qui leur est dû (caution rendue).
//
// Livreurs : `earning` vient d'être crédité sur le solde Dira ; la retenue y
// est prélevée ici même. Chauffeurs : rien n'est prélevé ici, la verticale
// écrit le montant rendu à son grand livre.
func (s *Service) Collect(ctx context.Context, userID, vertical string, earning int, refKind, refID string) (*CollectResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	all, err := s.repo.ContractsOfUser(ctx, uid, false)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	now := s.now()
	out := &CollectResponse{Lines: []CollectedLine{}}
	already := 0
	for i := range all {
		c := &all[i]
		if c.Vertical != vertical {
			continue
		}
		changed := false
		// Ce qui est à rendre (caution) — chauffeurs : la verticale crédite.
		if vertical == VerticalVTC {
			for j := range c.Payments {
				p := &c.Payments[j]
				if p.Pending && p.AmountXOF < 0 {
					p.Pending = false
					out.AmountXOF += p.AmountXOF
					out.Lines = append(out.Lines, CollectedLine{ContractID: c.ID.Hex(), ItemName: c.ItemName, AmountXOF: p.AmountXOF, Source: SourceRefund})
					changed = true
				}
			}
		}
		if c.Status == StatusActive {
			c.refreshLines(now)
			take := c.EarningsDeduction(earning, already, now)
			// Chauffeurs : l'échu qui aurait été prélevé sur un solde est porté
			// au grand livre, en plus de la retenue.
			if vertical == VerticalVTC && c.Plan.CollectFromWallet {
				if due := c.Outstanding(now, true) - take; due > 0 {
					take += due
				}
			}
			if take > 0 {
				source := SourceEarnings
				if vertical == VerticalFood && s.purse != nil {
					key := fmt.Sprintf("equipment:%s:%s:%s", c.ID.Hex(), refKind, refID)
					taken, err := s.purse.ChargeEquipment(ctx, userID, take, true, c.ID.Hex(), key)
					if err != nil {
						slog.WarnContext(ctx, "equipment: earnings deduction failed", "contract_id", c.ID.Hex(), "error", err)
						taken = 0
					}
					take = taken
				} else if vertical == VerticalVTC {
					source = SourceLedger
				}
				if take > 0 {
					already += take
					s.addPayment(c, take, source, refKind, refID, "", "", now)
					out.AmountXOF += take
					out.Lines = append(out.Lines, CollectedLine{ContractID: c.ID.Hex(), ItemName: c.ItemName, AmountXOF: take, Source: source})
					changed = true
					s.notify(ctx, userID, KeyCharged, map[string]string{"item": c.ItemName, "amount": money(take), "source": "vos gains"}, s.data(c))
				}
			}
		}
		if changed {
			if err := s.repo.SaveContract(ctx, c); err != nil {
				slog.ErrorContext(ctx, "equipment: contract not saved after collection", "contract_id", c.ID.Hex(), "error", err)
			}
		}
	}
	out.Standing = standingOf(all, now)
	return out, nil
}

// Standing : où en est une personne, tous contrats confondus.
func (s *Service) Standing(ctx context.Context, userID string) (*StandingResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	all, err := s.repo.ContractsOfUser(ctx, uid, true)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	st := standingOf(all, s.now())
	return &st, nil
}

func standingOf(all []Contract, now time.Time) StandingResponse {
	st := StandingResponse{}
	for i := range all {
		c := &all[i]
		if c.Status != StatusActive && c.Status != StatusReturned {
			continue
		}
		st.Contracts++
		st.OutstandingXOF += c.Outstanding(now, false)
		st.DueXOF += c.Outstanding(now, true)
		if !c.OverdueSince(now).IsZero() {
			for _, l := range c.Schedule {
				if l.owed() > 0 && !l.DueAt.AddDate(0, 0, c.Plan.GraceDays).After(now) {
					st.OverdueXOF += l.owed()
				}
			}
		}
		if c.Blocked(now) {
			st.Blocked = true
		}
	}
	return st
}

// RunDue est le BALAYAGE des échéances — toutes les quelques minutes :
// vieillir les lignes (échue, en retard + pénalité), ouvrir la période de
// loyer suivante, rappeler avant l'échéance, prélever sur le solde ce qui est
// échu, prévenir l'agent et l'équipe d'un retard ou d'un blocage.
func (s *Service) RunDue(ctx context.Context) {
	all, err := s.repo.ActiveContracts(ctx)
	if err != nil {
		slog.WarnContext(ctx, "equipment: sweep could not list contracts", "error", err)
		return
	}
	now := s.now()
	for i := range all {
		c := &all[i]
		changed := false
		// La période de loyer suivante.
		for c.Mode == ModeRental && c.Plan.Schedule == SchedulePerPeriod && c.NextPeriodAt != nil && !c.NextPeriodAt.After(now) {
			c.Schedule = append(c.Schedule, nextPeriodLine(c, *c.NextPeriodAt))
			next := periodAfter(*c.NextPeriodAt, c.Plan.Period)
			c.NextPeriodAt = &next
			changed = true
		}
		// Le rappel.
		if c.Plan.ReminderDays > 0 {
			for j := range c.Schedule {
				l := &c.Schedule[j]
				if l.owed() > 0 && l.RemindedAt == nil && l.DueAt.After(now) && !l.DueAt.AddDate(0, 0, -c.Plan.ReminderDays).After(now) {
					at := now
					l.RemindedAt = &at
					changed = true
					s.notify(ctx, c.UserID.Hex(), KeyDue, map[string]string{"item": c.ItemName, "amount": money(l.owed()), "date": l.DueAt.Format("02/01")}, s.data(c))
				}
			}
		}
		// Le retard.
		if overdue := c.refreshLines(now); len(overdue) > 0 {
			changed = true
			owed := 0
			for _, j := range overdue {
				owed += c.Schedule[j].owed()
			}
			s.notify(ctx, c.UserID.Hex(), KeyOverdue, map[string]string{"item": c.ItemName, "amount": money(owed)}, s.data(c))
			if st, err := s.repo.Settings(ctx, c.Country); err == nil && st.StaffAlertOverdue {
				who := c.UserID.Hex()
				if s.accounts != nil {
					if names, err := s.accounts.UserNames(ctx, []string{who}); err == nil && names[who] != "" {
						who = names[who]
					}
				}
				s.alert(ctx, c, KeyStaffOverdue, map[string]string{"who": who, "item": c.ItemName, "amount": money(owed)})
			}
		}
		if changed {
			if err := s.repo.SaveContract(ctx, c); err != nil {
				slog.ErrorContext(ctx, "equipment: contract not saved by sweep", "contract_id", c.ID.Hex(), "error", err)
				continue
			}
		}
		// Le prélèvement sur le solde (livreurs).
		s.chargeWallet(ctx, c, now)
		// Le blocage, dit UNE fois — et levé sans bruit.
		blocked := c.Blocked(now)
		if blocked && c.BlockedAt == nil {
			at := now
			c.BlockedAt = &at
			if err := s.repo.SaveContract(ctx, c); err == nil {
				s.notify(ctx, c.UserID.Hex(), KeyBlocked, map[string]string{"item": c.ItemName, "amount": money(c.Outstanding(now, true))}, s.data(c))
			}
		} else if !blocked && c.BlockedAt != nil {
			c.BlockedAt = nil
			_ = s.repo.SaveContract(ctx, c)
		}
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func modeLabel(mode string) string {
	switch mode {
	case ModeSale:
		return "achat"
	case ModeRental:
		return "location"
	default:
		return "prêt"
	}
}

// roleVertical dit de quelle verticale un appelant relève, d'après ce qu'il
// déclare — un chauffeur est `driver` dans les deux métiers.
func roleVertical(role, claimed string) (string, error) {
	if role != auth.RoleDriver {
		return "", apperr.Forbidden("forbidden", "equipment is for drivers and couriers")
	}
	if claimed != VerticalFood && claimed != VerticalVTC {
		return "", apperr.Validation("vertical must be food or vtc").WithMeta(map[string]any{"fields": []string{"vertical"}})
	}
	return claimed, nil
}
