//go:build integration

package sqs

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
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestConsumerAtomicCommitAndRollback(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	root, _ := filepath.Abs("../../../")
	up1, _ := os.ReadFile(filepath.Join(root, "migrations/000001_schema.up.sql"))
	up2, _ := os.ReadFile(filepath.Join(root, "migrations/000002_wager_currency.up.sql"))
	up3, _ := os.ReadFile(filepath.Join(root, "migrations/000003_pending_reference.up.sql"))
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reset := func() {
		if _, err := db.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, string(up1)+string(up2)+string(up3)); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer func() { _, _ = db.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE") }()

	for _, tc := range []struct {
		name string
		fail bool
	}{{"commit", false}, {"rollback", true}} {
		t.Run(tc.name, func(t *testing.T) {
			reset()
			seedConsumerWallet(t, db)
			p, err := postgres.NewPool(ctx, url)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			s := application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
			body := `{"messageId":"sqs-message","version":1,"type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00Z","data":{"providerId":"provider-a","externalTransactionId":"sqs-external","idempotencyKey":"sqs-key","playerId":"player","walletId":"wallet","roundId":"round","gameId":"game","kind":"BET","money":{"amount":"25.00","currency":"BRL"},"referenceExternalTransactionId":null}}`
			id := "sqs-message"
			consumer := Consumer{Service: s, Inbox: postgres.InboxRepository{}}
			if tc.fail {
				consumer.AfterProcessWager = func() error { return errors.New("injected before inbox completion") }
			}
			deleted, processErr := consumer.ProcessMessage(ctx, types.Message{Body: &body, MessageId: &id})
			if tc.fail {
				if processErr == nil || deleted {
					t.Fatalf("failure deleted=%v err=%v", deleted, processErr)
				}
			} else if processErr != nil || !deleted {
				t.Fatalf("deleted=%v err=%v", deleted, processErr)
			}
			var inbox, wagers, ledger, outbox int
			for query, target := range map[string]*int{"SELECT count(*) FROM inbox_messages": &inbox, "SELECT count(*) FROM wager_transactions WHERE origin='EXTERNAL'": &wagers, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='sqs-message'": &ledger, "SELECT count(*) FROM outbox_events WHERE transaction_id='sqs-message'": &outbox} {
				if err := db.QueryRow(ctx, query).Scan(target); err != nil {
					t.Fatal(err)
				}
			}
			if tc.fail {
				if inbox != 0 || wagers != 0 || ledger != 0 || outbox != 0 {
					t.Fatalf("partial rollback inbox=%d wagers=%d ledger=%d outbox=%d", inbox, wagers, ledger, outbox)
				}
			} else if inbox != 1 || wagers != 1 || ledger != 1 || outbox != 2 {
				t.Fatalf("commit counts inbox=%d wagers=%d ledger=%d outbox=%d", inbox, wagers, ledger, outbox)
			}
			var balance int64
			if err := db.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet'").Scan(&balance); err != nil {
				t.Fatal(err)
			}
			if tc.fail && balance != 10000 || !tc.fail && balance != 7500 {
				t.Fatalf("balance=%d", balance)
			}
		})
	}
}

