package equipment

import (
	"time"
)

// Pure arithmetic of a contract: what is owed, what an earning yields, when
// a line is late. No storage, no clock of its own — `now` is always passed,
// so every rule is testable to the day.

// periodAfter rend la date une période plus tard.
func periodAfter(t time.Time, period string) time.Time {
	switch period {
	case PeriodDaily:
		return t.AddDate(0, 0, 1)
	case PeriodBiweekly:
		return t.AddDate(0, 0, 14)
	case PeriodMonthly:
		return t.AddDate(0, 1, 0)
	default:
		return t.AddDate(0, 0, 7)
	}
}

// buildSchedule writes the lines of a contract at hand-over.
//
// La caution est TOUJOURS la première ligne, due à la remise. Une vente à
// échéances répartit le prix en parts égales, le reste sur la dernière — la
// somme des lignes fait exactement le prix. Un loyer commence par sa première
// période ; les suivantes naissent au fil du temps (`nextPeriod`).
func buildSchedule(c *Contract, handedAt time.Time) []Line {
	var lines []Line
	n := 1
	if c.DepositXOF > 0 {
		lines = append(lines, Line{N: n, Kind: LineDeposit, DueAt: handedAt, AmountXOF: c.DepositXOF, Status: LineDue})
		n++
	}
	switch c.Mode {
	case ModeSale:
		switch c.Plan.Schedule {
		case ScheduleUpfront:
			lines = append(lines, Line{N: n, Kind: LineInstallment, DueAt: handedAt, AmountXOF: c.PriceXOF, Status: LineDue})
		case ScheduleInstallments:
			count := c.Plan.Installments
			if count < 1 {
				count = 1
			}
			each := c.PriceXOF / count
			due := handedAt.AddDate(0, 0, c.Plan.FirstDueDays)
			for i := 0; i < count; i++ {
				amount := each
				if i == count-1 {
					amount = c.PriceXOF - each*(count-1)
				}
				lines = append(lines, Line{N: n, Kind: LineInstallment, DueAt: due, AmountXOF: amount, Status: LinePending})
				n++
				due = periodAfter(due, c.Plan.Period)
			}
		}
	case ModeRental:
		if c.Plan.Schedule != ScheduleNone {
			lines = append(lines, Line{N: n, Kind: LinePeriod, DueAt: handedAt, AmountXOF: c.PriceXOF, Status: LineDue})
		}
	}
	return lines
}

// nextPeriodLine adds the rental period that starts at `at`.
func nextPeriodLine(c *Contract, at time.Time) Line {
	return Line{N: len(c.Schedule) + 1, Kind: LinePeriod, DueAt: at, AmountXOF: c.PriceXOF, Status: LineDue}
}

func (l *Line) owed() int {
	if l.Status == LineWaived || l.Status == LinePaid {
		return 0
	}
	return l.AmountXOF + l.LateFeeXOF - l.PaidXOF
}

// Outstanding is what the agent still owes: every unpaid line, or only the
// lines already due when `onlyDue`.
func (c *Contract) Outstanding(now time.Time, onlyDue bool) int {
	total := 0
	for i := range c.Schedule {
		l := &c.Schedule[i]
		if onlyDue && l.DueAt.After(now) {
			continue
		}
		total += l.owed()
	}
	return total
}

// OverdueSince rend la date d'échéance de la ligne en retard la plus
// ancienne (grâce comprise), ou zéro.
func (c *Contract) OverdueSince(now time.Time) time.Time {
	var oldest time.Time
	for i := range c.Schedule {
		l := &c.Schedule[i]
		if l.owed() <= 0 {
			continue
		}
		limit := l.DueAt.AddDate(0, 0, c.Plan.GraceDays)
		if !limit.After(now) && (oldest.IsZero() || l.DueAt.Before(oldest)) {
			oldest = l.DueAt
		}
	}
	return oldest
}

// Blocked dit si le retard passe le seuil de blocage du plan.
func (c *Contract) Blocked(now time.Time) bool {
	if c.Plan.BlockAfterDays <= 0 || c.Status != StatusActive {
		return false
	}
	since := c.OverdueSince(now)
	if since.IsZero() {
		return false
	}
	return !since.AddDate(0, 0, c.Plan.GraceDays+c.Plan.BlockAfterDays).After(now)
}

