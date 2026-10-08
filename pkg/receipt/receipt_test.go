package receipt

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LES REÇUS — ce qu'un document imprimé doit garantir.

func rideDoc() Document {
	at := time.Date(2026, 10, 8, 14, 30, 0, 0, time.UTC)
	return Document{
		Kind: KindRide, Number: "a1b2c3", IssuedAt: at, OccurredAt: at,
		Country: "TG", CurrencyCode: "XOF", CurrencySymbol: "F CFA",
		Parties: []Party{
			{Role: "Client", Name: "Awa Ndiaye"},
			{Role: "Chauffeur", Name: "Kodjo A.", Detail: "Toyota Vitz · TG-4821-AB"},
		},
		Journey: &Journey{
			Stops: []Stop{
				{Kind: "pickup", Label: "Hédzranawoé", At: &at},
				{Kind: "dest", Label: "Aéroport de Lomé"},
			},
			DistanceM: 8234, PlannedDistanceM: 7900, DistanceSource: "tracked",
			DurationS: 1660, ApproachDistanceM: 2100, ApproachDurationS: 420,
		},
		Lines: []Line{
			{Label: "Prise en charge", Amount: 500},
			{Label: "Distance", Detail: "8,2 km", Amount: 1650},
			{Label: "Attente", Detail: "4 min", Amount: 200},
			{Label: "Code promo", Detail: "DIRA2000", Amount: -500},
		},
		Total:   1850,
		Payment: Payment{Method: "cash"},
	}
}

// ⚠️ LE TEST QUI COMPTE LE PLUS : un reçu dont le détail ne fait pas le total
// n'est PAS imprimé. Un tel document prouve une erreur, il porte notre nom, et
// c'est le client qui le découvre — souvent devant son comptable. Mieux vaut une
// erreur 500, qui est un incident qu'on corrige, qu'un PDF faux, qui est une
// réclamation qu'on ne peut pas gagner.
func TestAReceiptWhoseLinesDoNotAddUpIsNotPrinted(t *testing.T) {
	d := rideDoc()
	d.Total = 9999 // quelqu'un a touché au total sans toucher aux lignes

	out, err := Render(d, Options{})
	require.ErrorIs(t, err, ErrLinesDoNotAddUp)
	assert.Nil(t, out, "rien ne sort — surtout pas un document partiel")
	// Le message dit les DEUX nombres : sans eux, on cherche l'écart à la main.
	assert.Contains(t, err.Error(), "1850")
	assert.Contains(t, err.Error(), "9999")
}

// Une ligne d'information ne compte pas dans la somme : elle n'a pas de montant.
func TestAnInformationLineDoesNotBreakTheSum(t *testing.T) {
	d := rideDoc()
	d.Lines = append(d.Lines, Line{Label: "Majoration aéroport appliquée", NoAmount: true, Amount: 7777})
	require.NoError(t, d.Check())
}

func TestARideReceiptRendersAPdf(t *testing.T) {
	out, err := Render(rideDoc(), Options{Locale: "fr"})
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(out, []byte("%PDF-")), "un PDF commence par %%PDF-")
	assert.Greater(t, len(out), 800)
	// ⚠️ Et il reste LÉGER : ce document voyage par WhatsApp sur un réseau
	// africain. Un reçu de plusieurs mégaoctets ne serait jamais reçu.
	assert.Less(t, len(out), 40_000)
}

// ⚠️ LA SOURCE DE LA DISTANCE EST TOUJOURS DITE. « 11,4 km » présenté comme
// mesuré alors qu'il vient d'une estimation est la phrase d'un reçu qu'on ne peut
// plus défendre — et c'est le cas COURANT, pas le cas rare : il suffit que le
// téléphone du chauffeur ait perdu le réseau.
func TestTheDistanceAlwaysSaysWhereItComesFrom(t *testing.T) {
	tracked := SourceNote("tracked", "fr")
	planned := SourceNote("planned", "fr")
	assert.NotEqual(t, tracked, planned, "les deux cas ne peuvent pas se lire pareil")
	assert.Contains(t, planned, "estimée")
	assert.Contains(t, tracked, "réel")
	// Une source vide — un vieux trajet d'avant la mesure — se lit comme une
	// estimation, jamais comme une mesure.
	assert.Equal(t, planned, SourceNote("", "fr"))
}

// ⚠️ L'APPROCHE N'EST PAS DANS LA DISTANCE DE LA COURSE. C'est l'invariant du
// socle (`actual_distance_m` ne compte plus l'approche), et le reçu doit le
// refléter : les deux chiffres sont imprimés SÉPARÉMENT. Les additionner ferait
// lire « 10,3 km » pour une course de 8,2 km, et le prix ne collerait plus avec
// la distance affichée juste au-dessus.
func TestTheApproachIsPrintedApartFromTheRide(t *testing.T) {
	d := rideDoc()
	j := *d.Journey
	assert.Equal(t, 8234, j.DistanceM)
	assert.Equal(t, 2100, j.ApproachDistanceM)
	assert.NotEqual(t, j.DistanceM, j.DistanceM+j.ApproachDistanceM)
	out, err := Render(d, Options{Locale: "fr"})
	require.NoError(t, err)
	assert.NotEmpty(t, out)
}

