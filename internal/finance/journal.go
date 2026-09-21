package finance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kgtech-org/dira-core-api/internal/token"
)

// LE JOURNAL : d'un mouvement de portefeuille à une écriture en partie double.
//
// La règle de traduction est ICI, pure, et testée ligne par ligne : c'est
// elle qui dit ce que la plateforme gagne, doit et détient. Un motif inconnu
// n'est pas deviné : il produit une écriture « à classer » sur un compte
// d'attente, et le balayage la signale.

// Journal écrit une écriture par mouvement, dans la même transaction Mongo
// que le mouvement. Implémente `token.Journal`.
type Journal struct {
	repo          *Repository
	tokenPriceXOF int
	now           func() time.Time
}

func NewJournal(repo *Repository, tokenPriceXOF int) *Journal {
	if tokenPriceXOF <= 0 {
		tokenPriceXOF = token.DefaultTokenPriceXOF
	}
	return &Journal{repo: repo, tokenPriceXOF: tokenPriceXOF, now: func() time.Time { return time.Now().UTC() }}
}

// Post traduit et écrit. Une traduction impossible n'annule PAS le mouvement
// (l'argent a bougé, c'est ce qui compte) : elle écrit une écriture en
// attente que l'aperçu et le balayage montrent.
func (j *Journal) Post(ctx context.Context, tx *token.Transaction, w *token.Wallet) error {
	entry := EntryFor(tx, w, j.tokenPriceXOF)
	if entry == nil {
		return nil
	}
	return j.repo.InsertEntry(ctx, entry)
}

// AccSuspense : le compte d'attente des mouvements que la règle ne sait pas
// classer. Vide en régime normal ; tout solde ici est un constat.
const AccSuspense = "suspense"

