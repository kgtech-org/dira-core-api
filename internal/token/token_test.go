package token

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// fakeRepo reproduces the MongoDB contract semantics in memory: the
// conditional consume returns matched=false when balance < amount, and the
// unique index on owner_id triggers ErrDuplicateWallet.
type fakeRepo struct {
	mu           sync.Mutex
	wallets      map[primitive.ObjectID]*Wallet // by wallet id
	byOwner      map[primitive.ObjectID]primitive.ObjectID
	transactions []Transaction
	operations   map[string]bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		wallets:    make(map[primitive.ObjectID]*Wallet),
		byOwner:    make(map[primitive.ObjectID]primitive.ObjectID),
		operations: make(map[string]bool),
	}
}

// ClaimOperation REPRODUIT l'index unique : la seconde réservation de la même
// clé échoue. Un faux qui accepterait tout ferait passer au vert la garde
// d'idempotence sans qu'elle garantisse quoi que ce soit.
func (f *fakeRepo) ClaimOperation(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.operations[key] {
		return errOperationApplied
	}
	f.operations[key] = true
	return nil
}

func (f *fakeRepo) CreateWallet(_ context.Context, w *Wallet) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byOwner[w.OwnerID]; exists {
		return ErrDuplicateWallet
	}
	w.ID = primitive.NewObjectID()
	clone := *w
	f.wallets[w.ID] = &clone
	f.byOwner[w.OwnerID] = w.ID
	return nil
}

func (f *fakeRepo) FindWalletByOwner(_ context.Context, ownerID primitive.ObjectID) (*Wallet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.byOwner[ownerID]
	if !ok {
		return nil, nil
	}
	clone := *f.wallets[id]
	return &clone, nil
}

func (f *fakeRepo) ConsumeAtomic(_ context.Context, walletID primitive.ObjectID, amount int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wallets[walletID]
	if !ok || w.Balance < amount {
		return false, nil // matchedCount == 0
	}
	w.Balance -= amount
	w.UpdatedAt = time.Now().UTC()
	return true, nil
}

func (f *fakeRepo) Credit(_ context.Context, walletID primitive.ObjectID, amount int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wallets[walletID]
	if !ok {
		return errors.New("wallet not found")
	}
	w.Balance += amount
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// Le faux dépôt reproduit le vrai : le champ est nommé, et un montant négatif
// est légitime — c'est ainsi qu'une dette se solde.
func (f *fakeRepo) CreditField(_ context.Context, walletID primitive.ObjectID, field string, amount int) error {
	w, ok := f.wallets[walletID]
	if !ok {
		return errors.New("wallet not found")
	}
	switch field {
	case "balance":
		w.Balance += amount
	case "balance_xof":
		w.BalanceXOF += amount
	case "promo_xof":
		w.PromoXOF += amount
	default:
		return errors.New("unknown field " + field)
	}
	return nil
}

// SpendMoney reproduit le pipeline atomique du vrai dépôt : la condition
// porte sur la SOMME des deux postes, et le PROMOTIONNEL part en premier. Un
// faux qui débiterait l'argent d'abord laisserait passer un ordre de priorité
// inverse sans qu'aucun test ne le voie.
func (f *fakeRepo) SpendMoney(_ context.Context, walletID primitive.ObjectID, amount int) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wallets[walletID]
	if !ok {
		return 0, 0, errors.New("wallet not found")
	}
	if w.BalanceXOF+w.PromoXOF < amount {
		return 0, 0, ErrInsufficientFunds
	}
	fromPromo := w.PromoXOF
	if fromPromo > amount {
		fromPromo = amount
	}
	w.PromoXOF -= fromPromo
	w.BalanceXOF -= amount - fromPromo
	return fromPromo, amount - fromPromo, nil
}

// TakeMoney : l'argent réel seulement, tout ou ce qu'il y a.
func (f *fakeRepo) TakeMoney(_ context.Context, walletID primitive.ObjectID, amount int, partial bool) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wallets[walletID]
	if !ok {
		return 0, errors.New("wallet not found")
	}
	if w.BalanceXOF >= amount {
		w.BalanceXOF -= amount
		return amount, nil
	}
	if !partial || w.BalanceXOF <= 0 {
		return 0, nil
	}
	taken := w.BalanceXOF
	w.BalanceXOF = 0
	return taken, nil
}

