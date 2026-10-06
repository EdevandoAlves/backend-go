package domain

import (
	"errors"
	"testing"
	"time"
)

func TestWagerTransactionStateMachine(t *testing.T) {
	now := time.Now()
	amount, _ := ParseMoney("1.00", "BRL")
	tx, err := CreateWagerTransaction("id", "ext", "provider", "player", "wallet", TransactionBet, amount, now)
	if err != nil || tx.Status() != TransactionPending {
		t.Fatalf("create = %#v, %v", tx, err)
	}
	if err := tx.Transition(TransactionPendingReference, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Transition(TransactionProcessed, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Transition(TransactionRejected, now); !errors.Is(err, ErrTerminalTransaction) {
		t.Fatal("terminal transition not rejected")
	}
	rehydrated, err := RehydrateWagerTransaction("id", "ext", "provider", "player", "wallet", TransactionBet, amount, TransactionProcessed, "", now, now)
	if err != nil || rehydrated.Status() != TransactionProcessed {
		t.Fatal("rehydration failed")
	}
}

func TestWagerTransactionInvalidTransition(t *testing.T) {
	amount, _ := ParseMoney("1.00", "BRL")
	tx, _ := CreateWagerTransaction("id", "ext", "provider", "player", "wallet", TransactionBet, amount, time.Now())
	if err := tx.Transition(TransactionPending, time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("invalid transition accepted")
	}
}
