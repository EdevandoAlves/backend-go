//go:build integration

package application_test

import (
	"context"
	"encoding/json"
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

func TestOpenWalletIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	root, _ := filepath.Abs("../../")
	var migrations []string
	for _, name := range []string{"000001_schema.up.sql", "000002_wager_currency.up.sql", "000003_pending_reference.up.sql"} {
		b, readErr := os.ReadFile(filepath.Join(root, "migrations", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		migrations = append(migrations, string(b))
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public; SET search_path TO public"); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations {
		if _, err = pool.Exec(ctx, migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, "SET search_path TO public"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Close()
		cleanup, e := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
		if e == nil {
			_, _ = cleanup.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public")
			cleanup.Close()
		}
	}()
	p, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s := application.OpenWalletService{Manager: postgres.NewTxManager(p), Wallets: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	positive := domainMoney(t, "10.00")
	cmd := application.OpenWalletCommand{WalletID: "ow1", PlayerID: "op1", InitialBalance: positive, Now: time.Now().UTC(), OpeningID: "opening-ow1", LedgerID: "ledger-ow1", ProcessedEventID: "event-ow1", BalanceEventID: "balance-ow1"}
	if err := s.Execute(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id='ow1' AND balance_minor=1000 AND version=1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("wallet=%d err=%v", n, err)
	}
	for _, table := range []string{"wager_transactions", "wallet_ledger_entries", "outbox_events"} {
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE "+map[string]string{"wager_transactions": "wallet_id='ow1'", "wallet_ledger_entries": "wallet_id='ow1'", "outbox_events": "aggregate_id='ow1'"}[table]).Scan(&n); err != nil || n != map[string]int{"wager_transactions": 1, "wallet_ledger_entries": 1, "outbox_events": 2}[table] {
			t.Fatalf("%s=%d err=%v", table, n, err)
		}
	}
	rows, err := pool.Query(ctx, "SELECT event_type, payload FROM outbox_events WHERE aggregate_id='ow1' ORDER BY event_type")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]map[string]any{}
	for rows.Next() {
		var eventType string
		var payload []byte
		if err := rows.Scan(&eventType, &payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), "10.00 BRL") {
			t.Fatalf("legacy money format in %s: %s", eventType, payload)
		}
		var fields map[string]any
		if err := json.Unmarshal(payload, &fields); err != nil {
			t.Fatal(err)
		}
		seen[eventType] = fields
	}
	processed := seen["WagerTransactionProcessed"]
	if processed["transactionId"] != "opening-ow1" || processed["walletId"] != "ow1" || processed["state"] != "PROCESSED" || processed["operationType"] != "OPENING" {
		t.Fatalf("processed payload=%v", processed)
	}
	if processed["money"].(map[string]any)["amount"] != "10.00" || processed["money"].(map[string]any)["currency"] != "BRL" {
		t.Fatalf("processed money=%v", processed["money"])
	}
	if seen["WalletBalanceChanged"]["direction"] != "CREDIT" {
		t.Fatalf("balance payload=%v", seen["WalletBalanceChanged"])
	}
	zero := application.OpenWalletCommand{WalletID: "ow0", PlayerID: "op0", InitialBalance: domainMoney(t, "0.00"), Now: time.Now().UTC()}
	if err := s.Execute(ctx, zero); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wager_transactions WHERE wallet_id='ow0'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("zero records=%d err=%v", n, err)
	}
	if err := s.Execute(ctx, cmd); !errors.Is(err, postgres.ErrConflict) {
		t.Fatalf("duplicate=%v", err)
	}
	failed := cmd
	failed.WalletID = "ow2"
	failed.PlayerID = "op2"
	failed.OpeningID = "opening-ow2"
	failed.LedgerID = "ledger-ow2"
	failed.ProcessedEventID = "event-ow2"
	failed.BalanceEventID = "balance-ow2"
	s.AfterLedger = func() error { return errors.New("injected") }
	if err := s.Execute(ctx, failed); err == nil {
		t.Fatal("injected error accepted")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id='ow2'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("rollback count=%d err=%v", n, err)
	}
}

func domainMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	m, err := domain.ParseMoney(value, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	return m
}