func (f *fakeRepo) AdjustDebt(_ context.Context, walletID primitive.ObjectID, delta int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wallets[walletID]
	if !ok || w.DebtXOF+delta < 0 {
		return errors.New("wallet not found or debt below zero")
	}
	w.DebtXOF += delta
	return nil
}

func (f *fakeRepo) InsertTransaction(_ context.Context, t *Transaction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.ID = primitive.NewObjectID()
	f.transactions = append(f.transactions, *t)
	return nil
}

func (f *fakeRepo) ListTransactions(_ context.Context, walletID primitive.ObjectID, limit int, cursor string) ([]Transaction, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []Transaction
	for _, t := range f.transactions {
		if t.WalletID == walletID && (cursor == "" || t.ID.Hex() > cursor) {
			items = append(items, t)
		}
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID.Hex()
	}
	return items, next, nil
}

// ListTransactionsNewest REPRODUIT le vrai : du plus récent au plus ancien.
func (f *fakeRepo) ListTransactionsNewest(_ context.Context, walletID primitive.ObjectID, limit int, cursor string) ([]Transaction, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []Transaction
	for i := len(f.transactions) - 1; i >= 0; i-- {
		t := f.transactions[i]
		if t.WalletID == walletID && (cursor == "" || t.ID.Hex() < cursor) {
			items = append(items, t)
		}
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID.Hex()
	}
	return items, next, nil
}

// WithTransaction reproduit le SEUL effet de l'annulation dont un test
// dépend : une clé réservée dans une transaction qui échoue redevient libre.
func (f *fakeRepo) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	f.mu.Lock()
	before := make(map[string]bool, len(f.operations))
	for k, v := range f.operations {
		before[k] = v
	}
	f.mu.Unlock()
	err := fn(ctx)
	if err != nil {
		f.mu.Lock()
		f.operations = before
		f.mu.Unlock()
	}
	return err
}

func (f *fakeRepo) balanceOf(ownerID primitive.ObjectID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wallets[f.byOwner[ownerID]].Balance
}

func (f *fakeRepo) transactionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.transactions)
}

type fakeBooster struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeBooster) SetBoosted(_ context.Context, dishID string, boosted bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if boosted {
		f.calls = append(f.calls, dishID)
	}
	return nil
}

type fakeStores struct {
	owned map[string]string // storeID -> ownerUserID
}

func (f *fakeStores) OwnsStore(_ context.Context, userID, storeID string) (bool, error) {
	return f.owned[storeID] == userID, nil
}

type fakePayments struct {
	lastUserID  string
	lastOwnerID string
	lastTokens  int
	lastXOF     int
}

func (f *fakePayments) InitiatePurchase(_ context.Context, userID, walletOwnerID string, tokens, amountXOF int) (string, error) {
	f.lastUserID, f.lastOwnerID, f.lastTokens, f.lastXOF = userID, walletOwnerID, tokens, amountXOF
	return "pay_123", nil
}

func newTestService(repo *fakeRepo) (*Service, *fakePayments, *fakeBooster, *fakeStores) {
	payments := &fakePayments{}
	booster := &fakeBooster{}
	stores := &fakeStores{owned: map[string]string{}}
	svc := NewService(repo, nil, payments, booster, stores, 100, 5, map[string]int{"featured_listing": 10})
	return svc, payments, booster, stores
}

func seedWallet(t *testing.T, repo *fakeRepo, walletType string, balance int) primitive.ObjectID {
	t.Helper()
	ownerID := primitive.NewObjectID()
	w := &Wallet{OwnerID: ownerID, Type: walletType, Balance: balance, UpdatedAt: time.Now().UTC()}
	require.NoError(t, repo.CreateWallet(context.Background(), w))
	return ownerID
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, code, apperr.From(err).Code)
}

func TestCreateWalletIdempotent(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := primitive.NewObjectID().Hex()

	require.NoError(t, svc.CreateWallet(context.Background(), ownerID, WalletTypeDriver))
	require.NoError(t, svc.CreateWallet(context.Background(), ownerID, WalletTypeDriver), "duplicate owner must not fail")
	assert.Error(t, svc.CreateWallet(context.Background(), ownerID, "unknown"), "invalid wallet type must fail")
}

