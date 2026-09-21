package finance

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

var errFindingNotFound = apperr.NotFound("finding_not_found", "finding not found")

// StaffAlerter prévient le staff d'un pays (portée `core`).
type StaffAlerter interface {
	AlertStaff(ctx context.Context, scope, country, key string, vars, data map[string]string)
}

// Clé de notification du staff : un constat NOUVEAU du balayage.
const KeyStaffFinanceAlert = "staff_finance_alert"

type Service struct {
	repo  *Repository
	audit *audit.Recorder
	staff StaffAlerter
	now   func() time.Time
	// journalSince : depuis quand chaque mouvement doit avoir son écriture —
	// la mise en service du journal. Avant, les mouvements n'en ont pas, et
	// ce n'est pas un écart.
	journalSince time.Time
	runMu        sync.Mutex
	lastRun      *Run
}

func NewService(repo *Repository, auditRec *audit.Recorder, journalSince time.Time) *Service {
	return &Service{repo: repo, audit: auditRec, now: func() time.Time { return time.Now().UTC() }, journalSince: journalSince}
}

func (s *Service) SetStaffAlerter(a StaffAlerter) { s.staff = a }

// --- la facturation ---

func (s *Service) Billing(ctx context.Context, cc string) (*Billing, error) {
	if cc == "" {
		cc = country.FromContext(ctx)
	}
	b, err := s.repo.Billing(ctx, cc)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	fill(&b.Rides, DefaultBilling(cc).Rides)
	fill(&b.Deliveries, DefaultBilling(cc).Deliveries)
	return b, nil
}

// fill pose les défauts sur ce qu'un document ancien n'aurait pas.
func fill(v *VerticalBilling, def VerticalBilling) {
	if v.AgentCharge == "" {
		v.AgentCharge = def.AgentCharge
	}
	if v.ClientChargeAt == "" {
		v.ClientChargeAt = def.ClientChargeAt
	}
}

// BillingInput : ce que la console règle.
type BillingInput struct {
	Rides      *VerticalBilling `json:"rides"`
	Deliveries *VerticalBilling `json:"deliveries"`
}

func (s *Service) UpdateBilling(ctx context.Context, actorID string, in BillingInput) (*Billing, error) {
	cc := country.FromContext(ctx)
	cur, err := s.Billing(ctx, cc)
	if err != nil {
		return nil, err
	}
	before := *cur
	if in.Rides != nil {
		if err := validateVertical(*in.Rides, "rides"); err != nil {
			return nil, err
		}
		cur.Rides = *in.Rides
	}
	if in.Deliveries != nil {
		if err := validateVertical(*in.Deliveries, "deliveries"); err != nil {
			return nil, err
		}
		cur.Deliveries = *in.Deliveries
	}
	cur.UpdatedBy, cur.UpdatedAt = actorID, s.now()
	if err := s.repo.SaveBilling(ctx, cur); err != nil {
		return nil, apperr.Internal(err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, "finance.billing.update", "billing", cc, before, cur)
	}
	return cur, nil
}

func validateVertical(v VerticalBilling, field string) error {
	bad := func(reason string) error {
		return apperr.Validation("invalid billing").WithMeta(map[string]any{"fields": []string{field}, "reason": reason})
	}
	if v.AgentCharge != ChargeTokens && v.AgentCharge != ChargeCommission {
		return bad("agent_charge must be tokens or commission")
	}
	if v.CommissionPct < 0 || v.CommissionPct > 100 {
		return bad("commission_pct must be between 0 and 100")
	}
	switch v.ClientChargeAt {
	case ClientAtRequest, ClientAtAccept, ClientAtStart, ClientAtComplete:
	default:
		return bad("client_charge_at must be request, accept, start or complete")
	}
	if v.TokensPerRide < 0 || v.TokensPerKm < 0 || v.TokensMin < 0 || v.TokensMax < 0 || v.DebtLimitXOF < 0 {
		return bad("negative amounts")
	}
	if v.TokensMax > 0 && v.TokensMin > v.TokensMax {
		return bad("tokens_min above tokens_max")
	}
	return nil
}

// --- le journal ---