// --- LES MONTANTS -------------------------------------------------------

// ⚠️ LES DÉCIMALES VIENNENT DE LA MONNAIE. Un franc n'a pas de centimes et
// l'entier stocké EST l'unité ; un cedi en a deux, et l'entier stocké est en
// centièmes. Imprimer « 250000 GH₵ » là où il fallait « 2500,00 GH₵ » est une
// erreur de facteur cent sur un document qu'on présente.
func TestMoneyFollowsTheCurrencyNotTheHabit(t *testing.T) {
	franc := Document{CurrencyCode: "XOF", CurrencySymbol: "F CFA", Decimals: 0}
	cedi := Document{CurrencyCode: "GHS", CurrencySymbol: "GH₵", Decimals: 2}

	assert.Equal(t, "2 500 F CFA", franc.Money(2500))
	assert.Equal(t, "25,00 GH₵", cedi.Money(2500))
	// Un montant sans symbole retombe sur le code ISO plutôt que sur rien :
	// « 2 500 » nu sur un reçu ne dit pas dans quelle monnaie on a payé.
	bare := Document{CurrencyCode: "GNF"}
	assert.Equal(t, "2 500 GNF", bare.Money(2500))
}

// ⚠️ UNE REMISE PORTE UN SIGNE MOINS, pas des parenthèses de comptable : un reçu
// se lit par quelqu'un qui n'en est pas un.
func TestADiscountReadsAsADiscount(t *testing.T) {
	d := Document{CurrencySymbol: "F"}
	assert.Equal(t, "-500 F", d.Money(-500))
}

// ⚠️ L'ESPACE DES MILLIERS EST INSÉCABLE : un montant coupé en fin de ligne se
// relit « 2 » puis « 500 ».
func TestThousandsNeverBreakAcrossALine(t *testing.T) {
	d := Document{CurrencySymbol: "F"}
	assert.Equal(t, "1 234 567 F", d.Money(1234567))
	assert.NotContains(t, d.Money(1234567), " 234", "une espace ordinaire se couperait")
}

// --- LES DISTANCES ET LES DURÉES ----------------------------------------

// ⚠️ SOUS UN KILOMÈTRE, ON ÉCRIT LES MÈTRES. « 0,4 km » pour une course de
// quatre cents mètres a l'air d'une erreur de saisie.
func TestShortDistancesStayInMetres(t *testing.T) {
	assert.Equal(t, "400 m", Km(400))
	assert.Equal(t, "8,2 km", Km(8234))
	assert.Equal(t, "1,0 km", Km(1000))
	// Zéro n'est pas « 0 km » : c'est une distance qu'on n'a pas.
	assert.Equal(t, "—", Km(0))
}

// ⚠️ PAS DE SECONDES SUR UN REÇU. Personne ne lit « 27 min 43 s », et cette
// précision prétend une exactitude que la mesure n'a pas : la fin d'une course
// est l'instant où le chauffeur appuie sur un bouton.
func TestDurationsRoundToTheMinute(t *testing.T) {
	assert.Equal(t, "28 min", Duration(1660))
	assert.Equal(t, "1 h 30", Duration(5400))
	assert.Equal(t, "—", Duration(0))
}

// ⚠️ L'HEURE EST CELLE DU PAYS DE L'OPÉRATION. Un reçu en UTC daterait une
// course de 23 h 30 à Lomé du lendemain — et c'est la première chose qu'on
// vérifie en contestant un reçu.
func TestStampsUseTheOperationTimezone(t *testing.T) {
	t.Setenv("TZ", "UTC")
	at := time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC)
	lome, err := time.LoadLocation("Africa/Lome")
	require.NoError(t, err)
	assert.Equal(t, "08/10/2026 23:30", Stamp(at, lome))

	dakarPlus := time.Date(2026, 10, 8, 1, 15, 0, 0, time.UTC)
	libreville, err := time.LoadLocation("Africa/Libreville") // UTC+1
	require.NoError(t, err)
	assert.Equal(t, "08/10/2026 02:15", Stamp(dakarPlus, libreville))
}

// --- LE RELEVÉ ----------------------------------------------------------