func TestConsumeConcurrentDebit(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 1)

	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for range 2 {
		go func() {
			start.Wait()
			results <- svc.Consume(context.Background(), ownerID.Hex(), 1, ReasonOrderAccept, "", "", nil)
		}()
	}
	start.Done()

	var successes, insufficient int
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else {
			assertCode(t, err, "insufficient_tokens")
			insufficient++
		}
	}
	assert.Equal(t, 1, successes, "exactly one consume must succeed")
	assert.Equal(t, 1, insufficient, "exactly one consume must fail with insufficient_tokens")
	assert.Equal(t, 0, repo.balanceOf(ownerID), "balance must be 0, never negative")
	assert.Equal(t, 1, repo.transactionCount(), "only the winning debit records a transaction")
}

func TestConsumeBalanceNeverNegative(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 3)

	err := svc.Consume(context.Background(), ownerID.Hex(), 5, ReasonOrderAccept, "", "", nil)
	assertCode(t, err, "insufficient_tokens")
	assert.Equal(t, 3, repo.balanceOf(ownerID), "failed debit must not change the balance")
	assert.Equal(t, 0, repo.transactionCount(), "failed debit must not record a transaction")
}

func TestConsumeOrderAcceptRecordsOrderID(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 2)
	orderID := primitive.NewObjectID()

	require.NoError(t, svc.Consume(context.Background(), ownerID.Hex(), 1, ReasonOrderAccept, RefOrder, orderID.Hex(), nil))

	assert.Equal(t, 1, repo.balanceOf(ownerID), "order accept consumes exactly 1 token")
	require.Equal(t, 1, repo.transactionCount())
	tx := repo.transactions[0]
	assert.Equal(t, KindConsume, tx.Kind)
	assert.Equal(t, ReasonOrderAccept, tx.Reason)
	assert.Equal(t, -1, tx.Amount)
	require.NotNil(t, tx.RefID, "la référence doit être écrite sur order_accept")
	assert.Equal(t, orderID, *tx.RefID)
	// Le GENRE aussi : sans lui, une écriture de course et une écriture de
	// commande seraient indiscernables au grand livre.
	assert.Equal(t, RefOrder, tx.RefKind)
}

func TestConsumeWalletNotFound(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	err := svc.Consume(context.Background(), primitive.NewObjectID().Hex(), 1, ReasonOrderAccept, "", "", nil)
	assertCode(t, err, "wallet_not_found")
}

func TestCreditAfterPurchase(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 0)

	require.NoError(t, svc.Credit(context.Background(), ownerID.Hex(), 10, ReasonTopup))

	assert.Equal(t, 10, repo.balanceOf(ownerID))
	require.Equal(t, 1, repo.transactionCount())
	tx := repo.transactions[0]
	assert.Equal(t, KindPurchase, tx.Kind)
	assert.Equal(t, ReasonTopup, tx.Reason)
	assert.Equal(t, 10, tx.Amount)
}

func TestPurchaseDriver(t *testing.T) {
	repo := newFakeRepo()
	svc, payments, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 0)

	resp, err := svc.Purchase(context.Background(), ownerID.Hex(), auth.RoleDriver, PurchaseRequest{Tokens: 7})
	require.NoError(t, err)
	assert.Equal(t, "pay_123", resp.PaymentID)
	assert.Equal(t, 700, resp.AmountXOF, "7 tokens at 100 XOF each")
	assert.Equal(t, ownerID.Hex(), payments.lastOwnerID)
	assert.Equal(t, 7, payments.lastTokens)
	assert.Equal(t, 0, repo.balanceOf(ownerID), "credit only happens after payment confirmation")
}

func TestPurchaseMerchantRequiresOwnedStore(t *testing.T) {
	repo := newFakeRepo()
	svc, payments, _, stores := newTestService(repo)
	storeID := seedWallet(t, repo, WalletTypeMerchant, 0)
	userID := primitive.NewObjectID().Hex()

	// store_id missing
	_, err := svc.Purchase(context.Background(), userID, auth.RoleMerchant, PurchaseRequest{Tokens: 2})
	assertCode(t, err, "validation_failed")

	// not the owner
	_, err = svc.Purchase(context.Background(), userID, auth.RoleMerchant, PurchaseRequest{Tokens: 2, StoreID: storeID.Hex()})
	assertCode(t, err, "forbidden")

	// owner
	stores.owned[storeID.Hex()] = userID
	resp, err := svc.Purchase(context.Background(), userID, auth.RoleMerchant, PurchaseRequest{Tokens: 2, StoreID: storeID.Hex()})
	require.NoError(t, err)
	assert.Equal(t, 200, resp.AmountXOF)
	assert.Equal(t, storeID.Hex(), payments.lastOwnerID)
	assert.Equal(t, userID, payments.lastUserID)
}

