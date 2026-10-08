package receipt

// LE RENDU EN PDF.
//
// A4, une seule famille de caractères, aucune image : un reçu doit s'ouvrir
// partout, s'imprimer en noir et blanc et peser quelques kilo-octets. Un
// document chargé de logos et de dégradés se transfère mal sur un réseau
// africain, et c'est justement par courriel ou par WhatsApp qu'il voyage.
//
// ⚠️ LES POLICES DE BASE ET L'ENCODAGE CP1252, pas de police embarquée. Les
// quatorze polices PDF standard n'ont pas à être transportées — elles sont dans
// le lecteur —, mais elles ne connaissent pas l'UTF-8 : il faut traduire. Les
// accents, les guillemets français et le tiret cadratin y sont tous ; les
// émojis non, et c'est pour cela qu'il n'y en a aucun dans ce paquet.

import (
	"bytes"
	"fmt"
	"time"

	"github.com/go-pdf/fpdf"
)

const (
	marginX   = 15.0
	pageWidth = 210.0
	bodyWidth = pageWidth - 2*marginX
	amountCol = 34.0
	ink       = 25
	inkMuted  = 115
	ruleShade = 205
)

// Options porte ce que le rendu ne peut pas deviner.
type Options struct {
	// Locale : `fr` (défaut) ou `en`.
	Locale string
	// Location : le fuseau du PAYS DE L'OPÉRATION. Voir `Stamp`.
	Location *time.Location
	// Brand est le nom imprimé en tête. Vide = « Dira ».
	Brand string
	// Footer est la ligne de pied — mentions légales du pays, adresse de
	// l'entité. Vide = la mention par défaut.
	Footer string
}

// Render dessine le document et rend le PDF.
//
// ⚠️ IL VÉRIFIE AVANT DE DESSINER. Voir `ErrLinesDoNotAddUp` : un reçu dont le
// détail ne fait pas le total n'est pas imprimé du tout.
func Render(d Document, o Options) ([]byte, error) {
	if err := d.Check(); err != nil {
		return nil, err
	}
	l := lang(o.Locale)
	brand := o.Brand
	if brand == "" {
		brand = "Dira"
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginX, 14, marginX)
	pdf.SetAutoPageBreak(true, 18)
	tr := pdf.UnicodeTranslatorFromDescriptor("") // cp1252
	footer := o.Footer
	if footer == "" {
		footer = l.generatedBy
	}
	pdf.SetFooterFunc(func() {
		pdf.SetY(-14)
		pdf.SetFont("Helvetica", "", 7.5)
		pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
		pdf.CellFormat(bodyWidth/2, 5, tr(footer), "", 0, "L", false, 0, "")
		pdf.CellFormat(bodyWidth/2, 5,
			tr(fmt.Sprintf("%s %d", l.page, pdf.PageNo())), "", 0, "R", false, 0, "")
	})
	pdf.AddPage()

	r := &renderer{pdf: pdf, tr: tr, d: d, l: l, o: o}
	r.header(brand)
	r.parties()
	if d.Journey != nil {
		r.journey(*d.Journey)
	}
	if len(d.Rows) > 0 {
		r.statement()
	}
	if len(d.Lines) > 0 {
		r.lines()
	}
	if len(d.Summary) > 0 {
		r.summary()
	}
	r.payment()
	r.notes()

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("receipt: render: %w", err)
	}
	return buf.Bytes(), nil
}

type renderer struct {
	pdf *fpdf.Fpdf
	tr  func(string) string
	d   Document
	l   labels
	o   Options
}

