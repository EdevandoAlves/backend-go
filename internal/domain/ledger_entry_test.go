package domain

import (
	"errors"
	"testing"
)

func TestWalletLedgerEntryValidatesMath(t *testing.T) {
	before, _ := ParseMoney("10.00", "BRL")
	amount, _ := ParseMoney("2.00", "BRL")
	after, _ := ParseMoney("12.00", "BRL")
	entry, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, before, after)
	if err != nil || entry.Direction() != LedgerCredit {
		t.Fatalf("credit = %#v, %v", entry, err)
	}
	debitAfter, _ := ParseMoney("8.00", "BRL")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerDebit, amount, before, debitAfter); err != nil {
		t.Fatal(err)
	}
	wrong, _ := ParseMoney("11.00", "BRL")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, before, wrong); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("wrong ledger math accepted")
	}
	usd, _ := ParseMoney("1.00", "USD")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, usd, before, after); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("currency mismatch accepted")
	}
}
