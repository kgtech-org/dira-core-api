package equipment

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var day0 = time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)

func sale(price, deposit int, plan Plan) *Contract {
	c := &Contract{Mode: ModeSale, Status: StatusActive, PriceXOF: price, DepositXOF: deposit, Plan: plan, Quantity: 1}
	c.Schedule = buildSchedule(c, day0)
	return c
}

func TestInstallmentsSumToThePriceAndTheDepositComesFirst(t *testing.T) {
	c := sale(10_000, 2_000, Plan{Schedule: ScheduleInstallments, Installments: 3, Period: PeriodWeekly, FirstDueDays: 7})
	require.Len(t, c.Schedule, 4)
	assert.Equal(t, LineDeposit, c.Schedule[0].Kind)
	assert.Equal(t, day0, c.Schedule[0].DueAt, "la caution est due à la remise")
	assert.Equal(t, []int{3_333, 3_333, 3_334}, []int{c.Schedule[1].AmountXOF, c.Schedule[2].AmountXOF, c.Schedule[3].AmountXOF}, "le reste sur la dernière")
	assert.Equal(t, day0.AddDate(0, 0, 7), c.Schedule[1].DueAt)
	assert.Equal(t, day0.AddDate(0, 0, 14), c.Schedule[2].DueAt)
	assert.Equal(t, 12_000, c.Outstanding(day0, false))
	assert.Equal(t, 2_000, c.Outstanding(day0, true), "seule la caution est échue le jour même")
}

func TestUpfrontIsOneLineDueAtHandOver(t *testing.T) {
	c := sale(5_000, 0, Plan{Schedule: ScheduleUpfront})
	require.Len(t, c.Schedule, 1)
	assert.Equal(t, 5_000, c.Outstanding(day0, true))
}

func TestEarningsDeductionRespectsPercentFixedMinLeftAndCaps(t *testing.T) {
	plan := Plan{Schedule: ScheduleInstallments, Installments: 2, Period: PeriodWeekly, FirstDueDays: 7,
		CollectFromEarnings: true, EarningsPercent: 10, EarningsFixedXOF: 100}
	c := sale(10_000, 0, plan)

	// 10 % de 2 000 + 100 fixes = 300.
	assert.Equal(t, 300, c.EarningsDeduction(2_000, 0, day0))

	// Rien n'est encore ÉCHU : avec « seulement l'échu », pas de retenue.
	c.Plan.EarningsOnlyWhenDue = true
	assert.Equal(t, 0, c.EarningsDeduction(2_000, 0, day0))
	assert.Equal(t, 300, c.EarningsDeduction(2_000, 0, day0.AddDate(0, 0, 8)))
	c.Plan.EarningsOnlyWhenDue = false

	// Laisser au moins 1 800 à l'agent.
	c.Plan.MinLeftXOF = 1_800
	assert.Equal(t, 200, c.EarningsDeduction(2_000, 0, day0))
	// … tous contrats confondus : 150 déjà retenus ailleurs.
	assert.Equal(t, 50, c.EarningsDeduction(2_000, 150, day0))
	c.Plan.MinLeftXOF = 0

	// Plafond du jour : 250 déjà pris aujourd'hui, plafond 400 → 150.
	c.Plan.DailyCapXOF = 400
	c.Payments = append(c.Payments, Payment{At: day0.Add(-time.Hour), AmountXOF: 250, Source: SourceEarnings})
	assert.Equal(t, 150, c.EarningsDeduction(2_000, 0, day0))
	// Hier ne compte pas pour aujourd'hui.
	c.Payments[0].At = day0.AddDate(0, 0, -1)
	assert.Equal(t, 300, c.EarningsDeduction(2_000, 0, day0))
	// … mais compte pour la semaine (lundi 21 sept. : dimanche 20 est la semaine d'avant).
	c.Plan.WeeklyCapXOF = 300
	c.Payments[0].At = day0.AddDate(0, 0, 1) // mardi
	assert.Equal(t, 50, c.EarningsDeduction(2_000, 0, day0.AddDate(0, 0, 2)))

	// Jamais plus que ce qui est dû, ni plus que le gain.
	c.Plan = Plan{CollectFromEarnings: true, EarningsPercent: 100, Schedule: ScheduleUpfront}
	c.Schedule = buildSchedule(c, day0)
	c.Payments = nil
	assert.Equal(t, 10_000, c.EarningsDeduction(50_000, 0, day0))
	assert.Equal(t, 700, c.EarningsDeduction(700, 0, day0))
	// Un plan sans retenue sur gains ne retient rien.
	c.Plan.CollectFromEarnings = false
	assert.Equal(t, 0, c.EarningsDeduction(2_000, 0, day0))
}