func TestGetWallet(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, stores := newTestService(repo)

	driverID := seedWallet(t, repo, WalletTypeDriver, 4)
	got, err := svc.GetWallet(context.Background(), driverID.Hex(), auth.RoleDriver, "")
	require.NoError(t, err)
	assert.Equal(t, 4, got.Balance)
	assert.Equal(t, WalletTypeDriver, got.Type)

	storeID := seedWallet(t, repo, WalletTypeMerchant, 9)
	merchantID := primitive.NewObjectID().Hex()
	_, err = svc.GetWallet(context.Background(), merchantID, auth.RoleMerchant, storeID.Hex())
	assertCode(t, err, "forbidden")

	stores.owned[storeID.Hex()] = merchantID
	got, err = svc.GetWallet(context.Background(), merchantID, auth.RoleMerchant, storeID.Hex())
	require.NoError(t, err)
	assert.Equal(t, 9, got.Balance)
}

func TestListTransactionsPaginated(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 10)

	for range 3 {
		require.NoError(t, svc.Consume(context.Background(), ownerID.Hex(), 1, ReasonOrderAccept, "", "", nil))
	}

	page1, next, err := svc.ListTransactions(context.Background(), ownerID.Hex(), auth.RoleDriver, "", httpx.Page{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	require.NotEmpty(t, next)

	page2, next2, err := svc.ListTransactions(context.Background(), ownerID.Hex(), auth.RoleDriver, "", httpx.Page{Limit: 2, Cursor: next})
	require.NoError(t, err)
	assert.Len(t, page2, 1)
	assert.Empty(t, next2)
}

func TestBoostDish(t *testing.T) {
	repo := newFakeRepo()
	svc, _, booster, stores := newTestService(repo)
	storeID := seedWallet(t, repo, WalletTypeMerchant, 6)
	userID := primitive.NewObjectID().Hex()
	stores.owned[storeID.Hex()] = userID

	resp, err := svc.BoostDish(context.Background(), userID, "", storeID.Hex(), "dish1")
	require.NoError(t, err)
	assert.Equal(t, 5, resp.Cost)
	assert.Equal(t, 1, repo.balanceOf(storeID), "5 tokens consumed")
	assert.Equal(t, []string{"dish1"}, booster.calls)

	// insufficient balance: booster must not be called again
	_, err = svc.BoostDish(context.Background(), userID, "", storeID.Hex(), "dish2")
	assertCode(t, err, "insufficient_tokens")
	assert.Equal(t, []string{"dish1"}, booster.calls, "booster not called on failed debit")
	assert.Equal(t, 1, repo.balanceOf(storeID), "balance unchanged on failed debit")
}

func TestBoostDishRefundsWhenBoosterFails(t *testing.T) {
	repo := newFakeRepo()
	svc, _, booster, stores := newTestService(repo)
	storeID := seedWallet(t, repo, WalletTypeMerchant, 5)
	userID := primitive.NewObjectID().Hex()
	stores.owned[storeID.Hex()] = userID
	booster.err = errors.New("dish service down")

	_, err := svc.BoostDish(context.Background(), userID, "", storeID.Hex(), "dish1")
	require.Error(t, err)
	assert.Equal(t, 5, repo.balanceOf(storeID), "tokens must be refunded when the boost fails")
}

func TestBuyOption(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, stores := newTestService(repo)
	storeID := seedWallet(t, repo, WalletTypeMerchant, 15)
	userID := primitive.NewObjectID().Hex()
	stores.owned[storeID.Hex()] = userID

	resp, err := svc.BuyOption(context.Background(), userID, storeID.Hex(), "featured_listing")
	require.NoError(t, err)
	assert.Equal(t, 10, resp.Cost)
	assert.Equal(t, 5, repo.balanceOf(storeID))

	_, err = svc.BuyOption(context.Background(), userID, storeID.Hex(), "nope")
	assertCode(t, err, "option_not_found")
}

// --- idempotence des mouvements d'argent ---
//
// Ces tests existent à cause de l'extraction : `PayOrder` était un appel de
// fonction, c'est devenu un appel HTTP. Une réponse perdue — le débit
// appliqué, la réponse jamais arrivée — laisse l'appelant sans nouvelle. S'il
// réessaie, il débite deux fois. Le réseau ne dit pas ce qui s'est passé ;
// c'est au socle de rendre le réessai inoffensif.

func seedMoneyWallet(t *testing.T, repo *fakeRepo, cash, promo int) string {
	t.Helper()
	ownerID := primitive.NewObjectID()
	w := &Wallet{
		OwnerID: ownerID, Type: WalletTypeClient,
		BalanceXOF: cash, PromoXOF: promo, UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.CreateWallet(context.Background(), w))
	return ownerID.Hex()
}

func TestPayOrderReplayDoesNotDebitTwice(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := seedMoneyWallet(t, repo, 10_000, 0)
	orderID := primitive.NewObjectID().Hex()

	require.NoError(t, svc.Pay(context.Background(), owner, 3_000, RefOrder, orderID))
	// Le réessai doit RÉUSSIR — l'appelant voulait que l'argent bouge, il a
	// bougé — sans rien débiter de plus.
	require.NoError(t, svc.Pay(context.Background(), owner, 3_000, RefOrder, orderID))

	oid, err := primitive.ObjectIDFromHex(owner)
	require.NoError(t, err)
	repo.mu.Lock()
	balance := repo.wallets[repo.byOwner[oid]].BalanceXOF
	repo.mu.Unlock()
	assert.Equal(t, 7_000, balance, "le second appel ne doit rien débiter")
	assert.Equal(t, 1, repo.transactionCount(), "une seule écriture au grand livre")
}

// Le piège que la seule unicité (portefeuille, commande, motif) n'attrapait
// pas : le premier paiement épuise le promotionnel, le réessai puise dans
// l'argent réel et n'entre en collision avec RIEN. La garde porte donc sur
// l'OPÉRATION, pas sur la ligne du grand livre.
func TestPayOrderReplayAfterPromoExhaustedDoesNotDebitCash(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := seedMoneyWallet(t, repo, 10_000, 3_000)
	orderID := primitive.NewObjectID().Hex()

	require.NoError(t, svc.Pay(context.Background(), owner, 3_000, RefOrder, orderID))
	require.NoError(t, svc.Pay(context.Background(), owner, 3_000, RefOrder, orderID))

	oid, err := primitive.ObjectIDFromHex(owner)
	require.NoError(t, err)
	repo.mu.Lock()
	w := repo.wallets[repo.byOwner[oid]]
	cash, promo := w.BalanceXOF, w.PromoXOF
	repo.mu.Unlock()
	assert.Equal(t, 0, promo, "le promotionnel a payé la commande")
	assert.Equal(t, 10_000, cash, "l'argent réel ne doit pas payer une seconde fois")
}

func TestPayOrderStillDebitsDifferentOrders(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := seedMoneyWallet(t, repo, 10_000, 0)

	require.NoError(t, svc.Pay(context.Background(), owner, 3_000, RefOrder, primitive.NewObjectID().Hex()))
	require.NoError(t, svc.Pay(context.Background(), owner, 2_000, RefOrder, primitive.NewObjectID().Hex()))

	oid, err := primitive.ObjectIDFromHex(owner)
	require.NoError(t, err)
	repo.mu.Lock()
	balance := repo.wallets[repo.byOwner[oid]].BalanceXOF
	repo.mu.Unlock()
	// La garde ne doit pas confondre « déjà fait » et « ressemble à ».
	assert.Equal(t, 5_000, balance)
}

// Le pourboire d'une course est un SECOND mouvement sur la même référence :
// sa propre clé (il ne se confond pas avec le paiement de la course, déjà
// passé), sa propre unicité (rejoué, il ne débite pas deux fois), et son
// propre motif au relevé — « pourboire », pas « paiement ».
func TestATipIsItsOwnMovementOnAPaidRide(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := seedMoneyWallet(t, repo, 10_000, 0)
	rideID := primitive.NewObjectID().Hex()

	require.NoError(t, svc.Pay(context.Background(), owner, 3_000, RefRide, rideID))
	require.NoError(t, svc.Pay(context.Background(), owner, 500, RefTip, rideID), "le pourboire passe après la course")
	require.NoError(t, svc.Pay(context.Background(), owner, 500, RefTip, rideID), "rejoué : succès, sans second débit")

	oid, err := primitive.ObjectIDFromHex(owner)
	require.NoError(t, err)
	repo.mu.Lock()
	balance := repo.wallets[repo.byOwner[oid]].BalanceXOF
	var reasons []string
	for _, tx := range repo.transactions {
		reasons = append(reasons, tx.Reason+":"+tx.RefKind)
	}
	repo.mu.Unlock()
	assert.Equal(t, 6_500, balance)
	assert.Equal(t, []string{"payment:ride", "tip:tip"}, reasons)
}

func TestRefundOrderReplayDoesNotCreditTwice(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	owner := seedMoneyWallet(t, repo, 1_000, 0)
	orderID := primitive.NewObjectID().Hex()

	require.NoError(t, svc.Refund(context.Background(), owner, 3_000, RefOrder, orderID))
	require.NoError(t, svc.Refund(context.Background(), owner, 3_000, RefOrder, orderID))

	oid, err := primitive.ObjectIDFromHex(owner)
	require.NoError(t, err)
	repo.mu.Lock()
	balance := repo.wallets[repo.byOwner[oid]].BalanceXOF
	repo.mu.Unlock()
	// Un remboursement rejoué serait de l'argent offert : plus grave encore
	// qu'un débit doublé, parce que personne ne vient s'en plaindre.
	assert.Equal(t, 4_000, balance)
}

func TestConsumeReplayForTheSameOrderDoesNotDebitTwice(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 5)
	orderID := primitive.NewObjectID().Hex()

	require.NoError(t, svc.Consume(context.Background(), ownerID.Hex(), 1, ReasonOrderAccept, RefOrder, orderID, nil))
	require.NoError(t, svc.Consume(context.Background(), ownerID.Hex(), 1, ReasonOrderAccept, RefOrder, orderID, nil))

	assert.Equal(t, 4, repo.balanceOf(ownerID), "un livreur accepte une course une fois")
}

