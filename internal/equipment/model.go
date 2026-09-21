// Package equipment is the MATÉRIEL desk: vests, delivery bags, phones,
// helmets the platform sells, rents or lends to its couriers and drivers —
// and, above all, HOW the money comes back: up front, by instalments, taken
// from each earning, charged to the balance on a due date, or paid by hand.
//
// Au socle, parce que c'est une affaire de PERSONNE et d'ARGENT — pas de
// commande ni de course. Le contrat est la vérité : son échéancier, ses
// paiements, sa caution. Chaque réglage a un défaut par pays
// (`Settings`) que le contrat peut surcharger : l'exploitation décide, pas
// le code.
package equipment

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	collectionItems     = "equipment_items"
	collectionContracts = "equipment_contracts"
	collectionSettings  = "equipment_settings"
)

// Kinds of items.
const (
	KindVest   = "vest"
	KindBag    = "bag"
	KindPhone  = "phone"
	KindHelmet = "helmet"
	KindBox    = "box"
	KindOther  = "other"
)

var Kinds = []string{KindVest, KindBag, KindPhone, KindHelmet, KindBox, KindOther}

// Audiences — who an item is offered to. Le rôle est `driver` pour les deux ;
// c'est la VERTICALE qui distingue le livreur du chauffeur.
const (
	VerticalFood = "food" // livreurs
	VerticalVTC  = "vtc"  // chauffeurs
)

// Modes of a contract.
const (
	ModeSale   = "sale"   // vendu : le prix, en une ou plusieurs fois
	ModeRental = "rental" // loué : un loyer par période, une caution, un retour
	ModeLoan   = "loan"   // prêté : une caution au plus, un retour
)

var Modes = []string{ModeSale, ModeRental, ModeLoan}

// How the amount is spread over time.
const (
	ScheduleUpfront      = "upfront"      // tout à la remise
	ScheduleInstallments = "installments" // N échéances
	SchedulePerPeriod    = "per_period"   // un loyer par période, jusqu'au retour
	ScheduleNone         = "none"         // rien à payer (prêt sans caution)
)

// Periods.
const (
	PeriodDaily    = "daily"
	PeriodWeekly   = "weekly"
	PeriodBiweekly = "biweekly"
	PeriodMonthly  = "monthly"
)

var Periods = []string{PeriodDaily, PeriodWeekly, PeriodBiweekly, PeriodMonthly}

// Statuses of a contract.
const (
	StatusRequested = "requested" // demandé par l'agent, à qualifier
	StatusDraft     = "draft"     // préparé par l'exploitation
	StatusAccepted  = "accepted"  // conditions acceptées par l'agent
	StatusActive    = "active"    // remis
	StatusReturned  = "returned"  // rendu (location, prêt)
	StatusCompleted = "completed" // soldé (vente)
	StatusDefaulted = "defaulted" // impayé, clos par l'exploitation
	StatusCancelled = "cancelled"
)

// Statuses of a schedule line.
const (
	LinePending = "pending"
	LineDue     = "due"
	LineOverdue = "overdue"
	LinePaid    = "paid"
	LineWaived  = "waived"
)

// Kinds of schedule lines.
const (
	LineDeposit     = "deposit"
	LineInstallment = "installment"
	LinePeriod      = "period"
	LineDamage      = "damage"
)

// Sources of a payment.
const (
	SourceEarnings    = "earnings" // retenu sur un gain
	SourceWallet      = "wallet"   // prélevé sur le solde Dira
	SourceLedger      = "ledger"   // porté au grand livre de la verticale (chauffeurs VTC)
	SourceMobileMoney = "mobile_money"
	SourceManual      = "manual" // encaissé par l'exploitation (espèces, virement)
	SourceWaiver      = "waiver" // remise
	SourceRefund      = "refund" // rendu à l'agent (caution) — montant NÉGATIF
)

