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
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	// The schema is dropped only after this pool and the application pool close.
	reset := func() {
		_, _ = pool.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public")
		if _, e := pool.Exec(ctx, string(up1)+string(up2)); e != nil {
			t.Fatal(e)
		}
	}
	reset()
	defer func() {
		pool.Close()
		cleanup, e := pgxpool.New(ctx, url)
		if e == nil {
			_, _ = cleanup.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public")
			cleanup.Close()
		}
	}()
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
	if replay, e := s.ExecuteResult(ctx, command("ins", domain.TransactionBet, money(t, "1000.00"))); e != nil || !replay.IdempotentReplay || replay.Status != domain.TransactionRejected || replay.FailureCode != "INSUFFICIENT_FUNDS" || replay.Balance.MinorUnits() != 7500 {
		t.Fatalf("rejected replay=%+v err=%v", replay, e)
	}
	var beforeLedger, beforeEvents int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries").Scan(&beforeLedger); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events").Scan(&beforeEvents); e != nil {
		t.Fatal(e)
	}
	changed := command("changed", domain.TransactionBet, money(t, "1.00"))
	changed.IdempotencyKey = "key-ins"
	changed.ExternalID = "new-external"
	if _, e = s.ExecuteResult(ctx, changed); !errors.Is(e, application.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict=%v", e)
	}
	externalConflict := command("external-conflict", domain.TransactionBet, money(t, "1.00"))
	externalConflict.ExternalID = "ext-ins"
	if _, e = s.ExecuteResult(ctx, externalConflict); !errors.Is(e, application.ErrExternalIDConflict) {
		t.Fatalf("external conflict=%v", e)
	}
	var afterLedger, afterEvents int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries").Scan(&afterLedger); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events").Scan(&afterEvents); e != nil {
		t.Fatal(e)
	}
	if beforeLedger != afterLedger || beforeEvents != afterEvents {
		t.Fatalf("conflict effects ledger %d/%d events %d/%d", beforeLedger, afterLedger, beforeEvents, afterEvents)
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
	if replay, e := s.ExecuteResult(ctx, dup); e != nil || !replay.IdempotentReplay || replay.TransactionID != "dup" {
		t.Fatalf("duplicate replay=%+v err=%v", replay, e)
	}
	p.Close()
	p, e = postgres.NewPool(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	s = application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	if replay, e := s.ExecuteResult(ctx, dup); e != nil || !replay.IdempotentReplay || replay.TransactionID != "dup" {
		t.Fatalf("restart replay=%+v err=%v", replay, e)
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

func TestProcessWagerRollbackMatrix(t *testing.T) {
	tests := []struct {
		name    string
		source  domain.WagerTransactionType
		wantDir string
	}{
		{"bet", domain.TransactionBet, "CREDIT"},
		{"win", domain.TransactionWin, "DEBIT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, pool, url := integrationDatabase(t)
			defer pool.Close()
			p, err := postgres.NewPool(ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			s := application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
			seed(t, pool)
			source := command("rollback-source-"+tc.name, tc.source, money(t, "25.00"))
			if err = s.Execute(ctx, source); err != nil {
				t.Fatal(err)
			}
			rb := command("rollback-result-"+tc.name, domain.TransactionRollback, money(t, "25.00"))
			rb.ExternalID, rb.IdempotencyKey, rb.ReferenceExternalID = rb.ID+"-ext", rb.ID+"-key", source.ExternalID
			if err = s.Execute(ctx, rb); err != nil {
				t.Fatal(err)
			}
			var status, ref, direction string
			if err = pool.QueryRow(ctx, "SELECT status,reference_transaction_id FROM wager_transactions WHERE id=$1", rb.ID).Scan(&status, &ref); err != nil {
				t.Fatal(err)
			}
			if status != "PROCESSED" || ref != source.ID {
				t.Fatalf("status=%s ref=%s", status, ref)
			}
			if err = pool.QueryRow(ctx, "SELECT direction FROM wallet_ledger_entries WHERE transaction_id=$1", rb.ID).Scan(&direction); err != nil || direction != tc.wantDir {
				t.Fatalf("direction=%s err=%v", direction, err)
			}
		})
	}
	t.Run("refund", func(t *testing.T) {
		ctx, pool, url := integrationDatabase(t)
		defer pool.Close()
		p, err := postgres.NewPool(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		s := application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
		seed(t, pool)
		bet := command("rollback-refund-bet", domain.TransactionBet, money(t, "25.00"))
		if err = s.Execute(ctx, bet); err != nil {
			t.Fatal(err)
		}
		refund := command("rollback-refund-source", domain.TransactionRefund, money(t, "25.00"))
		refund.ExternalID, refund.IdempotencyKey, refund.ReferenceExternalID = "rollback-refund-source-ext", "rollback-refund-source-key", bet.ExternalID
		if err = s.Execute(ctx, refund); err != nil {
			t.Fatal(err)
		}
		rb := command("rollback-refund-result", domain.TransactionRollback, money(t, "25.00"))
		rb.ExternalID, rb.IdempotencyKey, rb.ReferenceExternalID = "rollback-refund-result-ext", "rollback-refund-result-key", refund.ExternalID
		if err = s.Execute(ctx, rb); err != nil {
			t.Fatal(err)
		}
		var status, ref, direction string
		if err = pool.QueryRow(ctx, "SELECT status,reference_transaction_id FROM wager_transactions WHERE id=$1", rb.ID).Scan(&status, &ref); err != nil || status != "PROCESSED" || ref != refund.ID {
			t.Fatalf("status=%s ref=%s err=%v", status, ref, err)
		}
		if err = pool.QueryRow(ctx, "SELECT direction FROM wallet_ledger_entries WHERE transaction_id=$1", rb.ID).Scan(&direction); err != nil || direction != "DEBIT" {
			t.Fatalf("direction=%s err=%v", direction, err)
		}
	})
	t.Run("insufficient-and-duplicate", func(t *testing.T) {
		ctx, pool, url := integrationDatabase(t)
		defer pool.Close()
		p, err := postgres.NewPool(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		s := application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
		seed(t, pool)
		win := command("rollback-insufficient-win", domain.TransactionWin, money(t, "25.00"))
		if err = s.Execute(ctx, win); err != nil {
			t.Fatal(err)
		}
		spend := command("rollback-insufficient-spend", domain.TransactionBet, money(t, "125.00"))
		if err = s.Execute(ctx, spend); err != nil {
			t.Fatal(err)
		}
		rb := command("rollback-insufficient-result", domain.TransactionRollback, money(t, "25.00"))
		rb.ExternalID, rb.IdempotencyKey, rb.ReferenceExternalID = "rollback-insufficient-ext", "rollback-insufficient-key", win.ExternalID
		if err = s.Execute(ctx, rb); err != nil {
			t.Fatal(err)
		}
		var status, failure, ref string
		if err = pool.QueryRow(ctx, "SELECT status,failure_code,reference_transaction_id FROM wager_transactions WHERE id=$1", rb.ID).Scan(&status, &failure, &ref); err != nil || status != "REJECTED" || failure != "INSUFFICIENT_FUNDS" || ref != win.ID {
			t.Fatalf("status=%s failure=%s ref=%s err=%v", status, failure, ref, err)
		}
		win2 := command("rollback-duplicate-win", domain.TransactionWin, money(t, "25.00"))
		if err = s.Execute(ctx, win2); err != nil {
			t.Fatal(err)
		}
		first := command("rollback-duplicate-first", domain.TransactionRollback, money(t, "25.00"))
		first.ExternalID, first.IdempotencyKey, first.ReferenceExternalID = "rollback-duplicate-first-ext", "rollback-duplicate-first-key", win2.ExternalID
		if err = s.Execute(ctx, first); err != nil {
			t.Fatal(err)
		}
		second := command("rollback-duplicate-result", domain.TransactionRollback, money(t, "25.00"))
		second.ExternalID, second.IdempotencyKey, second.ReferenceExternalID = "rollback-duplicate-ext", "rollback-duplicate-key", win2.ExternalID
		if err = s.Execute(ctx, second); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, "SELECT status,failure_code,reference_transaction_id FROM wager_transactions WHERE id=$1", second.ID).Scan(&status, &failure, &ref); err != nil || status != "REJECTED" || failure != "REFERENCE_ALREADY_REVERSED" || ref != win2.ID {
			t.Fatalf("duplicate status=%s failure=%s ref=%s err=%v", status, failure, ref, err)
		}
	})
}

func TestProcessWagerRefundProcessedBet(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	root, _ := filepath.Abs("../../")
	ctx := context.Background()
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_, _ = p.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public")
	for _, name := range []string{"000001_schema.up.sql", "000002_wager_currency.up.sql", "000003_pending_reference.up.sql"} {
		b, e := os.ReadFile(filepath.Join(root, "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = p.Exec(ctx, string(b)); e != nil {
			t.Fatal(e)
		}
	}
	pool, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := application.ProcessWagerService{Manager: postgres.NewTxManager(pool), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	seed(t, p)
	bet := command("refund-bet", domain.TransactionBet, money(t, "25.00"))
	if err = s.Execute(ctx, bet); err != nil {
		t.Fatal(err)
	}
	refund := command("refund", domain.TransactionRefund, money(t, "25.00"))
	refund.ExternalID, refund.IdempotencyKey, refund.ReferenceExternalID = "refund-ext", "refund-key", bet.ExternalID
	if err = s.Execute(ctx, refund); err != nil {
		t.Fatal(err)
	}
	var balance int64
	var status, refID string
	var version int64
	if err = p.QueryRow(ctx, "SELECT balance_minor,version FROM wallets WHERE id='wallet'").Scan(&balance, &version); err != nil {
		t.Fatal(err)
	}
	if balance != 10000 || version != 3 {
		t.Fatalf("wallet=%d version=%d", balance, version)
	}
	if err = p.QueryRow(ctx, "SELECT status,reference_transaction_id FROM wager_transactions WHERE id='refund'").Scan(&status, &refID); err != nil || status != "PROCESSED" || refID == "" {
		t.Fatalf("refund=%s ref=%s err=%v", status, refID, err)
	}
	var direction string
	var before, after, amount int64
	if err = p.QueryRow(ctx, "SELECT direction,amount_minor,before_minor,after_minor FROM wallet_ledger_entries WHERE transaction_id='refund'").Scan(&direction, &amount, &before, &after); err != nil {
		t.Fatal(err)
	}
	if direction != "CREDIT" || amount != 2500 || before != 7500 || after != 10000 {
		t.Fatalf("ledger=%s amount=%d before=%d after=%d", direction, amount, before, after)
	}
	var events int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE transaction_id='refund'").Scan(&events); err != nil || events != 2 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	bad := command("refund-bad", domain.TransactionRefund, money(t, "24.00"))
	bad.ExternalID, bad.IdempotencyKey, bad.ReferenceExternalID = "refund-bad-ext", "refund-bad-key", bet.ExternalID
	if err = s.Execute(ctx, bad); err != nil {
		t.Fatal(err)
	}
	var failure string
	if err = p.QueryRow(ctx, "SELECT status,failure_code FROM wager_transactions WHERE id='refund-bad'").Scan(&status, &failure); err != nil || status != "REJECTED" || failure != "REFERENCE_ALREADY_REVERSED" {
		t.Fatalf("bad=%s/%s err=%v", status, failure, err)
	}
	if err = p.QueryRow(ctx, "SELECT reference_transaction_id FROM wager_transactions WHERE id='refund-bad'").Scan(&refID); err != nil || refID != bet.ID {
		t.Fatalf("bad reference=%s err=%v", refID, err)
	}
	if err = p.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet'").Scan(&balance); err != nil || balance != 10000 {
		t.Fatalf("balance after mismatch=%d err=%v", balance, err)
	}
	var badLedger int
	if err = p.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='refund-bad'").Scan(&badLedger); err != nil || badLedger != 0 {
		t.Fatalf("mismatch ledger=%d err=%v", badLedger, err)
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

func TestProcessWagerHistoricalReplay(t *testing.T) {
	ctx, pool, url := integrationDatabase(t)

	p, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s := application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	seed(t, pool)

	original := command("historical-original", domain.TransactionBet, money(t, "25.00"))
	if _, err = s.ExecuteResult(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err = s.Execute(ctx, command("historical-win", domain.TransactionWin, money(t, "25.00"))); err != nil {
		t.Fatal(err)
	}

	var beforeLedger, beforeEvents int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries").Scan(&beforeLedger); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events").Scan(&beforeEvents); err != nil {
		t.Fatal(err)
	}
	replay := original
	replay.ID = "historical-replay"
	replay.TransactionID = "replay-ledger"
	replay.ProcessedEventID = "replay-processed"
	replay.RejectedEventID = "replay-rejected"
	replay.BalanceEventID = "replay-balance"
	replay.Now = original.Now.Add(time.Minute)
	result, err := s.ExecuteResult(ctx, replay)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IdempotentReplay || result.TransactionID != original.ID || result.Balance.MinorUnits() != 7500 {
		t.Fatalf("historical replay=%+v", result)
	}
	var afterLedger, afterEvents int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries").Scan(&afterLedger); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events").Scan(&afterEvents); err != nil {
		t.Fatal(err)
	}
	if beforeLedger != afterLedger || beforeEvents != afterEvents {
		t.Fatalf("replay effects ledger %d/%d events %d/%d", beforeLedger, afterLedger, beforeEvents, afterEvents)
	}
}

func TestProcessWagerConcurrentIdenticalReplay(t *testing.T) {
	ctx, pool, url := integrationDatabase(t)
	seed(t, pool)

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
	now := time.Now().UTC()
	first := command("identical-a", domain.TransactionBet, money(t, "25.00"))
	first.ProviderID = "same-provider"
	first.ExternalID = "same-external"
	first.IdempotencyKey = "same-key"
	first.Now = now
	second := first
	second.ID = "identical-b"
	second.TransactionID = "identical-b-ledger"
	second.ProcessedEventID = "identical-b-processed"
	second.RejectedEventID = "identical-b-rejected"
	second.BalanceEventID = "identical-b-balance"

	start := make(chan struct{})
	type outcome struct {
		result application.ProcessWagerResult
		err    error
	}
	results := make(chan outcome, 2)
	go func() { <-start; result, err := service1.ExecuteResult(ctx, first); results <- outcome{result, err} }()
	go func() { <-start; result, err := service2.ExecuteResult(ctx, second); results <- outcome{result, err} }()
	close(start)
	got := []outcome{<-results, <-results}
	var transactionID string
	processed, replayed := 0, 0
	for _, result := range got {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if transactionID == "" {
			transactionID = result.result.TransactionID
		}
		if result.result.TransactionID != transactionID {
			t.Fatalf("transaction IDs differ: %q and %q", transactionID, result.result.TransactionID)
		}
		if result.result.IdempotentReplay {
			replayed++
		} else {
			processed++
		}
	}
	if processed != 1 || replayed != 1 {
		t.Fatalf("processed=%d replayed=%d", processed, replayed)
	}
	var balance int64
	if err = pool.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet'").Scan(&balance); err != nil || balance != 7500 {
		t.Fatalf("balance=%d err=%v", balance, err)
	}
	var transactions, ledger, events int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM wager_transactions WHERE wallet_id='wallet' AND kind='BET'").Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE wallet_id='wallet' AND direction='DEBIT'").Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE aggregate_id='wallet'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if transactions != 1 || ledger != 1 || events != 2 {
		t.Fatalf("transactions=%d ledger=%d events=%d", transactions, ledger, events)
	}
}

func integrationDatabase(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	root, err := filepath.Abs("../../")
	if err != nil {
		t.Fatal(err)
	}
	up1, err := os.ReadFile(filepath.Join(root, "migrations/000001_schema.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up2, err := os.ReadFile(filepath.Join(root, "migrations/000002_wager_currency.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up3, err := os.ReadFile(filepath.Join(root, "migrations/000003_pending_reference.up.sql"))
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
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"+string(up1)+string(up2)+string(up3)); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, string(down2)+string(down1))
		pool.Close()
	})
	return ctx, pool, url
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
