//go:build integration

package application_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/adapters/postgres"
	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProcessWagerIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	root, _ := filepath.Abs("../../")
	up1, _ := os.ReadFile(filepath.Join(root, "migrations/000001_schema.up.sql"))
	up2, _ := os.ReadFile(filepath.Join(root, "migrations/000002_wager_currency.up.sql"))
	down1, _ := os.ReadFile(filepath.Join(root, "migrations/000001_schema.down.sql"))
	down2, _ := os.ReadFile(filepath.Join(root, "migrations/000002_wager_currency.down.sql"))
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	reset := func() {
		_, _ = pool.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public")
		if _, e := pool.Exec(ctx, string(up1)+string(up2)); e != nil {
			t.Fatal(e)
		}
	}
	reset()
	defer pool.Exec(ctx, string(down2)+string(down1))
	p, e := postgres.NewPool(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	s := application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	seed(t, pool)
	cmd := command("bet", domain.TransactionBet, money(t, "25.00"))
	if e = s.Execute(ctx, cmd); e != nil {
		t.Fatal(e)
	}
	var balance int64
	var n int
	if e = pool.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet'").Scan(&balance); e != nil || balance != 7500 {
		t.Fatalf("balance=%d err=%v", balance, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='bet'").Scan(&n); e != nil || n != 1 {
		t.Fatalf("ledger=%d err=%v", n, e)
	}
	ins := command("ins", domain.TransactionBet, money(t, "1000.00"))
	if e = s.Execute(ctx, ins); e != nil {
		t.Fatal(e)
	}
	var status, failure string
	if e = pool.QueryRow(ctx, "SELECT status,failure_code FROM wager_transactions WHERE id='ins'").Scan(&status, &failure); e != nil || status != "REJECTED" || failure != "INSUFFICIENT_FUNDS" {
		t.Fatalf("%s/%s err=%v", status, failure, e)
	}
	win := command("win", domain.TransactionWin, money(t, "25.00"))
	if e = s.Execute(ctx, win); e != nil {
		t.Fatal(e)
	}
	loss := command("loss", domain.TransactionLoss, money(t, "0.00"))
	if e = s.Execute(ctx, loss); e != nil {
		t.Fatal(e)
	}
	mismatch := command("mismatch", domain.TransactionWin, moneyUSD(t, "1.00"))
	if e = s.Execute(ctx, mismatch); e != nil {
		t.Fatal(e)
	}
	var resultCurrency string
	if e = pool.QueryRow(ctx, "SELECT result_currency FROM wager_transactions WHERE id='mismatch'").Scan(&resultCurrency); e != nil || resultCurrency != "BRL" {
		t.Fatalf("currency=%s err=%v", resultCurrency, e)
	}
	dup := command("dup", domain.TransactionBet, money(t, "1.00"))
	if e = s.Execute(ctx, dup); e != nil {
		t.Fatal(e)
	}
	if e = s.Execute(ctx, dup); !errors.Is(e, application.ErrConflict) {
		t.Fatalf("duplicate=%v", e)
	}
	s.AfterStep = func(step string) error {
		if step == "afterTerminal" {
			return errors.New("injected")
		}
		return nil
	}
	rollback := command("rollback", domain.TransactionBet, money(t, "1.00"))
	if e = s.Execute(ctx, rollback); e == nil {
		t.Fatal("injected error accepted")
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM wager_transactions WHERE id='rollback'").Scan(&n); e != nil || n != 0 {
		t.Fatalf("rollback=%d err=%v", n, e)
	}
}