func TestAStatementRendersItsRowsAndTotals(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)
	d := Document{
		Kind: KindStatement, IssuedAt: to, PeriodFrom: from, PeriodTo: to,
		Country: "SN", CurrencyCode: "XOF", CurrencySymbol: "F CFA",
		Parties: []Party{{Role: "Client", Name: "Awa Ndiaye"}},
		RowCols: []Column{
			{Label: "Date", Width: 28}, {Label: "Trajet"},
			{Label: "Distance", Width: 22, Right: true},
		},
		Rows: []Row{
			{Cells: []string{"03/09/2026 08:12", "Almadies → Plateau", "6,1 km"}, Amount: 2250},
			{Cells: []string{"03/09/2026 19:40", "Plateau → Almadies", "6,4 km"}, Amount: 2400},
		},
		Summary: []Line{
			{Label: "Opérations", Detail: "2", NoAmount: true},
			{Label: "Distance totale", Detail: "12,5 km", NoAmount: true},
			{Label: "Total", Amount: 4650},
		},
	}
	// ⚠️ Un relevé n'additionne PAS ses lignes jusqu'à un total de document :
	// `Lines` est vide, et `Check` ne doit pas s'en plaindre. Le faire aurait
	// interdit le seul document qu'un client demande à son employeur.
	require.NoError(t, d.Check())
	out, err := Render(d, Options{Locale: "fr"})
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(out, []byte("%PDF-")))
}

// --- LA LANGUE ----------------------------------------------------------

// ⚠️ LE FRANÇAIS EN REPLI, ET NON L'ANGLAIS. Les cinq pays ouverts sont
// francophones : un reçu servi en anglais à Lomé parce qu'un en-tête manquait
// serait un document inutilisable pour celui qui le présente.
func TestAnUnknownLocaleFallsBackToFrench(t *testing.T) {
	assert.Equal(t, dict["fr"], lang("pt"))
	assert.Equal(t, dict["fr"], lang(""))
	assert.Equal(t, dict["en"], lang("en"))
	// Une étiquette complète — « fr-FR », « en-GB » — se ramène à sa langue.
	assert.Equal(t, dict["en"], lang("en-GB"))
	assert.Equal(t, dict["fr"], lang("fr-CA"))
}

// ⚠️ TOUS LES MOTS DU DOCUMENT DOIVENT SURVIVRE À LA CONVERSION CP1252. Les
// quatorze polices PDF de base ne connaissent pas l'UTF-8 : le rendu traduit, et
// ce qui n'a pas d'équivalent devient un POINT — silencieusement, au milieu d'un
// reçu. Les accents, les guillemets français et le tiret cadratin y sont tous ;
// un émoji, une flèche « → » ou une espace fine n'y sont pas.
//
// ⚠️ ET LE TEST PASSE PAR LE VRAI TRADUCTEUR, RUNE PAR RUNE. J'ai essayé deux
// raccourcis avant celui-ci, et les deux étaient faux : une borne sur les points
// de code accusait le tiret cadratin, qui est parfaitement représentable ; une
// comparaison de longueurs ne voyait rien, puisque le caractère perdu est
// REMPLACÉ et non supprimé. Un test qui crie à tort finit par être ignoré, et un
// test qui ne voit rien laisse passer exactement ce qu'il devait garder.
func TestEveryDocumentWordSurvivesTheCp1252Conversion(t *testing.T) {
	tr := fpdf.New("P", "mm", "A4", "").UnicodeTranslatorFromDescriptor("")
	for code, l := range dict {
		for _, s := range []string{
			l.receiptRide, l.receiptOrder, l.statement, l.issuedAt, l.number,
			l.operationOf, l.journey, l.distance, l.planned, l.duration,
			l.approach, l.source, l.tracked, l.estimated, l.pickup, l.stop,
			l.dest, l.detail, l.amount, l.total, l.payment, l.paid,
			l.pendingPayment, l.cash, l.period, l.operations, l.totalDistance,
			l.page, l.generatedBy,
		} {
			for _, r := range s {
				assert.True(t, printable(tr, r),
					"%s : %q contient %q, que la police de base ne sait pas imprimer",
					code, s, r)
			}
		}
	}
}

// printable dit si une rune traverse la conversion sans être abîmée.
//
// Le traducteur travaille rune par rune et rend un POINT pour ce qu'il ne
// connaît pas : un point qui n'était pas un point est donc la signature d'un
// caractère perdu, et une longueur qui change l'est aussi.
func printable(tr func(string) string, r rune) bool {
	out := tr(string(r))
	if len(out) != 1 {
		return false
	}
	return out != "." || r == '.'
}

// Et le contrôle du contrôle : le traducteur abîme bien ce qu'il ne sait pas
// imprimer. Sans cette vérification, le test ci-dessus passerait même si `tr`
// rendait tout tel quel — et ne garderait plus rien.
func TestTheConversionReallyMarksWhatItCannotPrint(t *testing.T) {
	tr := fpdf.New("P", "mm", "A4", "").UnicodeTranslatorFromDescriptor("")
	// Ce qu'on serait tenté de mettre sur un reçu, et qui ne s'imprimerait pas.
	for _, r := range []rune{'\U0001F698', '\u2192', '\u202f', '\u2713'} {
		assert.False(t, printable(tr, r), "%q devrait être signalé", r)
	}
	// Ce dont un document français a besoin, et qui passe.
	for _, r := range []rune{'é', 'è', 'à', 'ç', 'ô', '«', '»', '\u2014', '°', '\u00a0', '.'} {
		assert.True(t, printable(tr, r), "%q doit passer", r)
	}
}