func (s *Service) Entries(ctx context.Context, f EntryFilter, limit int, cursor string) ([]Entry, string, error) {
	if f.Country == "" {
		f.Country = country.FromContext(ctx)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, next, err := s.repo.ListEntries(ctx, f, limit, cursor)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	return items, next, nil
}

// PostEvent : une verticale dit ce qui a bougé chez elle.
func (s *Service) PostEvent(ctx context.Context, ev Event) (int, error) {
	entries := EntriesForEvent(ev, s.now())
	if len(entries) == 0 {
		return 0, nil
	}
	if err := s.repo.InsertEntries(ctx, entries); err != nil {
		return 0, apperr.Internal(err)
	}
	return len(entries), nil
}

// Overview : l'aperçu comptable d'une période — la balance des comptes, les
// indicateurs, le détail par motif et par jour, et l'état des portefeuilles.
type Overview struct {
	Country  string           `json:"country"`
	From     time.Time        `json:"from"`
	To       time.Time        `json:"to"`
	Accounts []AccountBalance `json:"accounts"`
	KPIs     KPIs             `json:"kpis"`
	Reasons  []ReasonTotal    `json:"reasons"`
	Days     []DayTotal       `json:"days"`
	Wallets  WalletTotals     `json:"wallets"`
	Findings int              `json:"open_findings"`
	LastRun  *Run             `json:"last_run,omitempty"`
	// Balanced : la période est équilibrée (Σ débits = Σ crédits).
	Balanced bool `json:"balanced"`
}

type AccountBalance struct {
	Account
	Debit   int `json:"debit"`
	Credit  int `json:"credit"`
	Balance int `json:"balance"`
	Entries int `json:"entries"`
}

type KPIs struct {
	Revenue           int `json:"revenue"`
	RevenueCommission int `json:"revenue_commission"`
	RevenueTokens     int `json:"revenue_tokens"`
	RevenueEquipment  int `json:"revenue_equipment"`
	Expenses          int `json:"expenses"`
	Net               int `json:"net"`
	Liabilities       int `json:"liabilities"`
	Receivables       int `json:"receivables"`
	Clearing          int `json:"clearing"`
	Suspense          int `json:"suspense"`
	Collected         int `json:"collected"`
}

// WalletTotals : les soldes STOCKÉS, tous portefeuilles du pays confondus —
// ce que la plateforme doit à l'instant, indépendamment de la période.
type WalletTotals struct {
	Clients       int `json:"clients"`
	ClientsXOF    int `json:"clients_xof"`
	PromoXOF      int `json:"promo_xof"`
	Agents        int `json:"agents"`
	AgentsXOF     int `json:"agents_xof"`
	AgentTokens   int `json:"agent_tokens"`
	Merchants     int `json:"merchants"`
	MerchantsXOF  int `json:"merchants_xof"`
	MerchantToken int `json:"merchant_tokens"`
}

func (s *Service) Overview(ctx context.Context, from, to time.Time) (*Overview, error) {
	cc := country.FromContext(ctx)
	if to.IsZero() {
		to = s.now()
	}
	if from.IsZero() {
		from = to.AddDate(0, -1, 0)
	}
	f := EntryFilter{Country: cc, From: from, To: to}
	totals, err := s.repo.Totals(ctx, f)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	byAcc := map[string]AccountTotal{}
	for _, t := range totals {
		byAcc[t.Account] = t
	}
	ov := &Overview{Country: cc, From: from, To: to}
	debits, credits := 0, 0
	chart := append([]Account{}, Chart...)
	chart = append(chart, Account{AccSuspense, KindClearing, "À classer"})
	for _, a := range chart {
		t := byAcc[a.Code]
		bal := t.Credit - t.Debit
		if a.Kind == KindAsset || a.Kind == KindExpense {
			bal = t.Debit - t.Credit
		}
		ov.Accounts = append(ov.Accounts, AccountBalance{Account: a, Debit: t.Debit, Credit: t.Credit, Balance: bal, Entries: t.Entries})
		debits += t.Debit
		credits += t.Credit
		switch a.Kind {
		case KindRevenue:
			ov.KPIs.Revenue += bal
		case KindExpense:
			ov.KPIs.Expenses += bal
		case KindLiability:
			ov.KPIs.Liabilities += bal
		case KindAsset:
			if a.Code == AccAgentReceivable {
				ov.KPIs.Receivables += bal
			}
			if a.Code == AccMobileMoney {
				ov.KPIs.Collected += t.Debit
			}
		case KindClearing:
			if a.Code == AccSuspense {
				ov.KPIs.Suspense += t.Debit + t.Credit
			} else {
				ov.KPIs.Clearing += bal
			}
		}
	}
	ov.KPIs.RevenueCommission = balanceOf(byAcc, AccRevenueCommission)
	ov.KPIs.RevenueTokens = balanceOf(byAcc, AccRevenueTokens)
	ov.KPIs.RevenueEquipment = balanceOf(byAcc, AccRevenueEquipment)
	ov.KPIs.Net = ov.KPIs.Revenue - ov.KPIs.Expenses
	ov.Balanced = debits == credits
	if ov.Reasons, err = s.repo.ByReason(ctx, f); err != nil {
		return nil, apperr.Internal(err)
	}
	if ov.Days, err = s.repo.ByDay(ctx, f); err != nil {
		return nil, apperr.Internal(err)
	}
	if ov.Reasons == nil {
		ov.Reasons = []ReasonTotal{}
	}
	if ov.Days == nil {
		ov.Days = []DayTotal{}
	}
	// L'état des portefeuilles, à l'instant.
	_ = s.repo.Wallets(ctx, func(w WalletSnapshot) error {
		if cc != "" && w.Country != "" && w.Country != cc {
			return nil
		}
		switch w.Type {
		case "client":
			ov.Wallets.Clients++
			ov.Wallets.ClientsXOF += w.BalanceXOF
			ov.Wallets.PromoXOF += w.PromoXOF
		case "merchant":
			ov.Wallets.Merchants++
			ov.Wallets.MerchantsXOF += w.BalanceXOF
			ov.Wallets.MerchantToken += w.Balance
		default:
			ov.Wallets.Agents++
			ov.Wallets.AgentsXOF += w.BalanceXOF
			ov.Wallets.AgentTokens += w.Balance
		}
		return nil
	})
	if open, err := s.repo.ListFindings(ctx, "", cc, 500); err == nil {
		ov.Findings = len(open)
	}
	s.runMu.Lock()
	ov.LastRun = s.lastRun
	s.runMu.Unlock()
	return ov, nil
}

func balanceOf(byAcc map[string]AccountTotal, code string) int {
	t := byAcc[code]
	return t.Credit - t.Debit
}

// --- les constats ---

func (s *Service) Findings(ctx context.Context, status string) ([]Finding, error) {
	items, err := s.repo.ListFindings(ctx, status, country.FromContext(ctx), 200)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return items, nil
}

func (s *Service) Acknowledge(ctx context.Context, actorID, id, note string) (*Finding, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errFindingNotFound
	}
	f, err := s.repo.AckFinding(ctx, oid, actorID, strings.TrimSpace(note), s.now())
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		s.audit.Record(ctx, "finance.finding.ack", "finding", id, nil, map[string]any{"note": note, "kind": f.Kind})
	}
	return f, nil
}

