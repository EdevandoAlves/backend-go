//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoriesIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	root, err := filepath.Abs("../../../")
	if err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile(filepath.Join(root, "migrations", "000001_schema.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile(filepath.Join(root, "migrations", "000001_schema.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reset := func() {
		if _, err := pool.Exec(ctx, string(down)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(up)); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer func() { _, _ = pool.Exec(ctx, string(down)) }()

	t.Run("rollback", func(t *testing.T) { testAtomic(t, pool, "rollback", false) })
	t.Run("commit", func(t *testing.T) { testAtomic(t, pool, "commit", true) })
	t.Run("locks", func(t *testing.T) { testLocks(t, pool) })
	t.Run("version and errors", func(t *testing.T) { testVersionAndErrors(t, pool) })
}

func fixture(t *testing.T, tx pgx.Tx, id, player string) (domain.Wallet, domain.WagerTransaction, domain.WalletLedgerEntry) {
	t.Helper()
	now := time.Now().UTC()
	money, _ := domain.ParseMoney("1.00", "BRL")
	zero, _ := domain.Zero("BRL")
	w, err := domain.CreateWallet(id, player, money, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := (WalletRepository{}).Insert(context.Background(), tx, w); err != nil {
		t.Fatal(err)
	}
	opening, err := domain.CreateOpeningWagerTransaction("opening-"+id, player, id, money, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := (TransactionRepository{}).InsertOpening(context.Background(), tx, opening, money, 1); err != nil {
		t.Fatal(err)
	}
	entry, err := domain.NewWalletLedgerEntry("ledger-"+id, id, opening.ID(), domain.LedgerCredit, money, zero, money)
	if err != nil {
		t.Fatal(err)
	}
	if err := (LedgerRepository{}).Insert(context.Background(), tx, entry); err != nil {
		t.Fatal(err)
	}
	event := OutboxEvent{ID: "event-" + id, AggregateID: id, TransactionID: opening.ID(), EventType: "WagerTransactionProcessed", CorrelationID: id, EventVersion: 1, Payload: []byte(`{"wallet":"` + id + `"}`)}
	if err := (OutboxRepository{}).Insert(context.Background(), tx, event); err != nil {
		t.Fatal(err)
	}
	return w, opening, entry
}

func testAtomic(t *testing.T, pool *pgxpool.Pool, suffix string, commit bool) {
	m := &TxManager{pool: pool}
	ctx := context.Background()
	injected := errors.New("injected failure")
	err := m.WithinTransaction(ctx, func(tx pgx.Tx) error {
		fixture(t, tx, "wallet-atomic-"+suffix, "player-atomic-"+suffix)
		if !commit {
			return injected
		}
		return nil
	})
	if commit && err != nil {
		t.Fatal(err)
	}
	if !commit && !errors.Is(err, injected) {
		t.Fatalf("error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id='wallet-atomic'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	expected := 0
	if commit {
		expected = 1
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id=$1", "wallet-atomic-"+suffix).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("wallet count = %d, want %d", count, expected)
	}
	for _, table := range []string{"wager_transactions", "wallet_ledger_entries", "outbox_events"} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id LIKE $1", "%"+suffix).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Fatalf("%s count = %d, want %d", table, count, expected)
		}
	}
}

func testLocks(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	setup, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixture(t, setup, "wallet-a", "player-a")
	fixture(t, setup, "wallet-b", "player-b")
	if err := setup.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback(ctx)
	if _, err := (WalletRepository{}).GetForUpdate(ctx, tx1, "wallet-a"); err != nil {
		t.Fatal(err)
	}
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback(ctx)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); _, err := (WalletRepository{}).GetForUpdate(ctx, tx2, "wallet-a"); done <- err }()
	<-started
	select {
	case <-done:
		t.Fatal("wallet A lock did not block")
	case <-time.After(100 * time.Millisecond):
	}
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txB.Rollback(ctx)
	bctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if _, err := (WalletRepository{}).GetForUpdate(bctx, txB, "wallet-b"); err != nil {
		t.Fatalf("wallet B blocked: %v", err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("wallet A did not unblock")
	}
}

func testVersionAndErrors(t *testing.T, pool *pgxpool.Pool) {
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = fixture(t, tx, "wallet-v", "player-v")
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	check, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := (WalletRepository{}).Get(ctx, check, "wallet-v")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Balance().MinorUnits() != 100 || loaded.Version() != 1 {
		t.Fatalf("loaded wallet = %s/version %d, want 1.00/version 1", loaded.Balance(), loaded.Version())
	}
	money, _ := domain.ParseMoney("1.00", "BRL")
	if err := loaded.Credit(money, time.Now()); err != nil {
		t.Fatal(err)
	}
	if loaded.Balance().MinorUnits() != 200 || loaded.Version() != 2 {
		t.Fatalf("credited wallet = %s/version %d, want 2.00/version 2", loaded.Balance(), loaded.Version())
	}
	if err := (WalletRepository{}).Save(ctx, check, loaded, 1); err != nil {
		t.Fatal(err)
	}
	if err := check.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	check, _ = pool.Begin(ctx)
	if err := (WalletRepository{}).Save(ctx, check, loaded, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("save error = %v", err)
	}
	check.Rollback(ctx)
	tx, _ = pool.Begin(ctx)
	if _, err := (WalletRepository{}).Get(ctx, tx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found error = %v", err)
	}
	tx.Rollback(ctx)
	tx, _ = pool.Begin(ctx)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = (WalletRepository{}).Get(canceled, tx, "wallet-v")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	tx.Rollback(ctx)
}
