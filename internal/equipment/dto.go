package equipment

import "time"

// --- items ---

type ItemInput struct {
	Kind             string   `json:"kind" validate:"required,oneof=vest bag phone helmet box other"`
	Name             string   `json:"name" validate:"required,min=1,max=120"`
	Description      string   `json:"description" validate:"omitempty,max=2000"`
	Photos           []string `json:"photos" validate:"omitempty,max=8,dive,url"`
	Audiences        []string `json:"audiences" validate:"omitempty,dive,oneof=food vtc"`
	SalePriceXOF     int      `json:"sale_price_xof" validate:"min=0"`
	RentalDailyXOF   int      `json:"rental_daily_xof" validate:"min=0"`
	RentalWeeklyXOF  int      `json:"rental_weekly_xof" validate:"min=0"`
	RentalMonthlyXOF int      `json:"rental_monthly_xof" validate:"min=0"`
	DepositXOF       int      `json:"deposit_xof" validate:"min=0"`
	Stock            int      `json:"stock" validate:"min=0"`
	TrackStock       bool     `json:"track_stock"`
	Active           *bool    `json:"active"`
	DefaultPlan      *Plan    `json:"default_plan"`
}

type ItemResponse struct {
	ID               string    `json:"id"`
	Country          string    `json:"country"`
	Kind             string    `json:"kind"`
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	Photos           []string  `json:"photos"`
	Audiences        []string  `json:"audiences"`
	SalePriceXOF     int       `json:"sale_price_xof"`
	RentalDailyXOF   int       `json:"rental_daily_xof"`
	RentalWeeklyXOF  int       `json:"rental_weekly_xof"`
	RentalMonthlyXOF int       `json:"rental_monthly_xof"`
	DepositXOF       int       `json:"deposit_xof"`
	Stock            int       `json:"stock"`
	TrackStock       bool      `json:"track_stock"`
	Active           bool      `json:"active"`
	DefaultPlan      *Plan     `json:"default_plan,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func toItem(it *Item) ItemResponse {
	photos, aud := it.Photos, it.Audiences
	if photos == nil {
		photos = []string{}
	}
	if aud == nil {
		aud = []string{}
	}
	return ItemResponse{
		ID: it.ID.Hex(), Country: it.Country, Kind: it.Kind, Name: it.Name, Description: it.Description,
		Photos: photos, Audiences: aud, SalePriceXOF: it.SalePriceXOF,
		RentalDailyXOF: it.RentalDailyXOF, RentalWeeklyXOF: it.RentalWeeklyXOF, RentalMonthlyXOF: it.RentalMonthlyXOF,
		DepositXOF: it.DepositXOF, Stock: it.Stock, TrackStock: it.TrackStock, Active: it.Active,
		DefaultPlan: it.DefaultPlan, CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
}

// --- settings ---

type SettingsInput struct {
	AllowedModes          []string `json:"allowed_modes" validate:"omitempty,dive,oneof=sale rental loan"`
	AgentCanRequest       *bool    `json:"agent_can_request"`
	RequireAcceptance     *bool    `json:"require_acceptance"`
	MaxEarningsPercent    *int     `json:"max_earnings_percent" validate:"omitempty,min=0,max=100"`
	DefaultDepositPercent *int     `json:"default_deposit_percent" validate:"omitempty,min=0,max=100"`
	StaffAlertOverdue     *bool    `json:"staff_alert_overdue"`
	Defaults              *Plan    `json:"defaults"`
}

type SettingsResponse struct {
	Country               string    `json:"country"`
	AllowedModes          []string  `json:"allowed_modes"`
	AgentCanRequest       bool      `json:"agent_can_request"`
	RequireAcceptance     bool      `json:"require_acceptance"`
	MaxEarningsPercent    int       `json:"max_earnings_percent"`
	DefaultDepositPercent int       `json:"default_deposit_percent"`
	StaffAlertOverdue     bool      `json:"staff_alert_overdue"`
	Defaults              Plan      `json:"defaults"`
	UpdatedAt             time.Time `json:"updated_at"`
	// Ce que le code connaît — pour que la console propose sans deviner.
	Kinds     []string `json:"kinds"`
	Modes     []string `json:"modes"`
	Periods   []string `json:"periods"`
	Schedules []string `json:"schedules"`
}

func toSettings(s *Settings) SettingsResponse {
	return SettingsResponse{
		Country: s.Country, AllowedModes: s.AllowedModes, AgentCanRequest: s.AgentCanRequest,
		RequireAcceptance: s.RequireAcceptance, MaxEarningsPercent: s.MaxEarningsPercent,
		DefaultDepositPercent: s.DefaultDepositPercent, StaffAlertOverdue: s.StaffAlertOverdue,
		Defaults: s.Defaults, UpdatedAt: s.UpdatedAt,
		Kinds: Kinds, Modes: Modes, Periods: Periods,
		Schedules: []string{ScheduleUpfront, ScheduleInstallments, SchedulePerPeriod, ScheduleNone},
	}
}

// --- contracts ---

// ContractInput : ce que l'exploitation fixe. Tout ce qui est absent vient
// de l'article, puis des réglages du pays.
type ContractInput struct {
	UserID   string `json:"user_id" validate:"required,len=24,hexadecimal"`
	Vertical string `json:"vertical" validate:"required,oneof=food vtc"`
	ItemID   string `json:"item_id" validate:"required,len=24,hexadecimal"`
	Quantity int    `json:"quantity" validate:"omitempty,min=1,max=50"`
	Serial   string `json:"serial" validate:"omitempty,max=80"`
	Mode     string `json:"mode" validate:"required,oneof=sale rental loan"`
	// PriceXOF : prix de vente total ou loyer par période ; absent = celui de
	// l'article × quantité. DepositXOF : absent = celui de l'article × quantité.
	PriceXOF   *int   `json:"price_xof" validate:"omitempty,min=0"`
	DepositXOF *int   `json:"deposit_xof" validate:"omitempty,min=0"`
	Plan       *Plan  `json:"plan"`
	Notes      string `json:"notes" validate:"omitempty,max=2000"`
	// HandOverNow : remis à la création — l'échéancier démarre tout de suite.
	HandOverNow bool `json:"hand_over_now"`
}

type ContractPatch struct {
	Serial *string `json:"serial" validate:"omitempty,max=80"`
	Notes  *string `json:"notes" validate:"omitempty,max=2000"`
	Plan   *Plan   `json:"plan"`
	// Avant la remise seulement :
	PriceXOF   *int `json:"price_xof" validate:"omitempty,min=0"`
	DepositXOF *int `json:"deposit_xof" validate:"omitempty,min=0"`
}

type PaymentInput struct {
	AmountXOF int    `json:"amount_xof" validate:"required,min=1"`
	Source    string `json:"source" validate:"required,oneof=manual mobile_money waiver"`
	Note      string `json:"note" validate:"omitempty,max=500"`
}

type ReturnInput struct {
	Condition    string `json:"condition" validate:"omitempty,max=500"`
	DamageFeeXOF int    `json:"damage_fee_xof" validate:"min=0"`
	// RefundDeposit : rendre la caution (moins les dégâts) — défaut : le plan.
	RefundDeposit *bool `json:"refund_deposit"`
}

type RequestInput struct {
	ItemID   string `json:"item_id" validate:"required,len=24,hexadecimal"`
	Mode     string `json:"mode" validate:"required,oneof=sale rental loan"`
	Quantity int    `json:"quantity" validate:"omitempty,min=1,max=10"`
	Note     string `json:"note" validate:"omitempty,max=1000"`
}

type PayInput struct {
	AmountXOF int `json:"amount_xof" validate:"required,min=1"`
}

type LineResponse struct {
	N          int        `json:"n"`
	Kind       string     `json:"kind"`
	DueAt      time.Time  `json:"due_at"`
	AmountXOF  int        `json:"amount_xof"`
	LateFeeXOF int        `json:"late_fee_xof"`
	PaidXOF    int        `json:"paid_xof"`
	OwedXOF    int        `json:"owed_xof"`
	PaidAt     *time.Time `json:"paid_at,omitempty"`
	Status     string     `json:"status"`
}

type PaymentResponse struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	AmountXOF int       `json:"amount_xof"`
	Source    string    `json:"source"`
	RefKind   string    `json:"ref_kind,omitempty"`
	RefID     string    `json:"ref_id,omitempty"`
	Note      string    `json:"note,omitempty"`
	Pending   bool      `json:"pending,omitempty"`
}

type ContractResponse struct {
	ID       string `json:"id"`
	Country  string `json:"country"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name,omitempty"`
	Vertical string `json:"vertical"`
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	ItemKind string `json:"item_kind"`
	Quantity int    `json:"quantity"`
	Serial   string `json:"serial,omitempty"`
	Mode     string `json:"mode"`
	Plan     Plan   `json:"plan"`
	Status   string `json:"status"`

	PriceXOF   int `json:"price_xof"`
	DepositXOF int `json:"deposit_xof"`
	// Les TOTAUX, calculés : ce qui a été payé, ce qui reste, ce qui est
	// échu, depuis quand c'est en retard, et si l'agent est bloqué.
	PaidXOF        int        `json:"paid_xof"`
	OutstandingXOF int        `json:"outstanding_xof"`
	DueXOF         int        `json:"due_xof"`
	OverdueSince   *time.Time `json:"overdue_since,omitempty"`
	Blocked        bool       `json:"blocked"`

	Schedule []LineResponse    `json:"schedule"`
	Payments []PaymentResponse `json:"payments"`

	ReturnCondition string     `json:"return_condition,omitempty"`
	DamageFeeXOF    int        `json:"damage_fee_xof,omitempty"`
	NextPeriodAt    *time.Time `json:"next_period_at,omitempty"`
	Notes           string     `json:"notes,omitempty"`
	RequestedAt     *time.Time `json:"requested_at,omitempty"`
	AcceptedAt      *time.Time `json:"accepted_at,omitempty"`
	HandedAt        *time.Time `json:"handed_at,omitempty"`
	ReturnedAt      *time.Time `json:"returned_at,omitempty"`
	ClosedAt        *time.Time `json:"closed_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toContract(c *Contract, now time.Time) ContractResponse {
	resp := ContractResponse{
		ID: c.ID.Hex(), Country: c.Country, UserID: c.UserID.Hex(), Vertical: c.Vertical,
		ItemID: c.ItemID.Hex(), ItemName: c.ItemName, ItemKind: c.ItemKind, Quantity: c.Quantity, Serial: c.Serial,
		Mode: c.Mode, Plan: c.Plan, Status: c.Status, PriceXOF: c.PriceXOF, DepositXOF: c.DepositXOF,
		OutstandingXOF: c.Outstanding(now, false), DueXOF: c.Outstanding(now, true), Blocked: c.Blocked(now),
		Schedule: make([]LineResponse, 0, len(c.Schedule)), Payments: make([]PaymentResponse, 0, len(c.Payments)),
		ReturnCondition: c.ReturnCondition, DamageFeeXOF: c.DamageFeeXOF, NextPeriodAt: c.NextPeriodAt, Notes: c.Notes,
		RequestedAt: c.RequestedAt, AcceptedAt: c.AcceptedAt, HandedAt: c.HandedAt, ReturnedAt: c.ReturnedAt, ClosedAt: c.ClosedAt,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if since := c.OverdueSince(now); !since.IsZero() {
		resp.OverdueSince = &since
	}
	for _, l := range c.Schedule {
		resp.Schedule = append(resp.Schedule, LineResponse{
			N: l.N, Kind: l.Kind, DueAt: l.DueAt, AmountXOF: l.AmountXOF, LateFeeXOF: l.LateFeeXOF,
			PaidXOF: l.PaidXOF, OwedXOF: l.owed(), PaidAt: l.PaidAt, Status: l.Status,
		})
	}
	for _, p := range c.Payments {
		if p.AmountXOF > 0 {
			resp.PaidXOF += p.AmountXOF
		}
		resp.Payments = append(resp.Payments, PaymentResponse{
			ID: p.ID.Hex(), At: p.At, AmountXOF: p.AmountXOF, Source: p.Source,
			RefKind: p.RefKind, RefID: p.RefID, Note: p.Note, Pending: p.Pending,
		})
	}
	return resp
}

// StandingResponse : où en est une personne, tous contrats confondus — ce
// qu'une verticale demande avant de la laisser se mettre en ligne.
type StandingResponse struct {
	Contracts      int  `json:"contracts"`
	OutstandingXOF int  `json:"outstanding_xof"`
	DueXOF         int  `json:"due_xof"`
	OverdueXOF     int  `json:"overdue_xof"`
	Blocked        bool `json:"blocked"`
}

// CollectResponse : ce que le socle a retenu — ou rendu (négatif) — sur un
// gain, contrat par contrat.
type CollectResponse struct {
	AmountXOF int                `json:"amount_xof"`
	Lines     []CollectedLine    `json:"lines"`
	Standing  StandingResponse   `json:"standing"`
	Contracts []ContractResponse `json:"-"`
}

type CollectedLine struct {
	ContractID string `json:"contract_id"`
	ItemName   string `json:"item_name"`
	AmountXOF  int    `json:"amount_xof"`
	Source     string `json:"source"`
}