func TestConsumeWithoutOrderIsNotGuarded(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ownerID := seedWallet(t, repo, WalletTypeDriver, 5)

	// Sans commande, aucune clé naturelle : deux dépenses identiques peuvent
	// être deux gestes voulus, et les confondre bloquerait la seconde.
	require.NoError(t, svc.Consume(context.Background(), ownerID.Hex(), 1, "manual", "", "", nil))
	require.NoError(t, svc.Consume(context.Background(), ownerID.Hex(), 1, "manual", "", "", nil))

	assert.Equal(t, 3, repo.balanceOf(ownerID))
}

// Le portefeuille d'un client n'a JAMAIS pu s'ouvrir : `CreateWallet` refusait
// le type `client` que tous ses appelants lui passaient. Trois conséquences,
// chacune épinglée ici : l'ouverture doit passer, un client sans portefeuille
// doit en LIRE un (vide) plutôt qu'un 404, et une recharge confirmée doit se
// poser quelque part — un paiement `succeeded` sans crédit est de l'argent
// perdu.
func TestClientWalletOpensOnDemand(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ctx := context.Background()

	require.NoError(t, svc.CreateWallet(ctx, primitive.NewObjectID().Hex(), WalletTypeClient),
		"le type client doit être accepté")

	// Un compte de la période où l'ouverture échouait : aucun portefeuille.
	userID := primitive.NewObjectID().Hex()
	w, err := svc.GetWallet(ctx, userID, auth.RoleClient, "")
	require.NoError(t, err, "un client sans portefeuille en lit un vide, pas un 404")
	assert.Equal(t, WalletTypeClient, w.Type)
	assert.Equal(t, 0, w.BalanceXOF)

	// Une recharge sur un AUTRE compte sans portefeuille : elle doit y arriver.
	other := primitive.NewObjectID().Hex()
	require.NoError(t, svc.TopUp(ctx, other, 2_000, map[string]any{"payment_id": "p1"}))
	w, err = svc.GetWallet(ctx, other, auth.RoleClient, "")
	require.NoError(t, err)
	assert.Equal(t, 2_000, w.BalanceXOF, "la recharge crédite le portefeuille qu'elle vient d'ouvrir")

	// Un livreur, lui, doit AVOIR son portefeuille : l'absence reste visible.
	_, err = svc.GetWallet(ctx, primitive.NewObjectID().Hex(), auth.RoleDriver, "")
	require.Error(t, err, "pas d'ouverture implicite pour un livreur")
}

