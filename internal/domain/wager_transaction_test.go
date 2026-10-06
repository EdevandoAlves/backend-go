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

func TestOpeningWagerTransactionUsesInternalIdentity(t *testing.T) {
	now := time.Now()
	amount, _ := ParseMoney("10.00", "BRL")
	tx, err := CreateOpeningWagerTransaction("opening-1", "player", "wallet", amount, now)
	if err != nil {
		t.Fatal(err)
	}
	if tx.ID() != "opening-1" || tx.PlayerID() != "player" || tx.WalletID() != "wallet" || tx.Status() != TransactionProcessed {
		t.Fatalf("opening = %#v", tx)
	}
	if tx.ExternalID() != "" || tx.ProviderID() != "" || tx.ReferenceExternalID() != "" || tx.Type() != TransactionOpening {
		t.Fatalf("opening has external identity: %#v", tx)
	}
}

func TestOpeningWagerTransactionRejectsZeroAmount(t *testing.T) {
	zero, _ := Zero("BRL")
	if _, err := CreateOpeningWagerTransaction("opening-1", "player", "wallet", zero, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero opening accepted: %v", err)
	}
}

func TestExternalWagerTransactionRejectsOpening(t *testing.T) {
	amount, _ := ParseMoney("1.00", "BRL")
	if _, err := CreateWagerTransaction("id", "ext", "provider", "player", "wallet", TransactionOpening, amount, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("opening accepted by external constructor: %v", err)
	}
}

func TestExternalWagerTransactionAmountRules(t *testing.T) {
	zero, _ := Zero("BRL")
	positive, _ := ParseMoney("1.00", "BRL")
	if _, err := CreateWagerTransaction("loss", "ext", "provider", "player", "wallet", TransactionLoss, zero, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWagerTransaction("loss", "ext", "provider", "player", "wallet", TransactionLoss, positive, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatal("positive LOSS accepted")
	}
	for _, kind := range []WagerTransactionType{TransactionBet, TransactionWin, TransactionRefund, TransactionRollback} {
		if _, err := CreateWagerTransaction("id", "ext", "provider", "player", "wallet", kind, zero, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
			t.Fatalf("zero %s accepted", kind)
		}
		if _, err := CreateWagerTransaction("id", "ext", "provider", "player", "wallet", kind, positive, time.Now()); err != nil {
			t.Fatalf("positive %s rejected: %v", kind, err)
		}
	}
}

func TestRehydrateOpeningWagerTransaction(t *testing.T) {
	now := time.Now()
	amount, _ := ParseMoney("10.00", "BRL")
	tx, err := RehydrateWagerTransaction("opening-1", "", "", "player", "wallet", TransactionOpening, amount, TransactionProcessed, "", now, now)
	if err != nil || tx.Type() != TransactionOpening || tx.Status() != TransactionProcessed || tx.Amount() != amount {
		t.Fatalf("rehydrate opening = %#v, %v", tx, err)
	}
}

func TestRehydrateOpeningRejectsZeroAmount(t *testing.T) {
	zero, _ := Zero("BRL")
	if _, err := RehydrateWagerTransaction("opening-zero", "", "", "player", "wallet", TransactionOpening, zero, TransactionProcessed, "", time.Now(), time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero opening accepted during rehydration: %v", err)
	}
}

func TestRehydrateOpeningRejectsExternalFieldsAndNonProcessedStatus(t *testing.T) {
	amount, _ := ParseMoney("10.00", "BRL")
	for _, tc := range []struct {
		name, externalID, providerID, reference string
		status                                  WagerTransactionStatus
	}{
		{"external id", "external", "", "", TransactionProcessed},
		{"provider id", "", "provider", "", TransactionProcessed},
		{"reference", "", "", "reference", TransactionProcessed},
		{"pending", "", "", "", TransactionPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RehydrateWagerTransaction("opening-1", tc.externalID, tc.providerID, "player", "wallet", TransactionOpening, amount, tc.status, tc.reference, time.Now(), time.Now()); !errors.Is(err, ErrInvalidTransaction) {
				t.Fatalf("invalid opening accepted: %v", err)
			}
		})
	}
}

func TestWagerTransactionRejectsUninitializedMoney(t *testing.T) {
	var amount Money
	if _, err := CreateWagerTransaction("id", "ext", "provider", "player", "wallet", TransactionBet, amount, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero value money accepted by external constructor: %v", err)
	}
	if _, err := CreateOpeningWagerTransaction("opening-1", "player", "wallet", amount, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero value money accepted by opening constructor: %v", err)
	}
	if _, err := RehydrateWagerTransaction("id", "ext", "provider", "player", "wallet", TransactionBet, amount, TransactionProcessed, "", time.Now(), time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero value money accepted by rehydration: %v", err)
	}
}