func (r *renderer) header(brand string) {
	title := r.l.receiptRide
	switch r.d.Kind {
	case KindOrder:
		title = r.l.receiptOrder
	case KindStatement:
		title = r.l.statement
	}
	r.pdf.SetTextColor(ink, ink, ink)
	r.pdf.SetFont("Helvetica", "B", 19)
	r.pdf.CellFormat(bodyWidth/2, 9, r.tr(brand), "", 0, "L", false, 0, "")
	r.pdf.SetFont("Helvetica", "", 13)
	r.pdf.CellFormat(bodyWidth/2, 9, r.tr(title), "", 1, "R", false, 0, "")

	r.pdf.SetFont("Helvetica", "", 8.5)
	r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
	left := r.d.Country
	if r.d.CurrencyCode != "" {
		left += " · " + r.d.CurrencyCode
	}
	right := r.l.issuedAt + " " + Stamp(r.d.IssuedAt, r.o.Location)
	if r.d.Kind == KindStatement {
		left = fmt.Sprintf("%s : %s → %s", r.l.period,
			DayStamp(r.d.PeriodFrom, r.o.Location), DayStamp(r.d.PeriodTo, r.o.Location))
	} else if r.d.Number != "" {
		left = r.l.number + " " + r.d.Number + "   ·   " + left
	}
	r.pdf.CellFormat(bodyWidth/2, 5, r.tr(left), "", 0, "L", false, 0, "")
	r.pdf.CellFormat(bodyWidth/2, 5, r.tr(right), "", 1, "R", false, 0, "")
	if r.d.Kind != KindStatement && !r.d.OccurredAt.IsZero() {
		r.pdf.CellFormat(bodyWidth, 5,
			r.tr(r.l.operationOf+" "+Stamp(r.d.OccurredAt, r.o.Location)), "", 1, "L", false, 0, "")
	}
	r.rule(6)
}

func (r *renderer) rule(gap float64) {
	r.pdf.Ln(gap / 2)
	r.pdf.SetDrawColor(ruleShade, ruleShade, ruleShade)
	y := r.pdf.GetY()
	r.pdf.Line(marginX, y, pageWidth-marginX, y)
	r.pdf.Ln(gap / 2)
}

func (r *renderer) sectionTitle(s string) {
	r.pdf.SetFont("Helvetica", "B", 9)
	r.pdf.SetTextColor(ink, ink, ink)
	r.pdf.CellFormat(bodyWidth, 6, r.tr(s), "", 1, "L", false, 0, "")
}

func (r *renderer) parties() {
	if len(r.d.Parties) == 0 {
		return
	}
	// Deux colonnes : le payeur à gauche, celui qui a servi à droite. Au delà
	// de deux parties, on continue en lignes — un reçu de commande peut citer
	// plusieurs enseignes.
	w := bodyWidth / 2
	for i := 0; i < len(r.d.Parties); i += 2 {
		for j := i; j < i+2 && j < len(r.d.Parties); j++ {
			p := r.d.Parties[j]
			r.pdf.SetFont("Helvetica", "", 7.5)
			r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
			r.pdf.CellFormat(w, 4, r.tr(p.Role), "", 0, "L", false, 0, "")
		}
		r.pdf.Ln(4)
		for j := i; j < i+2 && j < len(r.d.Parties); j++ {
			p := r.d.Parties[j]
			r.pdf.SetFont("Helvetica", "B", 10)
			r.pdf.SetTextColor(ink, ink, ink)
			name := p.Name
			if name == "" {
				name = "—"
			}
			r.pdf.CellFormat(w, 5, r.tr(name), "", 0, "L", false, 0, "")
		}
		r.pdf.Ln(5)
		any := false
		for j := i; j < i+2 && j < len(r.d.Parties); j++ {
			if r.d.Parties[j].Detail != "" {
				any = true
			}
		}
		if any {
			for j := i; j < i+2 && j < len(r.d.Parties); j++ {
				r.pdf.SetFont("Helvetica", "", 8.5)
				r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
				r.pdf.CellFormat(w, 4.5, r.tr(r.d.Parties[j].Detail), "", 0, "L", false, 0, "")
			}
			r.pdf.Ln(4.5)
		}
	}
	r.rule(6)
}

