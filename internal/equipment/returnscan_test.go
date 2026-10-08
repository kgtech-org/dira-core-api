package equipment

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// LE RETOUR PROUVÉ PAR UN SCAN — et ce qu'il prouve, qui n'est pas ce que
// prouve la remise.

// rented prépare une location vivante avec caution PAYÉE, et rend le contrat.
//
// ⚠️ LA CAUTION DOIT ÊTRE PAYÉE, sinon il n'y a rien à rendre et les tests ne
// mesureraient rien — un retour « sans rien verser » passerait au vert pour la
// mauvaise raison.
func rented(t *testing.T, svc *Service, purse *memPurse, ctx context.Context) *ContractResponse {
	t.Helper()
	purse.balance[courierID] = 20_000
	it := vest(t, svc, ctx)
	c, err := svc.CreateContract(ctx, "admin", ContractInput{
		UserID: courierID, Vertical: VerticalFood, ItemID: it.ID, Mode: ModeRental, HandOverNow: true,
		Plan: &Plan{Period: PeriodWeekly, CollectFromWallet: true, DepositRefundable: true},
	})
	require.NoError(t, err)
	require.Equal(t, StatusActive, c.Status)
	// `CollectFromWallet` prélève déjà ce qui est dû à la remise ; s'il reste
	// quelque chose, on le solde.
	if c.DueXOF > 0 {
		_, err = svc.Pay(ctx, courierID, c.ID, PayInput{AmountXOF: c.DueXOF})
		require.NoError(t, err)
	}
	got, err := svc.GetContract(ctx, c.ID)
	require.NoError(t, err)
	require.Positive(t, paidDeposit(got), "la caution est bien encaissée")
	return got
}

func paidDeposit(c *ContractResponse) int {
	n := 0
	for _, l := range c.Schedule {
		if l.Kind == LineDeposit {
			n += l.PaidXOF
		}
	}
	return n
}

