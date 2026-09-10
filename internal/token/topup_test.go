package token

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// --- Achat POUR un point de vente (console) --------------------------------

// La console vise un PORTEFEUILLE, pas un rôle. Les deux types doivent
// fonctionner — et surtout le livreur, qu'une route indexée sur le point de
// vente aurait laissé de côté alors qu'il achète des jetons lui aussi.
func TestAdminPurchaseWorksForBothWalletTypes(t *testing.T) {
	for _, walletType := range []string{WalletTypeMerchant, WalletTypeDriver} {
		t.Run(walletType, func(t *testing.T) {
			repo := newFakeRepo()
			svc, payments, _, _ := newTestService(repo)
			ownerID := seedWallet(t, repo, walletType, 0)

			resp, err := svc.PurchaseForOwner(context.Background(), "admin-user", ownerID.Hex(), 25)
			require.NoError(t, err)

			assert.Equal(t, ownerID.Hex(), payments.lastOwnerID, "le paiement vise le portefeuille demandé")
			assert.Equal(t, "admin-user", payments.lastUserID, "le payeur reste l'utilisateur qui agit")
			assert.Equal(t, 25, resp.Tokens)
			assert.Equal(t, 2500, resp.AmountXOF, "25 jetons à 100 F")

			// Le solde ne bouge PAS : l'achat n'est qu'une intention tant que
			// le prestataire n'a pas confirmé l'encaissement.
			assert.Zero(t, repo.balanceOf(ownerID), "un achat initié ne crédite rien")
		})
	}
}

// Acheter pour un portefeuille inexistant créerait un paiement que rien ne
// pourrait créditer à son encaissement.
func TestAdminPurchaseRefusesAnUnknownWallet(t *testing.T) {
	repo := newFakeRepo()
	svc, payments, _, _ := newTestService(repo)

	_, err := svc.PurchaseForOwner(context.Background(), "admin-user", primitive.NewObjectID().Hex(), 10)
	require.Error(t, err)
	assert.Empty(t, payments.lastOwnerID, "aucun paiement ne doit être initié")
}

func TestAdminPurchaseRejectsNonPositiveAmounts(t *testing.T) {
	repo := newFakeRepo()
	svc, payments, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeMerchant, 0)

	for _, tokens := range []int{0, -5} {
		_, err := svc.PurchaseForOwner(context.Background(), "admin-user", ownerID.Hex(), tokens)
		assertCode(t, err, "validation_failed")
	}
	assert.Empty(t, payments.lastOwnerID)
}

// L'ouverture faite à l'admin sur le parcours marchand reste conditionnée à
// un point de vente nommé : il n'y a pas de portefeuille d'administrateur.
func TestPurchaseAsAdminStillRequiresAStore(t *testing.T) {
	svc, _, _, _ := newTestService(newFakeRepo())
	_, err := svc.Purchase(context.Background(), "admin-user", auth.RoleAdmin, PurchaseRequest{Tokens: 10})
	assertCode(t, err, "validation_failed")
}

// Un CLIENT possède un portefeuille — d'ARGENT — mais n'achète pas de jetons :
// ils sont le droit d'entrée d'un livreur et l'outil de promotion d'un
// marchand. Ouvrir la lecture du portefeuille au client ne doit pas ouvrir la
// vente de jetons avec elle.
func TestPurchaseStillRejectsRolesWithoutWallet(t *testing.T) {
	svc, _, _, _ := newTestService(newFakeRepo())
	_, err := svc.Purchase(context.Background(), "u1", auth.RoleClient, PurchaseRequest{Tokens: 10})
	assertCode(t, err, "forbidden")
}

// --- Crédit opérateur ------------------------------------------------------

func TestOperatorCreditAddsTokensAndReturnsTheStoredBalance(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeMerchant, 40)

	wallet, err := svc.CreditByOperator(context.Background(), "admin-user", ownerID.Hex(), 60, "geste commercial, commande #A12 livrée froide")
	require.NoError(t, err)

	assert.Equal(t, 100, wallet.Balance, "le solde renvoyé est relu du compte")
	assert.Equal(t, 100, repo.balanceOf(ownerID))
}

// Le mouvement doit être traçable dans le grand livre, et distinguable d'un
// achat réel : ces jetons n'ont pas de contrepartie financière.
func TestOperatorCreditIsRecordedWithItsOwnReason(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 0)

	_, err := svc.CreditByOperator(context.Background(), "admin-user", ownerID.Hex(), 10, "compensation panne applicative")
	require.NoError(t, err)

	require.Equal(t, 1, repo.transactionCount())
	tx := repo.transactions[0]
	assert.Equal(t, ReasonOperatorCredit, tx.Reason,
		"un crédit opérateur ne doit pas se confondre avec un achat dans le grand livre")
	assert.Equal(t, KindPurchase, tx.Kind)
	assert.Equal(t, 10, tx.Amount)
}

// Un crédit sans motif est indéfendable en revue de comptes.
func TestOperatorCreditRequiresAJustification(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeMerchant, 0)

	for _, justification := range []string{"", "   ", strings.Repeat("x", maxJustificationLen+1)} {
		_, err := svc.CreditByOperator(context.Background(), "admin-user", ownerID.Hex(), 10, justification)
		assertCode(t, err, "validation_failed")
	}
	assert.Zero(t, repo.balanceOf(ownerID), "aucun crédit ne doit passer sans motif recevable")
	assert.Zero(t, repo.transactionCount())
}

// Créditer un montant nul ou négatif retirerait de l'argent par une route qui
// prétend en ajouter.
func TestOperatorCreditRejectsNonPositiveAmounts(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeMerchant, 50)

	for _, amount := range []int{0, -10} {
		_, err := svc.CreditByOperator(context.Background(), "admin-user", ownerID.Hex(), amount, "motif valable")
		assertCode(t, err, "validation_failed")
	}
	assert.Equal(t, 50, repo.balanceOf(ownerID))
}

// Créditer un propriétaire sans portefeuille créerait un solde que personne ne
// réclamerait.
func TestOperatorCreditRefusesAnUnknownWallet(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)

	_, err := svc.CreditByOperator(context.Background(), "admin-user", primitive.NewObjectID().Hex(), 10, "motif valable")
	require.Error(t, err)
	assert.Zero(t, repo.transactionCount())
}

// Le prix unitaire doit voyager avec le solde : sans lui, chaque application
// recopierait 100 F et dériverait à la première décision commerciale.
func TestWalletCarriesTheUnitPrice(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, stores := newTestService(repo)
	storeID := seedWallet(t, repo, WalletTypeMerchant, 12)
	stores.owned[storeID.Hex()] = "merchant-user"

	wallet, err := svc.GetWallet(context.Background(), "merchant-user", auth.RoleMerchant, storeID.Hex())
	require.NoError(t, err)
	assert.Equal(t, 12, wallet.Balance)
	assert.Equal(t, 100, wallet.TokenPriceXOF, "le prix rendu doit être celui du service, pas une constante du client")

	// Et il doit rester cohérent avec ce que l'achat facture réellement.
	resp, err := svc.Purchase(context.Background(), "merchant-user", auth.RoleMerchant,
		PurchaseRequest{Tokens: 7, StoreID: storeID.Hex()})
	require.NoError(t, err)
	assert.Equal(t, 7*wallet.TokenPriceXOF, resp.AmountXOF,
		"le montant annoncé au client doit être celui qui est facturé")
}
