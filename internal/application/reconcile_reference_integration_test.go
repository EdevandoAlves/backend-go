//go:build integration

package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/adapters/postgres"
	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/jackc/pgx/v5"
)

func TestReconcileReferenceIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		conn.Close(ctx)
		cleanup, e := pgx.Connect(ctx, url)
		if e == nil { _, _ = cleanup.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); cleanup.Close(ctx) }
	}()
	root, _ := filepath.Abs("../../")
	for _, name := range []string{"000001_schema.up.sql", "000002_wager_currency.up.sql", "000003_pending_reference.up.sql"} {
		b, e := os.ReadFile(filepath.Join(root, "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = conn.Exec(ctx, string(b)); e != nil {
			t.Fatal(e)
		}
	}
	hash := strings.Repeat("a", 64)
	if _, err = conn.Exec(ctx, `INSERT INTO wallets(id,player_id,currency,balance_minor,version) VALUES ('wallet-9','player-9','BRL',7500,2)`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('bet-9','EXTERNAL','bet-later','provider-9','bet-key',$1,'player-9','wallet-9','game-9','round-9','BET',2500,'BRL','PROCESSED',7500,'BRL',2)`, hash); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = conn.Exec(ctx, `INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,reference_external_id,attempt_count,next_attempt_at) VALUES ('refund-9','EXTERNAL','refund-9','provider-9','refund-key',$1,'player-9','wallet-9','game-9','round-9','REFUND',2500,'BRL','PENDING_REFERENCE','bet-later',0,$2)`, hash, now); err != nil {
		t.Fatal(err)
	}
	p, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s := application.ReconcileReferenceService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	worked, err := s.ReconcileOne(ctx, now.Add(time.Second), application.ReconcileReferenceIDs{ProcessedEventID: "refund-processed-event", BalanceEventID: "refund-balance-event", LedgerID: "refund-ledger"})
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	var status, reference string
	if err = conn.QueryRow(ctx, "SELECT status, reference_transaction_id FROM wager_transactions WHERE id='refund-9'").Scan(&status, &reference); err != nil {
		t.Fatal(err)
	}
	if status != "PROCESSED" || reference != "bet-9" {
		t.Fatalf("refund status=%s reference=%s", status, reference)
	}
	var balance int64
	var version int64
	if err = conn.QueryRow(ctx, "SELECT balance_minor, version FROM wallets WHERE id='wallet-9'").Scan(&balance, &version); err != nil {
		t.Fatal(err)
	}
	if balance != 10000 || version != 3 {
		t.Fatalf("wallet=%d/%d", balance, version)
	}
	var ledger int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='refund-9' AND direction='CREDIT' AND amount_minor=2500").Scan(&ledger); err != nil || ledger != 1 {
		t.Fatalf("ledger=%d err=%v", ledger, err)
	}
	var events int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE transaction_id='refund-9'").Scan(&events); err != nil || events != 2 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	var betStatus string
	if err = conn.QueryRow(ctx, "SELECT status FROM wager_transactions WHERE id='bet-9'").Scan(&betStatus); err != nil || betStatus != "PROCESSED" {
		t.Fatalf("bet=%s err=%v", betStatus, err)
	}
}

func TestReconcileReferenceRetryExhaustion(t *testing.T) {
	url := "postgres://postgres:postgres@localhost:5432/s25_server_integration_test?sslmode=disable"
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err = conn.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		conn.Close(ctx)
		cleanup, e := pgx.Connect(ctx, url)
		if e == nil { _, _ = cleanup.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); cleanup.Close(ctx) }
	}()
	root, _ := filepath.Abs("../../")
	for _, name := range []string{"000001_schema.up.sql", "000002_wager_currency.up.sql", "000003_pending_reference.up.sql"} {
		b, e := os.ReadFile(filepath.Join(root, "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = conn.Exec(ctx, string(b)); e != nil {
			t.Fatal(e)
		}
	}
	hash := strings.Repeat("b", 64)
	if _, err = conn.Exec(ctx, `INSERT INTO wallets(id,player_id,currency,balance_minor,version) VALUES ('retry-wallet','retry-player','BRL',7500,2)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err = conn.Exec(ctx, `INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,reference_external_id,attempt_count,next_attempt_at) VALUES ('retry-refund','EXTERNAL','retry-refund','retry-provider','retry-key',$1,'retry-player','retry-wallet','retry-game','retry-round','REFUND',2500,'BRL','PENDING_REFERENCE','missing-bet',0,$2)`, hash, now); err != nil {
		t.Fatal(err)
	}
	p, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s := application.ReconcileReferenceService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	var tenthDue time.Time
	for attempt := 0; attempt < 9; attempt++ {
		callAt := now
		worked, e := s.ReconcileOne(ctx, callAt, application.ReconcileReferenceIDs{})
		if e != nil || !worked {
			t.Fatalf("attempt %d worked=%v err=%v", attempt, worked, e)
		}
		var status string
		var count int
		var next time.Time
		if e = conn.QueryRow(ctx, "SELECT status,attempt_count,next_attempt_at FROM wager_transactions WHERE id='retry-refund'").Scan(&status, &count, &next); e != nil {
			t.Fatal(e)
		}
		if status != "PENDING_REFERENCE" || count != attempt+1 || !next.Equal(now.Add(time.Duration(1<<attempt)*time.Second)) {
			t.Fatalf("attempt %d state=%s/%d/%v", attempt, status, count, next)
		}
		var n int
		if e = conn.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='retry-refund'").Scan(&n); e != nil || n != 0 {
			t.Fatalf("ledger=%d err=%v", n, e)
		}
		if e = conn.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE transaction_id='retry-refund'").Scan(&n); e != nil || n != 0 {
			t.Fatalf("outbox=%d err=%v", n, e)
		}
		if attempt < 8 {
			now = next
		} else {
			tenthDue = next
			now = callAt
		}
	}
	worked, err := s.ReconcileOne(ctx, now.Add(500*time.Millisecond), application.ReconcileReferenceIDs{})
	if err != nil || worked {
		t.Fatalf("early tenth worked=%v err=%v", worked, err)
	}
	worked, err = s.ReconcileOne(ctx, tenthDue, application.ReconcileReferenceIDs{})
	if err != nil || !worked {
		t.Fatalf("exhaustion worked=%v err=%v", worked, err)
	}
	var status string
	var failure *string
	var attempts int
	var next *time.Time
	var balance *int64
	var version *int64
	if err = conn.QueryRow(ctx, "SELECT status,failure_code,attempt_count,next_attempt_at,result_balance_minor,result_wallet_version FROM wager_transactions WHERE id='retry-refund'").Scan(&status, &failure, &attempts, &next, &balance, &version); err != nil {
		t.Fatal(err)
	}
	if status != "REJECTED" || failure == nil || *failure != "REFERENCE_NOT_FOUND" || attempts != 10 || next != nil || balance == nil || version == nil || *balance != 7500 || *version != 2 {
		t.Fatalf("terminal=%s/%v/%d/%v/%d/%d", status, failure, attempts, next, balance, version)
	}
	var n int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='retry-refund'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("terminal ledger=%d err=%v", n, err)
	}
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE transaction_id='retry-refund' AND event_type='WagerTransactionRejected' AND id='retry-refund:rejected'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rejected outbox=%d err=%v", n, err)
	}
}