// LastRun : le dernier balayage de ce processus.
func (s *Service) LastRun() *Run {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.lastRun
}

// RunIntegrity : LE BALAYAGE. Recalcule chaque solde depuis ses mouvements,
// vérifie le journal, et dit ce qui ne colle pas. Un seul à la fois.
func (s *Service) RunIntegrity(ctx context.Context) (*Run, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	now := s.now()
	run := &Run{StartedAt: now}
	seen := map[string]bool{}
	var found []Finding

	sums, err := s.repo.SumTransactions(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	// Un mouvement doit avoir son écriture à partir de la mise en service du
	// journal SUR CETTE BASE : la première écriture, ou la date réglée si
	// elle est postérieure. Sans aucune écriture, rien n'est exigible.
	since := s.journalSince
	if first, ok := s.repo.FirstEntryAt(ctx); ok {
		if first.After(since) {
			since = first
		}
	} else {
		since = now
	}
	missing, err := s.repo.MissingEntries(ctx, since)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	err = s.repo.Wallets(ctx, func(w WalletSnapshot) error {
		run.Wallets++
		sum := sums[w.ID]
		check := func(unit string, expected, actual int) {
			if expected == actual {
				return
			}
			found = append(found, Finding{
				Fingerprint: "drift:" + w.ID.Hex() + ":" + unit, Kind: FindingWalletDrift, Severity: "critical",
				Country: w.Country, WalletID: w.ID.Hex(), OwnerID: w.OwnerID.Hex(), OwnerType: w.Type, Unit: unit,
				Expected: expected, Actual: actual,
				Detail: fmt.Sprintf("le solde stocké (%d) ne vaut pas la somme des %d mouvements (%d) — %s", actual, sum.Count, expected, unit),
			})
		}
		check("token", sum.Tokens, w.Balance)
		check("xof", sum.XOF, w.BalanceXOF)
		check("promo", sum.Promo, w.PromoXOF)
		check("debt", sum.Debt, w.DebtXOF)
		for unit, v := range map[string]int{"token": w.Balance, "xof": w.BalanceXOF, "promo": w.PromoXOF, "debt": w.DebtXOF} {
			if v < 0 {
				found = append(found, Finding{
					Fingerprint: "negative:" + w.ID.Hex() + ":" + unit, Kind: FindingNegative, Severity: "critical",
					Country: w.Country, WalletID: w.ID.Hex(), OwnerID: w.OwnerID.Hex(), OwnerType: w.Type, Unit: unit,
					Expected: 0, Actual: v, Detail: "un solde négatif là où c'est interdit",
				})
			}
		}
		if n := missing[w.ID]; n > 0 {
			found = append(found, Finding{
				Fingerprint: "missing:" + w.ID.Hex(), Kind: FindingMissingEntry, Severity: "warning",
				Country: w.Country, WalletID: w.ID.Hex(), OwnerID: w.OwnerID.Hex(), OwnerType: w.Type,
				Expected: 0, Actual: n, Detail: fmt.Sprintf("%d mouvement(s) sans écriture au journal", n),
			})
		}
		return nil
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	unbalanced, total, err := s.repo.UnbalancedEntries(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	run.Entries = total
	for _, id := range unbalanced {
		found = append(found, Finding{
			Fingerprint: "unbalanced:" + id.Hex(), Kind: FindingEntryUnbalance, Severity: "critical",
			Detail: "écriture " + id.Hex() + " : débits ≠ crédits",
		})
	}
	// La balance générale, toutes périodes : Σ débits = Σ crédits.
	if totals, err := s.repo.Totals(ctx, EntryFilter{}); err == nil {
		d, c := 0, 0
		for _, t := range totals {
			d += t.Debit
			c += t.Credit
		}
		if d != c {
			found = append(found, Finding{
				Fingerprint: "trial", Kind: FindingTrialUnbalance, Severity: "critical",
				Expected: d, Actual: c, Detail: fmt.Sprintf("balance générale : débits %d, crédits %d", d, c),
			})
		}
	}

	for i := range found {
		seen[found[i].Fingerprint] = true
		isNew, err := s.repo.UpsertFinding(ctx, &found[i], now)
		if err != nil {
			slog.ErrorContext(ctx, "finance: finding not saved", "fingerprint", found[i].Fingerprint, "error", err)
			continue
		}
		run.Findings++
		if isNew {
			run.New++
			s.alert(ctx, found[i])
		}
	}
	if run.Resolved, err = s.repo.ResolveUnseen(ctx, seen, now); err != nil {
		slog.ErrorContext(ctx, "finance: findings not resolved", "error", err)
	}
	run.FinishedAt = s.now()
	s.lastRun = run
	slog.InfoContext(ctx, "finance: integrity sweep done", "wallets", run.Wallets, "entries", run.Entries,
		"findings", run.Findings, "new", run.New, "resolved", run.Resolved, "took", run.FinishedAt.Sub(run.StartedAt).String())
	return run, nil
}

func (s *Service) alert(ctx context.Context, f Finding) {
	if s.staff == nil {
		return
	}
	who := f.OwnerType
	if who == "" {
		who = "journal"
	}
	s.staff.AlertStaff(ctx, "core", f.Country, KeyStaffFinanceAlert, map[string]string{
		"kind": f.Kind, "who": who, "detail": f.Detail,
	}, map[string]string{"type": "finance_finding", "finding_id": f.ID.Hex(), "kind": f.Kind, "wallet_id": f.WalletID})
}

// RunEvery : le balayage à cadence fixe.
func (s *Service) RunEvery(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			if _, err := s.RunIntegrity(rctx); err != nil {
				slog.ErrorContext(ctx, "finance: integrity sweep failed", "error", err)
			}
			cancel()
		}
	}
}