// EntryFor rend l'écriture d'un mouvement de portefeuille, ou nil quand il
// ne déplace rien.
func EntryFor(tx *token.Transaction, w *token.Wallet, tokenPriceXOF int) *Entry {
	if tx == nil || tx.Amount == 0 {
		return nil
	}
	e := &Entry{
		At: tx.CreatedAt, Service: "core", Reason: tx.Reason, RefKind: tx.RefKind,
		Unit: tx.Unit, Description: describe(tx, w),
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if e.Unit == "" {
		e.Unit = UnitToken
	}
	if w != nil {
		e.Country = w.Country
		e.OwnerID = w.OwnerID.Hex()
		wid := w.ID
		e.WalletID = &wid
	}
	if !tx.ID.IsZero() {
		id := tx.ID
		e.TransactionID = &id
	}
	if tx.RefID != nil {
		e.RefID = tx.RefID.Hex()
	}
	amount := tx.Amount
	if amount < 0 {
		amount = -amount
	}
	credit := tx.Kind == token.KindPurchase // un crédit du portefeuille
	source, _ := tx.Ref["source"].(string)

	if e.Unit == UnitToken {
		// Les JETONS : valorisés au prix du jeton. Le passif « jetons
		// prépayés » monte quand un agent en achète (ou en reçoit), descend
		// et devient un produit quand il les consomme.
		e.Tokens = amount
		e.Amount = amount * tokenPriceXOF
		switch {
		case tx.Reason == token.ReasonTopup && credit:
			e.Lines = lines(AccMobileMoney, AccAgentTokens, e.Amount)
		case tx.Reason == token.ReasonOperatorCredit && credit:
			e.Lines = lines(AccExpenseCredits, AccAgentTokens, e.Amount)
		case strings.HasSuffix(tx.Reason, "_refund") && credit:
			// Ce qu'une acceptation avait pris et qui revient.
			e.Lines = lines(AccRevenueTokens, AccAgentTokens, e.Amount)
		case !credit:
			e.Lines = lines(AccAgentTokens, AccRevenueTokens, e.Amount)
		default:
			e.Lines = lines(AccSuspense, AccAgentTokens, e.Amount)
		}
		return e
	}

	e.Amount = amount
	clearing := clearingOf(tx.RefKind)
	walletAcc := walletAccount(w)
	switch tx.Reason {
	case token.ReasonWalletTopup:
		e.Lines = lines(AccMobileMoney, AccClientWallets, amount)
	case token.ReasonPromoCredit:
		e.Lines = lines(AccExpensePromo, AccPromoCredits, amount)
	case token.ReasonPayment:
		from := AccClientWallets
		if source == "promo" {
			from = AccPromoCredits
		}
		e.Lines = lines(from, clearing, amount)
	case token.ReasonTip:
		from := AccClientWallets
		if source == "promo" {
			from = AccPromoCredits
		}
		e.Lines = lines(from, AccAgentPayables, amount)
	case token.ReasonRefund:
		e.Lines = lines(clearing, AccClientWallets, amount)
	case token.ReasonOrderPayout:
		e.Lines = lines(AccOrderClearing, AccMerchantPayables, amount)
	case token.ReasonDeliveryFee:
		e.Lines = lines(AccOrderClearing, AccAgentPayables, amount)
	case token.ReasonEquipment:
		if credit {
			// La caution rendue.
			e.Lines = lines(AccRevenueEquipment, walletAcc, amount)
		} else {
			e.Lines = lines(walletAcc, AccRevenueEquipment, amount)
		}
	case ReasonCommission:
		// Une commission retenue sur ce que l'on versait à l'agent.
		e.Lines = lines(walletAcc, AccRevenueCommission, amount)
	case ReasonDebtRepaid:
		e.Lines = lines(walletAcc, AccAgentReceivable, amount)
	default:
		if credit {
			e.Lines = lines(AccSuspense, walletAcc, amount)
		} else {
			e.Lines = lines(walletAcc, AccSuspense, amount)
		}
	}
	return e
}

// Motifs d'argent que ce module ajoute au grand livre des portefeuilles.
const (
	// ReasonCommission : la part de la plateforme retenue sur un gain versé
	// à un agent en mode `commission`.
	ReasonCommission = "commission"
	// ReasonDebtRepaid : ce qu'un agent rembourse de sa dette (commission sur
	// espèces) — retenu sur un gain ou réglé à l'agence.
	ReasonDebtRepaid = "debt_repaid"
)

func lines(debit, credit string, amount int) []Line {
	return []Line{{Account: debit, Debit: amount}, {Account: credit, Credit: amount}}
}

func clearingOf(refKind string) string {
	switch refKind {
	case token.RefRide, token.RefRideAdjustment, token.RefTip:
		return AccRideClearing
	case token.RefOrder:
		return AccOrderClearing
	}
	return AccSuspense
}

// walletAccount : le passif que porte un portefeuille selon son propriétaire.
func walletAccount(w *token.Wallet) string {
	if w == nil {
		return AccSuspense
	}
	switch w.Type {
	case token.WalletTypeClient:
		return AccClientWallets
	case token.WalletTypeMerchant:
		return AccMerchantPayables
	default:
		return AccAgentPayables
	}
}

func describe(tx *token.Transaction, w *token.Wallet) string {
	who := "portefeuille"
	if w != nil {
		switch w.Type {
		case token.WalletTypeClient:
			who = "client"
		case token.WalletTypeMerchant:
			who = "marchand"
		default:
			who = "agent"
		}
	}
	ref := ""
	if tx.RefKind != "" && tx.RefID != nil {
		ref = " · " + tx.RefKind + " #" + strings.ToUpper(tx.RefID.Hex()[len(tx.RefID.Hex())-5:])
	}
	return fmt.Sprintf("%s · %s%s", tx.Reason, who, ref)
}

// --- les événements des verticales ---

// Event : ce qu'une verticale dit au journal quand de l'argent bouge chez
// elle sans passer par un portefeuille du socle — le grand livre des
// chauffeurs, surtout.
type Event struct {
	Kind          string `json:"kind" validate:"required,oneof=ride_settled ledger_entry"`
	Country       string `json:"country" validate:"required,len=2"`
	Vertical      string `json:"vertical" validate:"required,oneof=vtc food"`
	RefKind       string `json:"ref_kind"`
	RefID         string `json:"ref_id"`
	OwnerID       string `json:"owner_id"`
	At            string `json:"at"`
	PaymentMethod string `json:"payment_method"`
	// ride_settled
	FareXOF       int `json:"fare_xof"`
	CommissionXOF int `json:"commission_xof"`
	DriverXOF     int `json:"driver_xof"`
	// ledger_entry : `payout` · `settlement` · `adjustment` · `equipment` · `tip` · `commission`
	EntryKind string `json:"entry_kind"`
	AmountXOF int    `json:"amount_xof"`
	Note      string `json:"note"`
}

// EntriesForEvent traduit un événement de verticale. Plusieurs écritures
// possibles (une course en ligne répartit le prix en deux).
func EntriesForEvent(ev Event, now time.Time) []Entry {
	at := now
	if t, err := time.Parse(time.RFC3339, ev.At); err == nil {
		at = t
	}
	base := func(reason, desc string) Entry {
		return Entry{Country: ev.Country, At: at, Service: ev.Vertical, Reason: reason, RefKind: ev.RefKind, RefID: ev.RefID, OwnerID: ev.OwnerID, Unit: UnitXOF, Description: desc}
	}
	var out []Entry
	switch ev.Kind {
	case "ride_settled":
		switch ev.PaymentMethod {
		case "cash":
			// L'argent est dans la poche du chauffeur ; il doit la commission.
			if ev.CommissionXOF > 0 {
				e := base("ride_commission_cash", "commission due sur course en espèces")
				e.Amount = ev.CommissionXOF
				e.Lines = lines(AccAgentReceivable, AccRevenueCommission, ev.CommissionXOF)
				out = append(out, e)
			}
		default:
			// La plateforme a encaissé le prix (transit) : elle le répartit.
			if ev.DriverXOF > 0 {
				e := base("ride_earning", "part du chauffeur sur course payée en ligne")
				e.Amount = ev.DriverXOF
				e.Lines = lines(AccRideClearing, AccAgentPayables, ev.DriverXOF)
				out = append(out, e)
			}
			if ev.CommissionXOF > 0 {
				e := base("ride_commission", "commission sur course payée en ligne")
				e.Amount = ev.CommissionXOF
				e.Lines = lines(AccRideClearing, AccRevenueCommission, ev.CommissionXOF)
				out = append(out, e)
			}
		}
	case "ledger_entry":
		amount := ev.AmountXOF
		if amount == 0 {
			return nil
		}
		abs := amount
		if abs < 0 {
			abs = -abs
		}
		e := base("ledger_"+ev.EntryKind, ev.Note)
		e.Amount = abs
		switch ev.EntryKind {
		case "payout":
			// La plateforme verse au chauffeur ce qu'elle lui doit.
			e.Lines = lines(AccAgentPayables, AccMobileMoney, abs)
		case "settlement":
			// Le chauffeur règle sa dette.
			e.Lines = lines(AccMobileMoney, AccAgentReceivable, abs)
		case "adjustment":
			if amount > 0 {
				e.Lines = lines(AccExpenseCredits, AccAgentPayables, abs)
			} else {
				e.Lines = lines(AccAgentPayables, AccExpenseWriteoff, abs)
			}
		case "equipment":
			if amount < 0 {
				e.Lines = lines(AccAgentPayables, AccRevenueEquipment, abs)
			} else {
				e.Lines = lines(AccRevenueEquipment, AccAgentPayables, abs)
			}
		case "tip":
			// Déjà écrit par le socle au débit du passager : rien de plus.
			return nil
		case "commission":
			e.Lines = lines(AccAgentReceivable, AccRevenueCommission, abs)
		default:
			e.Lines = lines(AccSuspense, AccAgentPayables, abs)
		}
		out = append(out, e)
	}
	return out
}
