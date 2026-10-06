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

func TestWalletLedgerEntryRejectsNegativeBalances(t *testing.T) {
	amount, _ := ParseMoney("1.00", "BRL")
	negative, _ := NewMoneyForInternal(-1, "BRL")
	zero, _ := Zero("BRL")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, negative, zero); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("negative before accepted")
	}
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerDebit, amount, amount, negative); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("negative after accepted")
	}
}
