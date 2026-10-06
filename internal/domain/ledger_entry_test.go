package domain

import (
	"errors"
	"testing"
	"time"
)

func TestWalletLedgerEntryValidatesMath(t *testing.T) {
	before, _ := ParseMoney("10.00", "BRL")
	amount, _ := ParseMoney("2.00", "BRL")
	after, _ := ParseMoney("12.00", "BRL")
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	entry, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, before, after, now)
	if err != nil || entry.Direction() != LedgerCredit {
		t.Fatalf("credit = %#v, %v", entry, err)
	}
	if !entry.CreatedAt().Equal(now) {
		t.Fatalf("created at = %v, want %v", entry.CreatedAt(), now)
	}
	rehydrated, err := RehydrateWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, before, after, now)
	if err != nil || !rehydrated.CreatedAt().Equal(now) {
		t.Fatalf("rehydrated = %#v, %v", rehydrated, err)
	}
	debitAfter, _ := ParseMoney("8.00", "BRL")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerDebit, amount, before, debitAfter, now); err != nil {
		t.Fatal(err)
	}
	wrong, _ := ParseMoney("11.00", "BRL")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, before, wrong, now); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("wrong ledger math accepted")
	}
	usd, _ := ParseMoney("1.00", "USD")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, usd, before, after, now); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("currency mismatch accepted")
	}
}

func TestWalletLedgerEntryRejectsNegativeBalances(t *testing.T) {
	before, _ := ParseMoney("1.00", "BRL")
	amount, _ := ParseMoney("1.00", "BRL")
	after, _ := ParseMoney("2.00", "BRL")
	negative, _ := NewMoneyForInternal(-1, "BRL")
	zero, _ := Zero("BRL")
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, negative, zero, time.Now()); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("negative before accepted")
	}
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerDebit, amount, amount, negative, time.Now()); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("negative after accepted")
	}
	if _, err := NewWalletLedgerEntry("l", "w", "t", LedgerCredit, amount, before, after, time.Time{}); !errors.Is(err, ErrInvalidLedgerEntry) {
		t.Fatal("zero created at accepted")
	}
}
