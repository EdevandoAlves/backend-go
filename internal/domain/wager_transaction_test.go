package domain

import (
	"errors"
	"testing"
	"time"
)

func TestWagerTransactionStateMachine(t *testing.T) {
	now := time.Now()
	amount, _ := ParseMoney("1.00", "BRL")
	tx, err := CreateExternalWagerTransaction(WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "a", GameID: "game", RoundID: "round", Kind: TransactionBet, Amount: amount}, now)
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
	rehydrated, err := RehydrateExternalWagerTransaction(RehydratedWagerTransaction{WagerTransactionInput: WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "a", GameID: "game", RoundID: "round", Kind: TransactionBet, Amount: amount}, Status: TransactionProcessed, Result: &WagerTransactionResult{Balance: amount, WalletVersion: 1}, CreatedAt: now, UpdatedAt: now})
	if err != nil || rehydrated.Status() != TransactionProcessed {
		t.Fatal("rehydration failed")
	}
}

func TestWagerTransactionInvalidTransition(t *testing.T) {
	amount, _ := ParseMoney("1.00", "BRL")
	tx, _ := CreateExternalWagerTransaction(WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "a", GameID: "game", RoundID: "round", Kind: TransactionBet, Amount: amount}, time.Now())
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
	result, ok := tx.Result()
	if !ok || result.Balance != amount || result.WalletVersion != 1 {
		t.Fatalf("opening result = %#v, %v", result, ok)
	}
}

func TestRehydrateRequiresTerminalResult(t *testing.T) {
	now := time.Now()
	amount, _ := ParseMoney("1.00", "BRL")
	input := WagerTransactionInput{ID: "terminal", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "hash", GameID: "game", RoundID: "round", Kind: TransactionWin, Amount: amount}
	if _, err := RehydrateExternalWagerTransaction(RehydratedWagerTransaction{WagerTransactionInput: input, Status: TransactionProcessed, CreatedAt: now, UpdatedAt: now}); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("processed without result accepted: %v", err)
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
	if _, err := CreateExternalWagerTransaction(WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "a", GameID: "game", RoundID: "round", Kind: TransactionOpening, Amount: amount}, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("opening accepted by external constructor: %v", err)
	}
}

func TestExternalWagerTransactionAmountRules(t *testing.T) {
	zero, _ := Zero("BRL")
	positive, _ := ParseMoney("1.00", "BRL")
	base := WagerTransactionInput{ID: "loss", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "a", GameID: "game", RoundID: "round", Kind: TransactionLoss, Amount: zero}
	if _, err := CreateExternalWagerTransaction(base, time.Now()); err != nil {
		t.Fatal(err)
	}
	base.Amount = positive
	if _, err := CreateExternalWagerTransaction(base, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatal("positive LOSS accepted")
	}
	for _, kind := range []WagerTransactionType{TransactionBet, TransactionWin} {
		base.ID, base.Kind, base.Amount = "id", kind, zero
		if _, err := CreateExternalWagerTransaction(base, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
			t.Fatalf("zero %s accepted", kind)
		}
		base.Amount = positive
		if _, err := CreateExternalWagerTransaction(base, time.Now()); err != nil {
			t.Fatalf("positive %s rejected: %v", kind, err)
		}
	}
	for _, kind := range []WagerTransactionType{TransactionRefund, TransactionRollback} {
		if _, err := CreateExternalWagerTransaction(WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "hash", GameID: "game", RoundID: "round", Kind: kind, Amount: positive, ReferenceExternalID: "reference"}, time.Now()); err != nil {
			t.Fatalf("positive %s rejected: %v", kind, err)
		}
	}
}

func TestRehydrateOpeningWagerTransaction(t *testing.T) {
	now := time.Now()
	amount, _ := ParseMoney("10.00", "BRL")
	tx, err := RehydrateExternalWagerTransaction(RehydratedWagerTransaction{WagerTransactionInput: WagerTransactionInput{ID: "opening-1", PlayerID: "player", WalletID: "wallet", Kind: TransactionOpening, Amount: amount}, Status: TransactionProcessed, Result: &WagerTransactionResult{Balance: amount, WalletVersion: 1}, CreatedAt: now, UpdatedAt: now})
	if err != nil || tx.Type() != TransactionOpening || tx.Status() != TransactionProcessed || tx.Amount() != amount {
		t.Fatalf("rehydrate opening = %#v, %v", tx, err)
	}
}