func TestProcessWagerConcurrentBets(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	root, _ := filepath.Abs("../../")
	up1, err := os.ReadFile(filepath.Join(root, "migrations/000001_schema.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up2, err := os.ReadFile(filepath.Join(root, "migrations/000002_wager_currency.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	down1, err := os.ReadFile(filepath.Join(root, "migrations/000001_schema.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	down2, err := os.ReadFile(filepath.Join(root, "migrations/000002_wager_currency.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"+string(up1)+string(up2)); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, string(down2)+string(down1))
	const walletID = "wallet-concurrent"
	_, err = pool.Exec(ctx, `INSERT INTO wallets(id,player_id,currency,balance_minor,version,created_at,updated_at) VALUES ($1,'player-concurrent','BRL',10000,1,now(),now())`, walletID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO wager_transactions(id,origin,player_id,wallet_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('opening-concurrent','INTERNAL','player-concurrent',$1,'OPENING',10000,'BRL','PROCESSED',10000,'BRL',1)`, walletID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO wallet_ledger_entries(id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor,created_at) VALUES ('opening-ledger-concurrent',$1,'opening-concurrent','BRL','CREDIT',10000,0,10000,now())`, walletID)
	if err != nil {
		t.Fatal(err)
	}

	p1, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p1.Close()
	p2, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p2.Close()
	service1 := application.ProcessWagerService{Manager: postgres.NewTxManager(p1), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	service2 := application.ProcessWagerService{Manager: postgres.NewTxManager(p2), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	makeCommand := func(id string) application.ProcessWagerCommand {
		m := money(t, "80.00")
		now := time.Now().UTC()
		return application.ProcessWagerCommand{ID: id, ProviderID: "provider-concurrent", ExternalID: "external-" + id, IdempotencyKey: "key-" + id, PlayerID: "player-concurrent", WalletID: walletID, GameID: "game", RoundID: "round", Kind: domain.TransactionBet, Amount: m, Now: now, TransactionID: "ledger-" + id, ProcessedEventID: "processed-" + id, RejectedEventID: "rejected-" + id, BalanceEventID: "balance-" + id}
	}
	commands := []application.ProcessWagerCommand{makeCommand("concurrent-a"), makeCommand("concurrent-b")}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- service1.Execute(ctx, commands[0]) }()
	go func() { <-start; results <- service2.Execute(ctx, commands[1]) }()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent execute: %v", err)
		}
	}
	var balance int64
	var version int64
	if err := pool.QueryRow(ctx, `SELECT balance_minor,version FROM wallets WHERE id=$1`, walletID).Scan(&balance, &version); err != nil {
		t.Fatal(err)
	}
	if balance != 2000 || version != 2 {
		t.Fatalf("wallet=%d/version=%d, want 2000/2", balance, version)
	}
	var processed, rejected, ledger, events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wager_transactions WHERE wallet_id=$1 AND kind='BET'`, walletID).Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wager_transactions WHERE wallet_id=$1 AND kind='BET' AND status='REJECTED' AND failure_code='INSUFFICIENT_FUNDS'`, walletID).Scan(&rejected); err != nil {
		t.Fatal(err)
	}
	if processed != 2 || rejected != 1 {
		t.Fatalf("transactions=%d rejected=%d", processed, rejected)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id=$1 AND direction='DEBIT'`, walletID).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if ledger != 1 {
		t.Fatalf("debit ledger=%d", ledger)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1`, walletID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 3 {
		t.Fatalf("outbox events=%d", events)
	}
}

func command(id string, k domain.WagerTransactionType, m domain.Money) application.ProcessWagerCommand {
	now := time.Now().UTC()
	return application.ProcessWagerCommand{ID: id, ProviderID: "provider", ExternalID: "ext-" + id, IdempotencyKey: "key-" + id, PlayerID: "player", WalletID: "wallet", GameID: "game", RoundID: "round", Kind: k, Amount: m, Now: now, TransactionID: "ledger-" + id, ProcessedEventID: "processed-" + id, RejectedEventID: "rejected-" + id, BalanceEventID: "balance-" + id}
}
func money(t *testing.T, v string) domain.Money {
	t.Helper()
	m, e := domain.ParseMoney(v, "BRL")
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func moneyUSD(t *testing.T, v string) domain.Money {
	t.Helper()
	m, e := domain.ParseMoney(v, "USD")
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func seed(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	tx, e := p.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	_, e = tx.Exec(context.Background(), "INSERT INTO wallets(id,player_id,currency,balance_minor,version,created_at,updated_at) VALUES ('wallet','player','BRL',10000,1,$1,$1)", now)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(context.Background(), "INSERT INTO wager_transactions(id,origin,player_id,wallet_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('opening','INTERNAL','player','wallet','OPENING',10000,'BRL','PROCESSED',10000,'BRL',1)")
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(context.Background(), "INSERT INTO wallet_ledger_entries(id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor,created_at) VALUES ('open-ledger','wallet','opening','BRL','CREDIT',10000,0,10000,$1)", now)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(context.Background()); e != nil {
		t.Fatal(e)
	}
}