// La fiche d'un compte lit le grand livre DU PLUS RÉCENT au plus ancien, et
// chaque mouvement dit son unité ; le geste promotionnel d'un opérateur y
// figure sous son motif, à part de l'argent du client.
func TestAdminLedgerIsNewestFirstAndPromoIsApart(t *testing.T) {
	svc, _, _, _ := newTestService(newFakeRepo())
	owner := primitive.NewObjectID()
	require.NoError(t, svc.CreateWallet(context.Background(), owner.Hex(), WalletTypeClient))
	require.NoError(t, svc.TopUp(context.Background(), owner.Hex(), 2000, map[string]any{"payment_id": "pay-1"}))
	w, err := svc.PromoByOperator(context.Background(), "admin-1", owner.Hex(), 500, "dédommagement")
	require.NoError(t, err)
	assert.Equal(t, 2000, w.BalanceXOF)
	assert.Equal(t, 500, w.PromoXOF)
	_, err = svc.PromoByOperator(context.Background(), "admin-1", owner.Hex(), 500, "")
	assert.Error(t, err, "sans justificatif, pas de geste")

	items, _, err := svc.TransactionsOf(context.Background(), owner.Hex(), httpx.Page{Limit: 10})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, ReasonPromoCredit, items[0].Reason, "le plus récent d'abord")
	assert.Equal(t, "xof", items[0].Unit)
	assert.Equal(t, ReasonWalletTopup, items[1].Reason)
}