// ⚠️⚠️ LE TEST QUI PORTE TOUT LE GESTE : LE QR AFFICHE CE QUE LE PORTEUR
// ACCEPTE. Un scan qui n'aurait prouvé qu'« il était là » laissait entière la
// seule chose qui se conteste vraiment — le montant retenu sur la caution. Le
// constat part donc AVEC le code, et le montant annoncé sous le QR doit être
// exactement celui qui sera versé.
func TestTheReturnQrShowsExactlyWhatWillBeRefunded(t *testing.T) {
	svc, _, purse, _, _, ctx := desk(t)
	c := rented(t, svc, purse, ctx)
	deposit := paidDeposit(c)

	qr, err := svc.MintReturnCode(ctx, "admin", c.ID, ReturnInput{
		Condition: "déchirure à l'épaule", DamageFeeXOF: 500,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, qr.Code)
	assert.Equal(t, c.ID, qr.ContractID)
	assert.Equal(t, "Koffi", qr.HolderName, "le comptoir doit pouvoir vérifier son écran")
	assert.Contains(t, qr.URL, qr.Code)
	// Ce que le porteur LIT avant de scanner.
	assert.Equal(t, "déchirure à l'épaule", qr.Condition)
	assert.Equal(t, 500, qr.DamageFeeXOF)
	assert.True(t, qr.RefundsDeposit)
	assert.Equal(t, deposit-500, qr.RefundXOF)

	// ⚠️ ET RIEN N'A ENCORE BOUGÉ : un code qui expire sans être scanné ne doit
	// laisser aucune trace sur l'argent. C'est la différence entre « voici ce
	// que nous allons retenir » et « nous avons retenu ».
	pending, err := svc.GetContract(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, pending.Status)
	assert.Zero(t, pending.DamageFeeXOF)
	assert.Empty(t, pending.ReturnCondition)
	assert.Empty(t, pending.ReturnedVia)
}

// Le scan applique le constat, et rien d'autre.
func TestTheScanAppliesTheCounterSAssessment(t *testing.T) {
	svc, store, purse, _, _, ctx := desk(t)
	c := rented(t, svc, purse, ctx)
	deposit := paidDeposit(c)
	before := purse.balance[courierID]

	qr, err := svc.MintReturnCode(ctx, "admin", c.ID, ReturnInput{
		Condition: "déchirure à l'épaule", DamageFeeXOF: 500,
	})
	require.NoError(t, err)

	out, err := svc.ScanReturn(ctx, courierID, qr.Code)
	require.NoError(t, err)
	assert.Equal(t, StatusReturned, out.Status)
	assert.Equal(t, "déchirure à l'épaule", out.ReturnCondition)
	assert.Equal(t, 500, out.DamageFeeXOF)
	// ⚠️ LA PREUVE EST ENREGISTRÉE : c'est ce champ qu'on regarde quand une
	// caution est contestée.
	assert.Equal(t, ReturnedViaScan, out.ReturnedVia)
	// LE MONTANT ANNONCÉ EST LE MONTANT VERSÉ.
	assert.Equal(t, qr.RefundXOF, purse.balance[courierID]-before,
		"afficher 5 000 sous le QR et verser 4 000 serait la réclamation que ce geste éteint")
	assert.Equal(t, deposit-500, purse.balance[courierID]-before)
	// Et le stock est revenu, une seule fois.
	for _, it := range store.items {
		assert.Equal(t, 3, it.Stock)
	}
}

// ⚠️ LE PORTEUR ACCEPTE, IL NE NÉGOCIE PAS. Si le scan relisait un constat
// envoyé par l'application, n'importe qui pourrait se déclarer « bon état, 0 F
// de dégâts » en scannant son propre QR — et le geste censé protéger
// l'exploitation l'aurait désarmée.
func TestTheHolderCannotRewriteTheAssessmentWhileScanning(t *testing.T) {
	svc, _, purse, _, _, ctx := desk(t)
	c := rented(t, svc, purse, ctx)
	deposit := paidDeposit(c)
	before := purse.balance[courierID]

	qr, err := svc.MintReturnCode(ctx, "admin", c.ID, ReturnInput{
		Condition: "brûlure", DamageFeeXOF: 1_000,
	})
	require.NoError(t, err)

	// `ScanReturn` ne prend QUE le code : il n'y a pas d'endroit où glisser un
	// constat. C'est une garantie de signature, et ce test la fige.
	out, err := svc.ScanReturn(ctx, courierID, qr.Code)
	require.NoError(t, err)
	assert.Equal(t, 1_000, out.DamageFeeXOF, "les dégâts constatés au comptoir")
	assert.Equal(t, deposit-1_000, purse.balance[courierID]-before)
}

// ⚠️ LA VOIE DU COMPTOIR RESTE OUVERTE, et elle DIT qu'elle est la voie du
// comptoir : un comptoir sans réseau, un téléphone sans caméra, un sac renvoyé
// par un collègue. Mais un retour sans acceptation ne doit pas se lire comme un
// retour accepté.
func TestTheStaffPathStaysOpenAndSaysSo(t *testing.T) {
	svc, _, purse, _, _, ctx := desk(t)
	c := rented(t, svc, purse, ctx)

	out, err := svc.Return(ctx, "admin", c.ID, ReturnInput{Condition: "bon état"})
	require.NoError(t, err)
	assert.Equal(t, StatusReturned, out.Status)
	assert.Equal(t, ReturnedViaStaff, out.ReturnedVia)
}

// ⚠️ DEUX SCANS SIMULTANÉS NE RENDENT PAS DEUX CAUTIONS. Le code est consommé
// en une seule écriture : le second appel ne trouve plus rien, et rend le
// contrat tel qu'il est plutôt qu'une erreur — la personne a scanné, c'est fait.
func TestASecondScanRefundsNothingMore(t *testing.T) {
	svc, _, purse, _, _, ctx := desk(t)
	c := rented(t, svc, purse, ctx)
	before := purse.balance[courierID]

	qr, err := svc.MintReturnCode(ctx, "admin", c.ID, ReturnInput{Condition: "bon état"})
	require.NoError(t, err)
	_, err = svc.ScanReturn(ctx, courierID, qr.Code)
	require.NoError(t, err)
	once := purse.balance[courierID] - before

	_, err = svc.ScanReturn(ctx, courierID, qr.Code)
	require.Error(t, err, "le code est brûlé")
	assert.Equal(t, once, purse.balance[courierID]-before, "pas deux cautions")
}

// ⚠️ TROIS REFUS, TROIS GESTES — comme pour la remise. « Ce n'est pas le vôtre »
// et « demandez un nouveau code » n'envoient pas la même personne au même
// endroit, et un seul message « code invalide » les enverrait tous au comptoir.
func TestTheThreeRefusalsStaySeparate(t *testing.T) {
	svc, _, purse, _, _, ctx := desk(t)
	c := rented(t, svc, purse, ctx)
	qr, err := svc.MintReturnCode(ctx, "admin", c.ID, ReturnInput{})
	require.NoError(t, err)

	// Le code de quelqu'un d'autre : « regardez le bon écran ».
	_, err = svc.ScanReturn(ctx, driverID, qr.Code)
	require.Error(t, err)
	assert.Equal(t, "equipment_return_not_yours", apperr.From(err).Code)

	// Expiré : « demandez-en un nouveau ».
	svc.advance(handoverCodeTTL + time.Minute)
	_, err = svc.ScanReturn(ctx, courierID, qr.Code)
	require.Error(t, err)
	assert.Equal(t, "equipment_return_code_expired", apperr.From(err).Code)

	// Inconnu.
	_, err = svc.ScanReturn(ctx, courierID, "ZZZZZZZZZZZZZZZZ")
	require.Error(t, err)
	assert.Equal(t, "equipment_return_code_unknown", apperr.From(err).Code)
}

// ⚠️ UN CODE DE REMISE NE REND RIEN, ET UN CODE DE RETOUR NE REMET RIEN. Les
// deux gestes ont deux champs séparés ; un seul champ réutilisé aurait laissé un
// code de remise oublié rendre le gilet qu'il venait de remettre.
func TestAHandoverCodeCannotConcludeAReturn(t *testing.T) {
	svc, _, _, _, _, ctx := desk(t)
	c, _ := draftContract(t, svc, ctx)
	qr, err := svc.MintHandoverCode(ctx, "admin", c.ID)
	require.NoError(t, err)

	_, err = svc.ScanReturn(ctx, courierID, qr.Code)
	require.Error(t, err, "un code de remise n'est pas un code de retour")
	assert.Equal(t, "equipment_return_code_unknown", apperr.From(err).Code)
}

// On n'affiche pas un QR de retour pour un contrat qui n'est pas à rendre : le
// porteur scannerait pour rien, et c'est au comptoir, devant lui, qu'il faut
// l'apprendre.
func TestNoReturnCodeForAContractThatIsNotOut(t *testing.T) {
	svc, _, _, _, _, ctx := desk(t)
	c, _ := draftContract(t, svc, ctx)
	_, err := svc.MintReturnCode(ctx, "admin", c.ID, ReturnInput{})
	assert.Error(t, err, "rien n'est dehors : il n'y a rien à rendre")
}