func TestRehydrateOpeningRejectsZeroAmount(t *testing.T) {
	zero, _ := Zero("BRL")
	if _, err := RehydrateExternalWagerTransaction(RehydratedWagerTransaction{WagerTransactionInput: WagerTransactionInput{ID: "opening-zero", PlayerID: "player", WalletID: "wallet", Kind: TransactionOpening, Amount: zero}, Status: TransactionProcessed, CreatedAt: time.Now(), UpdatedAt: time.Now()}); !errors.Is(err, ErrInvalidTransaction) {
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
			if _, err := RehydrateExternalWagerTransaction(RehydratedWagerTransaction{WagerTransactionInput: WagerTransactionInput{ID: "opening-1", ExternalID: tc.externalID, ProviderID: tc.providerID, PlayerID: "player", WalletID: "wallet", Kind: TransactionOpening, Amount: amount, ReferenceExternalID: tc.reference}, Status: tc.status, CreatedAt: time.Now(), UpdatedAt: time.Now()}); !errors.Is(err, ErrInvalidTransaction) {
				t.Fatalf("invalid opening accepted: %v", err)
			}
		})
	}
}

func TestWagerTransactionRejectsUninitializedMoney(t *testing.T) {
	var amount Money
	if _, err := CreateExternalWagerTransaction(WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "a", GameID: "game", RoundID: "round", Kind: TransactionBet, Amount: amount}, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero value money accepted by external constructor: %v", err)
	}
	if _, err := CreateOpeningWagerTransaction("opening-1", "player", "wallet", amount, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero value money accepted by opening constructor: %v", err)
	}
	if _, err := RehydrateExternalWagerTransaction(RehydratedWagerTransaction{WagerTransactionInput: WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "a", GameID: "game", RoundID: "round", Kind: TransactionBet, Amount: amount}, Status: TransactionProcessed, CreatedAt: time.Now(), UpdatedAt: time.Now()}); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("zero value money accepted by rehydration: %v", err)
	}
}

func TestExternalWagerTransactionRehydratesAllFields(t *testing.T) {
	now := time.Now()
	amount, _ := ParseMoney("1.00", "BRL")
	balance, _ := ParseMoney("11.00", "BRL")
	input := WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "hash", GameID: "game", RoundID: "round", Kind: TransactionWin, Amount: amount, ReferenceExternalID: "ref"}
	original, err := CreateExternalWagerTransaction(input, now)
	if err != nil {
		t.Fatal(err)
	}
	rehydrated, err := RehydrateExternalWagerTransaction(RehydratedWagerTransaction{
		WagerTransactionInput: input, Status: TransactionProcessed, ReferenceTransactionID: "reference-id", FailureCode: "", Result: &WagerTransactionResult{Balance: balance, WalletVersion: 7}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if original.ID() != rehydrated.ID() || rehydrated.IdempotencyKey() != "idem" || rehydrated.PayloadHash() != "hash" || rehydrated.GameID() != "game" || rehydrated.RoundID() != "round" || rehydrated.ReferenceExternalID() != "ref" || rehydrated.ReferenceTransactionID() != "reference-id" || rehydrated.FailureCode() != "" || rehydrated.Status() != TransactionProcessed {
		t.Fatalf("fields lost during rehydration: %#v", rehydrated)
	}
	result, ok := rehydrated.Result()
	if !ok || result.Balance != balance || result.WalletVersion != 7 {
		t.Fatalf("result lost during rehydration: %#v, %v", result, ok)
	}
}

func TestExternalReferenceRules(t *testing.T) {
	amount, _ := ParseMoney("1.00", "BRL")
	base := WagerTransactionInput{ID: "id", ExternalID: "ext", ProviderID: "provider", PlayerID: "player", WalletID: "wallet", IdempotencyKey: "idem", PayloadHash: "hash", GameID: "game", RoundID: "round", Amount: amount}
	for _, kind := range []WagerTransactionType{TransactionRefund, TransactionRollback} {
		base.Kind = kind
		if _, err := CreateExternalWagerTransaction(base, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
			t.Fatalf("%s accepted without reference", kind)
		}
	}
	base.Kind, base.ReferenceExternalID = TransactionWin, ""
	if _, err := CreateExternalWagerTransaction(base, time.Now()); err != nil {
		t.Fatalf("WIN without reference rejected: %v", err)
	}
	for _, kind := range []WagerTransactionType{TransactionBet, TransactionLoss} {
		base.Kind, base.ReferenceExternalID = kind, "reference"
		if _, err := CreateExternalWagerTransaction(base, time.Now()); !errors.Is(err, ErrInvalidTransaction) {
			t.Fatalf("%s accepted with reference", kind)
		}
	}
}