// takenSince sums what earnings deductions took from this contract since `from`.
func (c *Contract) takenSince(from time.Time) int {
	sum := 0
	for _, p := range c.Payments {
		if p.Source == SourceEarnings && !p.At.Before(from) && p.AmountXOF > 0 {
			sum += p.AmountXOF
		}
	}
	return sum
}

// startOfDay / startOfWeek en UTC — l'heure légale des pays ouverts.
func startOfDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func startOfWeek(t time.Time) time.Time {
	d := startOfDay(t)
	wd := int(d.Weekday())
	if wd == 0 {
		wd = 7
	}
	return d.AddDate(0, 0, -(wd - 1))
}

// EarningsDeduction rend ce que ce contrat retient sur un gain de `earning`
// francs, `already` ayant déjà été retenu par d'autres contrats sur le même
// gain. Zéro quand rien n'est dû, quand le plan ne retient pas sur les
// gains, ou quand les bornes l'interdisent.
func (c *Contract) EarningsDeduction(earning, already int, now time.Time) int {
	if c.Status != StatusActive || !c.Plan.CollectFromEarnings || earning <= 0 {
		return 0
	}
	owed := c.Outstanding(now, c.Plan.EarningsOnlyWhenDue)
	if owed <= 0 {
		return 0
	}
	take := earning*c.Plan.EarningsPercent/100 + c.Plan.EarningsFixedXOF
	if take > owed {
		take = owed
	}
	// Ce qu'on laisse au moins à l'agent, tous contrats confondus.
	if c.Plan.MinLeftXOF > 0 {
		if room := earning - already - c.Plan.MinLeftXOF; take > room {
			take = room
		}
	}
	if left := earning - already; take > left {
		take = left
	}
	if c.Plan.DailyCapXOF > 0 {
		if room := c.Plan.DailyCapXOF - c.takenSince(startOfDay(now)); take > room {
			take = room
		}
	}
	if c.Plan.WeeklyCapXOF > 0 {
		if room := c.Plan.WeeklyCapXOF - c.takenSince(startOfWeek(now)); take > room {
			take = room
		}
	}
	if take < 0 {
		return 0
	}
	return take
}

// apply spreads a payment over the unpaid lines, oldest due first, and
// returns what could not be placed (an over-payment).
func (c *Contract) apply(amount int, at time.Time) int {
	for i := range c.Schedule {
		if amount <= 0 {
			break
		}
		l := &c.Schedule[i]
		owed := l.owed()
		if owed <= 0 {
			continue
		}
		part := owed
		if part > amount {
			part = amount
		}
		l.PaidXOF += part
		amount -= part
		if l.owed() == 0 {
			paidAt := at
			l.PaidAt = &paidAt
			l.Status = LinePaid
		}
	}
	return amount
}

// settle marks the contract completed when a sale is fully paid.
func (c *Contract) settle(now time.Time) {
	if c.Mode == ModeSale && c.Status == StatusActive && c.Outstanding(now, false) == 0 {
		c.Status = StatusCompleted
		closed := now
		c.ClosedAt = &closed
	}
}

// refreshLines ages the schedule: pending → due at the due date, due →
// overdue past the grace period, with the late fee added ONCE.
func (c *Contract) refreshLines(now time.Time) (newlyOverdue []int) {
	for i := range c.Schedule {
		l := &c.Schedule[i]
		if l.owed() <= 0 {
			continue
		}
		if l.Status == LinePending && !l.DueAt.After(now) {
			l.Status = LineDue
		}
		if l.Status == LineDue && !l.DueAt.AddDate(0, 0, c.Plan.GraceDays).After(now) {
			l.Status = LineOverdue
			l.LateFeeXOF = c.Plan.LateFeeXOF + l.AmountXOF*c.Plan.LateFeePercent/100
			newlyOverdue = append(newlyOverdue, i)
		}
	}
	return newlyOverdue
}

// merge fills the empty fields of a plan from a fallback: the item's
// default, then the country's.
func merge(plan, fallback Plan) Plan {
	out := plan
	if out.Schedule == "" {
		out.Schedule = fallback.Schedule
	}
	if out.Installments == 0 {
		out.Installments = fallback.Installments
	}
	if out.Period == "" {
		out.Period = fallback.Period
	}
	return out
}
