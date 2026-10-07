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
	defer conn.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE")
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