// journey imprime le trajet — et c'est le bloc pour lequel ce document existe.
func (r *renderer) journey(j Journey) {
	r.sectionTitle(r.l.journey)
	for _, s := range j.Stops {
		kind := r.l.stop
		switch s.Kind {
		case "pickup":
			kind = r.l.pickup
		case "dest":
			kind = r.l.dest
		}
		r.pdf.SetFont("Helvetica", "", 7.5)
		r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
		r.pdf.CellFormat(22, 5, r.tr(kind), "", 0, "L", false, 0, "")
		r.pdf.SetFont("Helvetica", "", 9.5)
		r.pdf.SetTextColor(ink, ink, ink)
		at := ""
		if s.At != nil {
			at = Stamp(*s.At, r.o.Location)
		}
		r.pdf.CellFormat(bodyWidth-22-28, 5, r.tr(s.Label), "", 0, "L", false, 0, "")
		r.pdf.SetFont("Helvetica", "", 8)
		r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
		r.pdf.CellFormat(28, 5, r.tr(at), "", 1, "R", false, 0, "")
	}
	r.pdf.Ln(2)

	// ⚠️ LA DISTANCE RÉELLEMENT PARCOURUE EN GRAND, et l'estimation du devis à
	// côté — jamais à la place. L'écart entre les deux est exactement ce qu'une
	// réclamation examine (« il a fait un détour »), et un document qui n'en
	// porterait qu'une ne permettrait pas de la trancher.
	r.pdf.SetFont("Helvetica", "", 7.5)
	r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
	r.pdf.CellFormat(bodyWidth/3, 4, r.tr(r.l.distance), "", 0, "L", false, 0, "")
	r.pdf.CellFormat(bodyWidth/3, 4, r.tr(r.l.duration), "", 0, "L", false, 0, "")
	if j.ApproachDistanceM > 0 || j.ApproachDurationS > 0 {
		r.pdf.CellFormat(bodyWidth/3, 4, r.tr(r.l.approach), "", 0, "L", false, 0, "")
	}
	r.pdf.Ln(4)
	r.pdf.SetFont("Helvetica", "B", 13)
	r.pdf.SetTextColor(ink, ink, ink)
	r.pdf.CellFormat(bodyWidth/3, 6, r.tr(Km(j.DistanceM)), "", 0, "L", false, 0, "")
	r.pdf.CellFormat(bodyWidth/3, 6, r.tr(Duration(j.DurationS)), "", 0, "L", false, 0, "")
	if j.ApproachDistanceM > 0 || j.ApproachDurationS > 0 {
		r.pdf.SetFont("Helvetica", "", 11)
		r.pdf.CellFormat(bodyWidth/3, 6,
			r.tr(Km(j.ApproachDistanceM)+" · "+Duration(j.ApproachDurationS)), "", 0, "L", false, 0, "")
	}
	r.pdf.Ln(6)

	r.pdf.SetFont("Helvetica", "", 8)
	r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
	note := SourceNote(j.DistanceSource, r.o.Locale)
	if j.PlannedDistanceM > 0 && j.PlannedDistanceM != j.DistanceM {
		note += "   ·   " + r.l.planned + " : " + Km(j.PlannedDistanceM)
	}
	r.pdf.MultiCell(bodyWidth, 4.5, r.tr(note), "", "L", false)
	r.rule(5)
}

func (r *renderer) lines() {
	r.pdf.SetFont("Helvetica", "", 7.5)
	r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
	r.pdf.CellFormat(bodyWidth-amountCol, 5, r.tr(r.l.detail), "", 0, "L", false, 0, "")
	r.pdf.CellFormat(amountCol, 5, r.tr(r.l.amount), "", 1, "R", false, 0, "")

	for _, line := range r.d.Lines {
		style := ""
		if line.Strong {
			style = "B"
		}
		r.pdf.SetFont("Helvetica", style, 9.5)
		r.pdf.SetTextColor(ink, ink, ink)
		label := line.Label
		if line.Detail != "" {
			label += "   " + line.Detail
		}
		r.pdf.CellFormat(bodyWidth-amountCol, 5.5, r.tr(label), "", 0, "L", false, 0, "")
		if line.NoAmount {
			r.pdf.Ln(5.5)
			continue
		}
		r.pdf.SetFont("Helvetica", style, 9.5)
		r.pdf.CellFormat(amountCol, 5.5, r.tr(r.d.Money(line.Amount)), "", 1, "R", false, 0, "")
	}

	r.rule(4)
	label := r.d.TotalLabel
	if label == "" {
		label = r.l.total
	}
	r.pdf.SetFont("Helvetica", "B", 12)
	r.pdf.SetTextColor(ink, ink, ink)
	r.pdf.CellFormat(bodyWidth-amountCol, 8, r.tr(label), "", 0, "L", false, 0, "")
	r.pdf.CellFormat(amountCol, 8, r.tr(r.d.Money(r.d.Total)), "", 1, "R", false, 0, "")
}