// Plan says how the money comes back. Tout est facultatif et défaut aux
// réglages du pays (`Settings.Defaults`) ; un contrat ne porte que ce qu'il
// surcharge.
type Plan struct {
	Schedule string `bson:"schedule" json:"schedule"`
	// Installments : nombre d'échéances (schedule=installments) ; Period :
	// leur pas, et le pas du loyer (per_period). FirstDueDays : jours entre
	// la remise et la première échéance.
	Installments int    `bson:"installments,omitempty" json:"installments"`
	Period       string `bson:"period,omitempty" json:"period"`
	FirstDueDays int    `bson:"first_due_days" json:"first_due_days"`

	// Les CANAUX de recouvrement — cumulables. Le paiement par l'agent (app,
	// mobile money) et l'encaissement par l'exploitation sont toujours
	// possibles.
	CollectFromEarnings bool `bson:"collect_from_earnings" json:"collect_from_earnings"`
	CollectFromWallet   bool `bson:"collect_from_wallet" json:"collect_from_wallet"`
	// AllowPartial : sur le solde, prendre ce qu'il y a quand il ne couvre pas
	// l'échéance.
	AllowPartial bool `bson:"allow_partial" json:"allow_partial"`

	// La RETENUE SUR GAINS : un pourcentage de chaque gain et/ou un fixe,
	// bornée par ce qu'on laisse au moins à l'agent et par des plafonds.
	EarningsPercent  int `bson:"earnings_percent,omitempty" json:"earnings_percent"`
	EarningsFixedXOF int `bson:"earnings_fixed_xof,omitempty" json:"earnings_fixed_xof"`
	MinLeftXOF       int `bson:"min_left_xof,omitempty" json:"min_left_xof"`
	DailyCapXOF      int `bson:"daily_cap_xof,omitempty" json:"daily_cap_xof"`
	WeeklyCapXOF     int `bson:"weekly_cap_xof,omitempty" json:"weekly_cap_xof"`
	// EarningsOnlyWhenDue : ne retenir que ce qui est échu (sinon, la retenue
	// rembourse en avance sur l'échéancier — plus vite libéré).
	EarningsOnlyWhenDue bool `bson:"earnings_only_when_due" json:"earnings_only_when_due"`

	// Le RETARD : délai de grâce, pénalité (fixe et/ou pourcentage de
	// l'échéance, une fois), blocage de la mise en ligne au-delà de N jours
	// de retard (0 = jamais).
	GraceDays      int `bson:"grace_days" json:"grace_days"`
	LateFeeXOF     int `bson:"late_fee_xof,omitempty" json:"late_fee_xof"`
	LateFeePercent int `bson:"late_fee_percent,omitempty" json:"late_fee_percent"`
	BlockAfterDays int `bson:"block_after_days" json:"block_after_days"`
	// ReminderDays : rappel avant l'échéance (0 = aucun).
	ReminderDays int `bson:"reminder_days" json:"reminder_days"`
	// DepositRefundable : rendre la caution au retour (moins les dégâts).
	DepositRefundable bool `bson:"deposit_refundable" json:"deposit_refundable"`
}

// Item is one article of the catalogue, per country.
type Item struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	Country     string             `bson:"country"`
	Kind        string             `bson:"kind"`
	Name        string             `bson:"name"`
	Description string             `bson:"description,omitempty"`
	Photos      []string           `bson:"photos,omitempty"`
	// Audiences : `food` (livreurs), `vtc` (chauffeurs) — vide = les deux.
	Audiences []string `bson:"audiences,omitempty"`
	// Ce qu'on en fait : un prix de vente, des loyers par période (0 = non
	// proposé), une caution.
	SalePriceXOF     int `bson:"sale_price_xof,omitempty"`
	RentalDailyXOF   int `bson:"rental_daily_xof,omitempty"`
	RentalWeeklyXOF  int `bson:"rental_weekly_xof,omitempty"`
	RentalMonthlyXOF int `bson:"rental_monthly_xof,omitempty"`
	DepositXOF       int `bson:"deposit_xof,omitempty"`
	// Stock : unités disponibles ; TrackStock à faux = illimité.
	Stock      int  `bson:"stock"`
	TrackStock bool `bson:"track_stock"`
	Active     bool `bson:"active"`
	// DefaultPlan surcharge les défauts du pays pour cet article.
	DefaultPlan *Plan     `bson:"default_plan,omitempty"`
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
}

// Line is one amount to collect, when.
type Line struct {
	N          int        `bson:"n"`
	Kind       string     `bson:"kind"`
	DueAt      time.Time  `bson:"due_at"`
	AmountXOF  int        `bson:"amount_xof"`
	LateFeeXOF int        `bson:"late_fee_xof,omitempty"`
	PaidXOF    int        `bson:"paid_xof"`
	PaidAt     *time.Time `bson:"paid_at,omitempty"`
	Status     string     `bson:"status"`
	RemindedAt *time.Time `bson:"reminded_at,omitempty"`
}

// Payment is one movement on the contract, positive when the agent pays,
// negative when the platform gives back.
type Payment struct {
	ID        primitive.ObjectID `bson:"_id"`
	At        time.Time          `bson:"at"`
	AmountXOF int                `bson:"amount_xof"`
	Source    string             `bson:"source"`
	// RefKind/RefID : le gain retenu (commande, course), le paiement mobile
	// money, ou rien (encaissement manuel).
	RefKind string `bson:"ref_kind,omitempty"`
	RefID   string `bson:"ref_id,omitempty"`
	Note    string `bson:"note,omitempty"`
	ActorID string `bson:"actor_id,omitempty"`
	// Pending : porté au grand livre de la verticale, pas encore confirmé
	// (chauffeurs VTC : le socle ne tient pas leur solde).
	Pending bool `bson:"pending,omitempty"`
}

