//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPendingReferenceFoundation(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
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
	defer conn.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public")
	root := "../../../migrations/"
	for _, name := range []string{"000001_schema.up.sql", "000002_wager_currency.up.sql", "000003_pending_reference.up.sql"} {
		contents, readErr := os.ReadFile(root + name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = conn.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err = conn.Exec(ctx, "INSERT INTO wallets(id,player_id,currency,balance_minor) VALUES ('w','p','BRL',0)"); err != nil {
		t.Fatal(err)
	}
	insert := func(id, kind, status, ref, failure string, attempts int, next any) {
		t.Helper()
		_, e := conn.Exec(ctx, `INSERT INTO wager_transactions
			(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,failure_code,reference_external_id,reference_transaction_id,attempt_count,next_attempt_at)
			VALUES ($1,'EXTERNAL',$2,$3,$4,$5,'p','w','g','r',$6,1,'BRL',$7,$8,$9,$10,$11,$12)`, id, id, "pr-"+id, "key-"+id, hash, kind, status, nullIfEmpty(failure), "original", nullIfEmpty(ref), attempts, next)
		if e != nil {
			t.Fatalf("insert %s: %v", id, e)
		}
	}
	insert("pending-refund", "REFUND", "PENDING", "", "", 0, nil)
	insert("pending-refund-2", "REFUND", "PENDING_REFERENCE", "", "", 2, time.Now())
	if _, err = conn.Exec(ctx, "UPDATE wager_transactions SET reference_transaction_id='pending-refund', status='PROCESSED', result_balance_minor=1, result_currency='BRL', result_wallet_version=1, next_attempt_at=NULL WHERE id='pending-refund-2'"); err != nil {
		t.Fatal(err)
	}
	insert("not-found", "REFUND", "PENDING_REFERENCE", "", "", 9, time.Now())
	if _, err = conn.Exec(ctx, `UPDATE wager_transactions
		SET status = 'REJECTED',
			failure_code = 'REFERENCE_NOT_FOUND',
			result_balance_minor = 0,
			result_currency = 'BRL',
			result_wallet_version = 1,
			attempt_count = 10,
			next_attempt_at = NULL
		WHERE id = 'not-found'`); err != nil {
		t.Fatal("expire pending reference:", err)
	}
	mustState := func(name, state, sql string, args ...any) {
		t.Helper()
		_, e := conn.Exec(ctx, sql, args...)
		if e == nil {
			t.Fatalf("%s unexpectedly succeeded", name)
		}
		if pe, ok := e.(*pgconn.PgError); !ok || pe.Code != state {
			t.Fatalf("%s: got %v, want %s", name, e, state)
		}
	}
	mustState("missing schedule", "23514", "UPDATE wager_transactions SET status='PENDING_REFERENCE' WHERE id='pending-refund'")
	mustState("terminal next attempt", "23514", "UPDATE wager_transactions SET status='PROCESSED', result_balance_minor=1, result_currency='BRL', result_wallet_version=1, next_attempt_at=now() WHERE id='pending-refund'")
	mustState("normal rejection without reference", "23514", "UPDATE wager_transactions SET status='REJECTED', failure_code='NOOP', result_balance_minor=1, result_currency='BRL', result_wallet_version=1 WHERE id='pending-refund'")
	mustState("second reference", "55000", "UPDATE wager_transactions SET reference_transaction_id='pending-refund-2' WHERE id='pending-refund-2'")
	mustState("negative attempts", "23514", "UPDATE wager_transactions SET attempt_count=-1 WHERE id='pending-refund'")

	claim1, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer claim1.Close(ctx)
	claim2, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer claim2.Close(ctx)
	tx1, err := claim1.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback(ctx)
	if _, err = tx1.Exec(ctx, "SELECT id FROM wager_transactions WHERE status='PENDING' FOR UPDATE SKIP LOCKED"); err != nil {
		t.Fatal(err)
	}
	tx2, err := claim2.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback(ctx)
	var claimed string
	if err = tx2.QueryRow(ctx, "SELECT id FROM wager_transactions WHERE status='PENDING' FOR UPDATE SKIP LOCKED").Scan(&claimed); err != pgx.ErrNoRows {
		t.Fatalf("second claim got %q, %v", claimed, err)
	}
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