func TestPaymentsFillTheOldestLinesFirstAndSettleTheSale(t *testing.T) {
	c := sale(3_000, 1_000, Plan{Schedule: ScheduleInstallments, Installments: 3, Period: PeriodWeekly, FirstDueDays: 7})
	left := c.apply(1_500, day0)
	assert.Equal(t, 0, left)
	assert.Equal(t, LinePaid, c.Schedule[0].Status, "la caution d'abord")
	assert.Equal(t, 500, c.Schedule[1].PaidXOF)
	assert.Equal(t, 2_500, c.Outstanding(day0, false))
	left = c.apply(3_000, day0)
	assert.Equal(t, 500, left, "le trop-perçu revient")
	c.settle(day0)
	assert.Equal(t, StatusCompleted, c.Status)
}

func TestLinesAgeAndTheLateFeeIsAddedOnce(t *testing.T) {
	c := sale(4_000, 0, Plan{Schedule: ScheduleInstallments, Installments: 2, Period: PeriodWeekly, FirstDueDays: 7,
		GraceDays: 2, LateFeeXOF: 100, LateFeePercent: 5, BlockAfterDays: 5})
	assert.Empty(t, c.refreshLines(day0))
	assert.Equal(t, LinePending, c.Schedule[0].Status)

	d7 := day0.AddDate(0, 0, 7)
	assert.Empty(t, c.refreshLines(d7))
	assert.Equal(t, LineDue, c.Schedule[0].Status, "échue le jour J")

	d9 := day0.AddDate(0, 0, 9)
	assert.Equal(t, []int{0}, c.refreshLines(d9), "en retard après la grâce")
	assert.Equal(t, LineOverdue, c.Schedule[0].Status)
	assert.Equal(t, 100+100, c.Schedule[0].LateFeeXOF, "100 fixes + 5 % de 2 000")
	assert.Empty(t, c.refreshLines(day0.AddDate(0, 0, 10)), "pas une seconde fois")
	assert.Equal(t, 2_200, c.Outstanding(d9, true))

	assert.False(t, c.Blocked(day0.AddDate(0, 0, 13)), "grâce 2 + blocage 5 = J+14")
	assert.True(t, c.Blocked(day0.AddDate(0, 0, 14)))
	// Payer lève le blocage.
	c.apply(2_200, day0.AddDate(0, 0, 14))
	assert.False(t, c.Blocked(day0.AddDate(0, 0, 14)))
	// Sans seuil, jamais bloqué.
	c.Plan.BlockAfterDays = 0
	assert.False(t, c.Blocked(day0.AddDate(0, 0, 60)))
}

func TestRentalStartsWithItsFirstPeriodAndGrows(t *testing.T) {
	c := &Contract{Mode: ModeRental, Status: StatusActive, PriceXOF: 1_500, DepositXOF: 5_000,
		Plan: Plan{Schedule: SchedulePerPeriod, Period: PeriodWeekly}}
	c.Schedule = buildSchedule(c, day0)
	require.Len(t, c.Schedule, 2)
	assert.Equal(t, LineDeposit, c.Schedule[0].Kind)
	assert.Equal(t, LinePeriod, c.Schedule[1].Kind)
	c.Schedule = append(c.Schedule, nextPeriodLine(c, periodAfter(day0, PeriodWeekly)))
	assert.Equal(t, 3, c.Schedule[2].N)
	assert.Equal(t, day0.AddDate(0, 0, 7), c.Schedule[2].DueAt)
	assert.Equal(t, 8_000, c.Outstanding(day0.AddDate(0, 0, 7), true))
}

func TestMergeFillsOnlyWhatIsEmpty(t *testing.T) {
	out := merge(Plan{Installments: 6}, DefaultSettings("TG").Defaults)
	assert.Equal(t, 6, out.Installments)
	assert.Equal(t, ScheduleInstallments, out.Schedule)
	assert.Equal(t, PeriodWeekly, out.Period)
}
