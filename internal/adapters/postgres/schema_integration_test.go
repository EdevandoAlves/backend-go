//go:build integration

package postgres

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrations(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || !strings.Contains(parsed.Path, "_test") {
		t.Fatalf("TEST_DATABASE_URL must target a database containing _test")
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
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	reset := func() {
		if _, err := conn.Exec(ctx, string(down)); err != nil {
			t.Fatalf("reset schema: %v", err)
		}
	}
	reset()
	defer reset()
	if _, err := conn.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	t.Run("constraints", func(t *testing.T) { testConstraints(t, conn) })
}

func testConstraints(t *testing.T, conn *pgx.Conn) {
	ctx := context.Background()
	exec := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, sql, args...)
		return err
	}
	mustFail := func(name, state, sql string, args ...any) {
		t.Helper()
		err := exec(sql, args...)
		if err == nil {
			t.Fatalf("%s unexpectedly succeeded", name)
		}
		pgErr, ok := err.(*pgconn.PgError)
		if !ok || pgErr.Code != state {
			t.Fatalf("%s: SQLSTATE %s, got %v", name, state, err)
		}
	}
	hash := strings.Repeat("a", 64)
	if err := exec("INSERT INTO wallets(id,player_id,currency,balance_minor) VALUES ('w1','p1','BRL',0)"); err != nil {
		t.Fatal(err)
	}
	if err := exec("INSERT INTO wallets(id,player_id,currency,balance_minor) VALUES ('w2','p1','USD',0)"); err != nil {
		t.Fatal(err)
	}
	mustFail("blank wallet id", "23514", "INSERT INTO wallets(id,player_id,currency,balance_minor) VALUES (' ','p2','BRL',0)")
	mustFail("negative wallet", "23514", "INSERT INTO wallets(id,player_id,currency,balance_minor) VALUES ('wn','p2','BRL',-1)")
	mustFail("duplicate player currency", "23505", "INSERT INTO wallets(id,player_id,currency,balance_minor) VALUES ('w3','p1','BRL',0)")

	opening := "INSERT INTO wager_transactions(id,origin,player_id,wallet_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('o1','INTERNAL','p1','w1','OPENING',100,'BRL','PROCESSED',100,'BRL',1)"
	if err := exec(opening); err != nil {
		t.Fatal(err)
	}
	mustFail("processed without result", "23514", "INSERT INTO wager_transactions(id,origin,player_id,wallet_id,kind,amount_minor,currency,status) VALUES ('missing-result','INTERNAL','p1','w1','OPENING',100,'BRL','PROCESSED')")
	mustFail("second opening", "23505", "INSERT INTO wager_transactions(id,origin,player_id,wallet_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('o2','INTERNAL','p1','w1','OPENING',100,'BRL','PROCESSED',100,'BRL',1)")
	mustFail("zero opening", "23514", "INSERT INTO wager_transactions(id,origin,player_id,wallet_id,kind,amount_minor,currency,status) VALUES ('o0','INTERNAL','p1','w1','OPENING',0,'BRL','PROCESSED')")
	mustFail("external opening", "23514", "INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status) VALUES ('eo','EXTERNAL','e','pr','k','"+hash+"','p1','w1','g','r','OPENING',100,'BRL','PROCESSED')")
	mustFail("invalid hash", "23514", "INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status) VALUES ('bad','EXTERNAL','e','pr','kb','BAD','p1','w1','g','r','LOSS',0,'BRL','PROCESSED')")
	for _, field := range []string{"provider_id", "external_id", "idempotency_key", "payload_hash", "game_id", "round_id"} {
		columns := "id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version"
		values := "'null-" + field + "','EXTERNAL','e-null','pr-null','k-null','" + hash + "','p1','w1','g','r','LOSS',0,'BRL','PROCESSED',0,'BRL',1"
		literal := map[string]string{"provider_id": "'pr-null'", "external_id": "'e-null'", "idempotency_key": "'k-null'", "payload_hash": "'" + hash + "'", "game_id": "'g'", "round_id": "'r'"}[field]
		values = strings.Replace(values, literal, "NULL", 1)
		mustFail("external NULL "+field, "23514", "INSERT INTO wager_transactions("+columns+") VALUES ("+values+")")
	}
	for _, field := range []string{"result_balance_minor", "result_currency", "result_wallet_version"} {
		columns := "id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version"
		values := "'processed-null-" + field + "','EXTERNAL','e-" + field + "','pr-" + field + "','k-" + field + "','" + hash + "','p1','w1','g','r','LOSS',0,'BRL','PROCESSED',0,'BRL',1"
		switch field {
		case "result_balance_minor":
			values = strings.Replace(values, ",'PROCESSED',0,'BRL',1", ",'PROCESSED',NULL,'BRL',1", 1)
		case "result_currency":
			values = strings.Replace(values, ",'PROCESSED',0,'BRL',1", ",'PROCESSED',0,NULL,1", 1)
		case "result_wallet_version":
			values = strings.Replace(values, ",'PROCESSED',0,'BRL',1", ",'PROCESSED',0,'BRL',NULL", 1)
		}
		mustFail("processed missing "+field, "23514", "INSERT INTO wager_transactions("+columns+") VALUES ("+values+")")
		rejected := strings.Replace(values, "'processed-null-", "'rejected-null-", 1)
		rejected = strings.Replace(rejected, "'PROCESSED'", "'REJECTED'", 1)
		rejected = rejected + ", 'REJECTED_CODE'"
		mustFail("rejected missing "+field, "23514", "INSERT INTO wager_transactions("+columns+",failure_code) VALUES ("+rejected+")")
	}

	insertExternal := func(id, provider, external, key string, status string) {
		t.Helper()
		if err := exec("INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version,failure_code) VALUES ($1,'EXTERNAL',$2,$3,$4,$5,'p1','w1','g','r','LOSS',0,'BRL',$6,0,'BRL',1,CASE WHEN $6='REJECTED' THEN 'NOOP' ELSE NULL END)", id, external, provider, key, hash, status); err != nil {
			t.Fatal(err)
		}
	}
	insertExternal("l1", "pr", "e1", "k1", "PROCESSED")
	mustFail("same idempotency", "23505", "INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('l2','EXTERNAL','e2','pr','k1','"+hash+"','p1','w1','g','r','LOSS',0,'BRL','PROCESSED',0,'BRL',1)")
	mustFail("same external", "23505", "INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('l3','EXTERNAL','e1','pr','k3','"+hash+"','p1','w1','g','r','LOSS',0,'BRL','PROCESSED',0,'BRL',1)")
	insertExternal("l4", "other", "e1", "k4", "PROCESSED")
	insertExternal("l5", "other", "e5", "k1", "PROCESSED")
	if err := exec("INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,result_balance_minor,result_currency,result_wallet_version) VALUES ('l6','EXTERNAL','e6','other','k6','" + hash + "','p1','w2','g','r','LOSS',0,'USD','PROCESSED',0,'USD',1)"); err != nil {
		t.Fatal(err)
	}
	if err := exec("INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status) VALUES ('l7','EXTERNAL','e7','other','k7','" + hash + "','p1','w1','g','r','BET',1,'BRL','PENDING')"); err != nil {
		t.Fatal(err)
	}
	if err := exec("INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,reference_external_id,next_attempt_at) VALUES ('l8','EXTERNAL','e8','other','k8','" + hash + "','p1','w1','g','r','REFUND',1,'BRL','PENDING_REFERENCE','e7',now())"); err != nil {
		t.Fatal(err)
	}
	if err := exec("UPDATE wager_transactions SET reference_transaction_id='l7',status='PROCESSED',result_balance_minor=1,result_currency='BRL',result_wallet_version=2,next_attempt_at=NULL WHERE id='l8'"); err != nil {
		t.Fatal(err)
	}

	if err := exec("INSERT INTO wallet_ledger_entries(id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor) VALUES ('led','w1','o1','BRL','CREDIT',100,0,100)"); err != nil {
		t.Fatal(err)
	}
	mustFail("ledger math", "23514", "INSERT INTO wallet_ledger_entries(id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor) VALUES ('bad','w1','o1','BRL','CREDIT',10,0,5)")
	mustFail("ledger negative", "23514", "INSERT INTO wallet_ledger_entries(id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor) VALUES ('bad2','w1','o1','BRL','DEBIT',10,0,-10)")
	mustFail("ledger wallet currency FK", "23503", "INSERT INTO wallet_ledger_entries(id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor) VALUES ('bad3','w2','l6','BRL','CREDIT',10,0,10)")
	mustFail("ledger transaction FK", "23503", "INSERT INTO wallet_ledger_entries(id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor) VALUES ('bad4','w1','l6','BRL','CREDIT',10,0,10)")
	mustFail("ledger update", "55000", "UPDATE wallet_ledger_entries SET after_minor=101 WHERE id='led'")
	mustFail("ledger delete", "55000", "DELETE FROM wallet_ledger_entries WHERE id='led'")

	mustFail("identity update", "55000", "UPDATE wager_transactions SET external_id='changed' WHERE id='l1'")
	mustFail("reference fill from pending", "55000", "UPDATE wager_transactions SET reference_transaction_id='l6',status='PROCESSED',result_balance_minor=1,result_currency='BRL',result_wallet_version=2,next_attempt_at=NULL WHERE id='l7'")
	mustFail("reference change after terminal", "55000", "UPDATE wager_transactions SET reference_transaction_id='l6' WHERE id='l8'")
	mustFail("terminal update", "55000", "UPDATE wager_transactions SET amount_minor=1 WHERE id='o1'")

	if err := exec("INSERT INTO inbox_messages(consumer_name,message_id,payload_hash) VALUES ('worker','m1','" + hash + "')"); err != nil {
		t.Fatal(err)
	}
	mustFail("inbox duplicate", "23505", "INSERT INTO inbox_messages(consumer_name,message_id,payload_hash) VALUES ('worker','m1','"+hash+"')")
	mustFail("inbox hash", "23514", "INSERT INTO inbox_messages(consumer_name,message_id,payload_hash) VALUES ('worker','m2','bad')")
	mustFail("inbox incomplete completion", "23514", "UPDATE inbox_messages SET completed_at=now() WHERE message_id='m1'")
	if err := exec("UPDATE inbox_messages SET transaction_id='l1',completed_at=now() WHERE message_id='m1'"); err != nil {
		t.Fatal(err)
	}

	if err := exec("INSERT INTO outbox_events(id,aggregate_id,transaction_id,event_type,event_version,correlation_id,payload) VALUES ('ev','wallet','o1','WagerTransactionProcessed',1,'corr','{}')"); err != nil {
		t.Fatal(err)
	}
	mustFail("outbox duplicate", "23505", "INSERT INTO outbox_events(id,aggregate_id,transaction_id,event_type,event_version,correlation_id,payload) VALUES ('ev2','wallet','o1','WagerTransactionProcessed',1,'corr','{}')")
	mustFail("outbox snapshot", "55000", "UPDATE outbox_events SET payload='{\"changed\":true}' WHERE id='ev'")
	if err := exec("UPDATE outbox_events SET attempts=1,last_error='retry',claimed_by='worker',claim_expires_at=now() WHERE id='ev'"); err != nil {
		t.Fatal(err)
	}
}