// Contract binds an agent to an item under a plan.
type Contract struct {
	ID       primitive.ObjectID `bson:"_id,omitempty"`
	Country  string             `bson:"country"`
	UserID   primitive.ObjectID `bson:"user_id"`
	Vertical string             `bson:"vertical"` // food | vtc
	ItemID   primitive.ObjectID `bson:"item_id"`
	// Figé à la création : le contrat reste lisible si l'article change.
	ItemName string `bson:"item_name"`
	ItemKind string `bson:"item_kind"`
	Quantity int    `bson:"quantity"`
	Serial   string `bson:"serial,omitempty"`

	Mode   string `bson:"mode"`
	Plan   Plan   `bson:"plan"`
	Status string `bson:"status"`

	// PriceXOF : le prix total (vente) ou le loyer par période (location),
	// pour toute la quantité. DepositXOF : la caution demandée.
	PriceXOF   int `bson:"price_xof"`
	DepositXOF int `bson:"deposit_xof"`

	Schedule []Line    `bson:"schedule"`
	Payments []Payment `bson:"payments"`

	// Le RETOUR (location, prêt).
	ReturnCondition string `bson:"return_condition,omitempty"`
	DamageFeeXOF    int    `bson:"damage_fee_xof,omitempty"`
	// NextPeriodAt : la prochaine période de loyer à générer.
	NextPeriodAt *time.Time `bson:"next_period_at,omitempty"`
	// BlockedAt : depuis quand le blocage a été constaté (et dit) ; vide
	// quand l'agent n'est pas bloqué. C'est ce qui évite de le redire à
	// chaque balayage.
	BlockedAt *time.Time `bson:"blocked_at,omitempty"`

	Notes       string     `bson:"notes,omitempty"`
	CreatedBy   string     `bson:"created_by,omitempty"`
	RequestedAt *time.Time `bson:"requested_at,omitempty"`
	AcceptedAt  *time.Time `bson:"accepted_at,omitempty"`
	HandedAt    *time.Time `bson:"handed_at,omitempty"`
	ReturnedAt  *time.Time `bson:"returned_at,omitempty"`
	ClosedAt    *time.Time `bson:"closed_at,omitempty"`
	CreatedAt   time.Time  `bson:"created_at"`
	UpdatedAt   time.Time  `bson:"updated_at"`
}

// Settings per country: the defaults every contract starts from, and what
// the exploitation allows.
type Settings struct {
	Country string `bson:"_id"`
	// AllowedModes : ce qu'on propose dans ce pays.
	AllowedModes []string `bson:"allowed_modes"`
	// AgentCanRequest : l'agent demande un article depuis l'application.
	AgentCanRequest bool `bson:"agent_can_request"`
	// RequireAcceptance : l'agent doit accepter les conditions dans l'app
	// avant la remise.
	RequireAcceptance bool `bson:"require_acceptance"`
	// MaxEarningsPercent : plafond du pourcentage retenu sur un gain, tous
	// contrats confondus.
	MaxEarningsPercent int `bson:"max_earnings_percent"`
	// DefaultDepositPercent : caution par défaut = % du prix de vente quand
	// l'article n'en fixe pas.
	DefaultDepositPercent int  `bson:"default_deposit_percent"`
	Defaults              Plan `bson:"defaults"`
	// StaffAlertOverdue : prévenir l'équipe d'un retard.
	StaffAlertOverdue bool      `bson:"staff_alert_overdue"`
	UpdatedAt         time.Time `bson:"updated_at"`
}

// DefaultSettings est le plancher : ce qui vaut tant qu'un pays n'a rien
// réglé. Prudent — pas de blocage, pas de pénalité, une retenue modérée.
func DefaultSettings(country string) Settings {
	return Settings{
		Country:               country,
		AllowedModes:          []string{ModeSale, ModeRental, ModeLoan},
		AgentCanRequest:       true,
		RequireAcceptance:     true,
		MaxEarningsPercent:    50,
		DefaultDepositPercent: 0,
		StaffAlertOverdue:     true,
		Defaults: Plan{
			Schedule:            ScheduleInstallments,
			Installments:        4,
			Period:              PeriodWeekly,
			FirstDueDays:        7,
			CollectFromEarnings: true,
			CollectFromWallet:   true,
			AllowPartial:        true,
			EarningsPercent:     10,
			MinLeftXOF:          0,
			GraceDays:           3,
			BlockAfterDays:      0,
			ReminderDays:        2,
			DepositRefundable:   true,
		},
	}
}