// --- le matériel : une retenue bornée, rejouable, sur l'argent réel seulement --

func TestEquipmentChargeTakesAllOrWhatThereIs(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ctx := context.Background()
	owner := primitive.NewObjectID().Hex()
	require.NoError(t, svc.CreateWallet(ctx, owner, WalletTypeDriver))
	require.NoError(t, svc.CreditEarnings(ctx, owner, 1500, ReasonDeliveryFee, RefOrder, primitive.NewObjectID().Hex(), nil))
	contract := primitive.NewObjectID().Hex()

	// Sans partiel, un solde insuffisant ne prend RIEN — pas une erreur.
	taken, err := svc.ChargeEquipment(ctx, owner, 2000, false, contract, "equipment:"+contract+":1")
	require.NoError(t, err)
	assert.Equal(t, 0, taken)
	// … et la clé reste libre : une fois le solde crédité, le même balayage
	// (même clé du jour) doit pouvoir prélever.
	require.NoError(t, svc.CreditEarnings(ctx, owner, 1000, ReasonDeliveryFee, RefOrder, primitive.NewObjectID().Hex(), nil))
	taken, err = svc.ChargeEquipment(ctx, owner, 2000, false, contract, "equipment:"+contract+":1")
	require.NoError(t, err)
	assert.Equal(t, 2000, taken)
	require.NoError(t, svc.CreditEarnings(ctx, owner, 1000, ReasonDeliveryFee, RefOrder, primitive.NewObjectID().Hex(), nil))

	// Avec partiel, ce qu'il y a.
	taken, err = svc.ChargeEquipment(ctx, owner, 2000, true, contract, "equipment:"+contract+":2")
	require.NoError(t, err)
	assert.Equal(t, 1500, taken)

	// Rejouer la même clé ne prend pas deux fois.
	require.NoError(t, svc.CreditEarnings(ctx, owner, 500, ReasonDeliveryFee, RefOrder, primitive.NewObjectID().Hex(), nil))
	taken, err = svc.ChargeEquipment(ctx, owner, 500, false, contract, "equipment:"+contract+":2")
	require.NoError(t, err)
	assert.Equal(t, 0, taken)

	w, err := svc.WalletOf(ctx, owner)
	require.NoError(t, err)
	assert.Equal(t, 500, w.BalanceXOF)
	// Le grand livre dit « matériel », rattaché au contrat — une ligne par
	// prélèvement effectif (2 000 puis 1 500), aucune pour ce qui n'a rien pris.
	var reasons []string
	for _, tx := range repo.transactions {
		if tx.Reason == ReasonEquipment {
			reasons = append(reasons, tx.RefKind)
			assert.Equal(t, contract, tx.RefID.Hex())
		}
	}
	assert.Equal(t, []string{RefEquipment, RefEquipment}, reasons)
}