func TestConsumerCompletedReplayAndPoisonMessageAreDeletedWithoutFinancialReplay(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" || !strings.Contains(url, "_test") {
		t.Fatal("TEST_DATABASE_URL must target a _test database")
	}
	root, err := filepath.Abs("../../../")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	up := make([]byte, 0)
	for _, name := range []string{"000001_schema.up.sql", "000002_wager_currency.up.sql", "000003_pending_reference.up.sql"} {
		migration, readErr := os.ReadFile(filepath.Join(root, "migrations", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		up = append(up, migration...)
	}
	if _, err = db.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE") }()
	if _, err = db.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	seedConsumerWallet(t, db)
	p, err := postgres.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	service := application.ProcessWagerService{Manager: postgres.NewTxManager(p), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
	consumer := Consumer{Service: service, Inbox: postgres.InboxRepository{}}
	body := `{"messageId":"replay-message","version":1,"type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00Z","data":{"providerId":"provider-a","externalTransactionId":"replay-external","idempotencyKey":"replay-key","playerId":"player","walletId":"wallet","roundId":"round","gameId":"game","kind":"BET","money":{"amount":"25.00","currency":"BRL"},"referenceExternalTransactionId":null}}`
	id := "replay-message"
	msg := types.Message{Body: &body, MessageId: &id}
	if deleted, err := consumer.ProcessMessage(ctx, msg); err != nil || !deleted {
		t.Fatalf("initial message deleted=%v err=%v", deleted, err)
	}
	var before struct {
		inbox, wagers, ledger, outbox int
		balance                       int64
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM inbox_messages").Scan(&before.inbox); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM wager_transactions WHERE origin='EXTERNAL'").Scan(&before.wagers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='replay-message'").Scan(&before.ledger); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE transaction_id='replay-message'").Scan(&before.outbox); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet'").Scan(&before.balance); err != nil {
		t.Fatal(err)
	}
	if deleted, err := consumer.ProcessMessage(ctx, msg); err != nil || !deleted {
		t.Fatalf("completed replay deleted=%v err=%v", deleted, err)
	}
	var after struct {
		inbox, wagers, ledger, outbox int
		balance                       int64
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM inbox_messages").Scan(&after.inbox); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM wager_transactions WHERE origin='EXTERNAL'").Scan(&after.wagers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM wallet_ledger_entries WHERE transaction_id='replay-message'").Scan(&after.ledger); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM outbox_events WHERE transaction_id='replay-message'").Scan(&after.outbox); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet'").Scan(&after.balance); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("completed replay changed financial state: before=%+v after=%+v", before, after)
	}
	poisonBody := strings.Replace(body, "replay-external", "different-external", 1)
	poison := types.Message{Body: &poisonBody, MessageId: &id}
	deleted, err := consumer.ProcessMessage(ctx, poison)
	if !deleted || !errors.Is(err, postgres.ErrInboxPayloadConflict) {
		t.Fatalf("poison message deleted=%v err=%v", deleted, err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM wager_transactions WHERE origin='EXTERNAL'").Scan(&after.wagers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT balance_minor FROM wallets WHERE id='wallet'").Scan(&after.balance); err != nil {
		t.Fatal(err)
	}
	if after.wagers != before.wagers || after.balance != before.balance {
		t.Fatalf("poison message changed financial state: before=%+v after=%+v", before, after)
	}
}

func seedConsumerWallet(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	amount, _ := domain.ParseMoney("100.00", "BRL")
	zero, _ := domain.Zero("BRL")
	err := func() error {
		tx, err := db.Begin(context.Background())
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		w, err := domain.CreateWallet("wallet", "player", amount, now)
		if err != nil {
			return err
		}
		if err = (postgres.WalletRepository{}).Insert(context.Background(), tx, w); err != nil {
			return err
		}
		opening, err := domain.CreateOpeningWagerTransaction("opening-wallet", "player", "wallet", amount, now)
		if err != nil {
			return err
		}
		if err = (postgres.TransactionRepository{}).InsertOpening(context.Background(), tx, opening, amount, 1); err != nil {
			return err
		}
		entry, err := domain.NewWalletLedgerEntry("opening-ledger", "wallet", "opening-wallet", domain.LedgerCredit, amount, zero, amount, now)
		if err != nil {
			return err
		}
		if err = (postgres.LedgerRepository{}).Insert(context.Background(), tx, entry); err != nil {
			return err
		}
		if err = (postgres.OutboxRepository{}).Insert(context.Background(), tx, application.OutboxEvent{ID: "opening-event", AggregateID: "wallet", TransactionID: "opening-wallet", EventType: "WagerTransactionProcessed", CorrelationID: "opening", EventVersion: 1, Payload: []byte(`{"opening":true}`)}); err != nil {
			return err
		}
		return tx.Commit(context.Background())
	}()
	if err != nil {
		t.Fatal(err)
	}
}

var _ pgx.Tx