func (r *renderer) statement() {
	cols := r.d.RowCols
	// La dernière colonne porte l'argent ; les autres se partagent le reste.
	fixed := 0.0
	flex := 0
	for _, c := range cols {
		if c.Width > 0 {
			fixed += c.Width
		} else {
			flex++
		}
	}
	rest := bodyWidth - fixed - amountCol
	width := func(c Column) float64 {
		if c.Width > 0 {
			return c.Width
		}
		if flex == 0 {
			return rest
		}
		return rest / float64(flex)
	}

	r.pdf.SetFont("Helvetica", "", 7.5)
	r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
	for _, c := range cols {
		align := "L"
		if c.Right {
			align = "R"
		}
		r.pdf.CellFormat(width(c), 5, r.tr(c.Label), "", 0, align, false, 0, "")
	}
	r.pdf.CellFormat(amountCol, 5, r.tr(r.l.amount), "", 1, "R", false, 0, "")

	for _, row := range r.d.Rows {
		r.pdf.SetFont("Helvetica", "", 9)
		r.pdf.SetTextColor(ink, ink, ink)
		for i, c := range cols {
			cell := ""
			if i < len(row.Cells) {
				cell = row.Cells[i]
			}
			align := "L"
			if c.Right {
				align = "R"
			}
			r.pdf.CellFormat(width(c), 5.2, r.tr(cell), "", 0, align, false, 0, "")
		}
		r.pdf.CellFormat(amountCol, 5.2, r.tr(r.d.Money(row.Amount)), "", 1, "R", false, 0, "")
	}
	r.rule(4)
}

func (r *renderer) summary() {
	for _, s := range r.d.Summary {
		r.pdf.SetFont("Helvetica", "", 8)
		r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
		r.pdf.CellFormat(bodyWidth-amountCol, 5.5, r.tr(s.Label+"   "+s.Detail), "", 0, "L", false, 0, "")
		if s.NoAmount {
			r.pdf.Ln(5.5)
			continue
		}
		r.pdf.SetFont("Helvetica", "B", 10)
		r.pdf.SetTextColor(ink, ink, ink)
		r.pdf.CellFormat(amountCol, 5.5, r.tr(r.d.Money(s.Amount)), "", 1, "R", false, 0, "")
	}
}

// payment dit comment l'opération a été payée.
//
// ⚠️ « NON RÉGLÉ » N'EST PAS « IMPAYÉ ». Une course en espèces terminée n'est pas
// réglée au sens de la plateforme — c'est le chauffeur qui a l'argent — et un
// reçu qui annoncerait « en attente de paiement » à quelqu'un qui vient de payer
// de la main à la main serait faux, et vexant.
func (r *renderer) payment() {
	if r.d.Payment.Method == "" {
		return
	}
	r.pdf.Ln(3)
	r.pdf.SetFont("Helvetica", "", 7.5)
	r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
	r.pdf.CellFormat(bodyWidth, 4, r.tr(r.l.payment), "", 1, "L", false, 0, "")
	state := r.l.pendingPayment
	switch {
	case r.d.Payment.Method == "cash":
		state = r.l.cash
	case r.d.Payment.Settled:
		state = r.l.paid
	}
	line := r.d.Payment.Method + " — " + state
	if r.d.Payment.Reference != "" {
		line += "   ·   " + r.d.Payment.Reference
	}
	r.pdf.SetFont("Helvetica", "", 9.5)
	r.pdf.SetTextColor(ink, ink, ink)
	r.pdf.CellFormat(bodyWidth, 5, r.tr(line), "", 1, "L", false, 0, "")
}

func (r *renderer) notes() {
	if len(r.d.Notes) == 0 {
		return
	}
	r.pdf.Ln(3)
	r.pdf.SetFont("Helvetica", "", 8)
	r.pdf.SetTextColor(inkMuted, inkMuted, inkMuted)
	for _, n := range r.d.Notes {
		r.pdf.MultiCell(bodyWidth, 4.3, r.tr("· "+n), "", "L", false)
	}
}