// La COMMISSION et la DETTE : un gain arrive net de commission ; une course
// en espèces prend ce qu'il y a et porte le reste à la dette ; le prochain
// gain rembourse d'abord.
func TestCommissionAndDebt(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _, _ := newTestService(repo)
	ctx := context.Background()
	owner := primitive.NewObjectID().Hex()
	require.NoError(t, svc.CreateWallet(ctx, owner, WalletTypeDriver))
	order := primitive.NewObjectID().Hex()

	// Un gain de 1 000 avec 150 de commission : 850 restent.
	retained, repaid, err := svc.CreditEarningsNet(ctx, owner, 1000, 150, ReasonDeliveryFee, RefOrder, order, nil)
	require.NoError(t, err)
	assert.Equal(t, 150, retained)
	assert.Equal(t, 0, repaid)
	w, _ := svc.WalletOf(ctx, owner)
	assert.Equal(t, 850, w.BalanceXOF)
	// Rejoué : rien deux fois.
	_, _, err = svc.CreditEarningsNet(ctx, owner, 1000, 150, ReasonDeliveryFee, RefOrder, order, nil)
	require.NoError(t, err)
	w, _ = svc.WalletOf(ctx, owner)
	assert.Equal(t, 850, w.BalanceXOF)

	// Une course en espèces : 1 200 de commission, le solde n'en a que 850.
	cash := primitive.NewObjectID().Hex()
	paid, owed, err := svc.Owe(ctx, owner, 1200, ReasonCommission, ReasonCommissionDue, RefOrder, cash)
	require.NoError(t, err)
	assert.Equal(t, 850, paid)
	assert.Equal(t, 350, owed)
	w, _ = svc.WalletOf(ctx, owner)
	assert.Equal(t, 0, w.BalanceXOF)
	assert.Equal(t, 350, w.DebtXOF)

	// Le gain suivant rembourse la dette d'abord.
	next := primitive.NewObjectID().Hex()
	retained, repaid, err = svc.CreditEarningsNet(ctx, owner, 500, 0, ReasonDeliveryFee, RefOrder, next, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, retained)
	assert.Equal(t, 350, repaid)
	w, _ = svc.WalletOf(ctx, owner)
	assert.Equal(t, 150, w.BalanceXOF)
	assert.Equal(t, 0, w.DebtXOF)

	// Ce que le grand livre dit : les mouvements de dette portent leur delta
	// et disent s'ils ont touché le solde.
	var due, repaidTx int
	for _, tx := range repo.transactions {
		switch tx.Reason {
		case ReasonCommissionDue:
			due++
			assert.Equal(t, 350, tx.Ref["debt_delta"])
			assert.Equal(t, true, tx.Ref["no_balance"])
		case ReasonDebtRepaid:
			repaidTx++
			assert.Equal(t, -350, tx.Ref["debt_delta"])
			assert.Nil(t, tx.Ref["no_balance"], "remboursé sur le solde : le solde a bougé")
		}
	}
	assert.Equal(t, 1, due)
	assert.Equal(t, 1, repaidTx)

	// Une dette réglée à l'agence ne touche pas le solde.
	_, owed, err = svc.Owe(ctx, owner, 500, ReasonCommission, ReasonCommissionDue, RefOrder, primitive.NewObjectID().Hex())
	require.NoError(t, err)
	assert.Equal(t, 350, owed, "150 pris sur le solde, 350 dus")
	resp, err := svc.SettleDebt(ctx, "admin", owner, 350, "espèces à l'agence")
	require.NoError(t, err)
	assert.Equal(t, 0, resp.DebtXOF)
	assert.Equal(t, 0, resp.BalanceXOF)
	_, err = svc.SettleDebt(ctx, "admin", owner, 1, "trop")
	assert.Error(t, err, "on ne rembourse pas plus que la dette")
}
