package domain

import (
	"errors"
	"testing"
	"time"
)

func TestCreateWallet(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	balance, err := ParseMoney("25.00", "BRL")
	if err != nil {
		t.Fatal(err)
	}
	wallet, err := CreateWallet("wallet-1", "player-1", balance, now)
	if err != nil {
		t.Fatalf("CreateWallet returned error: %v", err)
	}
	if wallet.ID() != "wallet-1" || wallet.PlayerID() != "player-1" || wallet.Balance().MinorUnits() != 2500 || wallet.Version() != 1 {
		t.Fatalf("unexpected wallet: %#v", wallet)
	}
}

func TestWalletCreationRejectsNegativeBalance(t *testing.T) {
	negative, _ := NewMoneyForInternal(-1, "BRL")
	now := time.Now()
	if _, err := CreateWallet("wallet-1", "player-1", negative, now); !errors.Is(err, ErrInvalidWallet) {
		t.Fatalf("CreateWallet error = %v, want ErrInvalidWallet", err)
	}
	if _, err := RehydrateWallet("wallet-1", "player-1", "BRL", negative, 1, now, now); !errors.Is(err, ErrInvalidWallet) {
		t.Fatalf("RehydrateWallet error = %v, want ErrInvalidWallet", err)
	}
}

func TestWalletRehydrateAndBalanceOperations(t *testing.T) {
	now := time.Now()
	balance, _ := ParseMoney("10.00", "BRL")
	wallet, _ := CreateWallet("w", "p", balance, now)
	zero, _ := Zero("BRL")
	if err := wallet.Credit(zero, now.Add(time.Minute)); err != nil || wallet.Version() != 1 {
		t.Fatal("zero credit changed wallet")
	}
	if !wallet.UpdatedAt().Equal(now) {
		t.Fatal("zero credit changed UpdatedAt")
	}
	two, _ := ParseMoney("2.00", "BRL")
	creditTime := now.Add(time.Minute)
	if err := wallet.Credit(two, creditTime); err != nil || wallet.Version() != 2 || wallet.Balance().MinorUnits() != 1200 {
		t.Fatal("credit failed")
	}
	if !wallet.UpdatedAt().Equal(creditTime) {
		t.Fatal("credit did not change UpdatedAt")
	}
	if err := wallet.Debit(zero, now); err != nil || wallet.Version() != 2 {
		t.Fatal("zero debit changed wallet")
	}
	if !wallet.UpdatedAt().Equal(creditTime) {
		t.Fatal("zero debit changed UpdatedAt")
	}
	debitTime := now.Add(2 * time.Minute)
	if err := wallet.Debit(two, debitTime); err != nil || wallet.Version() != 3 || wallet.Balance().MinorUnits() != 1000 {
		t.Fatal("debit failed")
	}
	if !wallet.UpdatedAt().Equal(debitTime) {
		t.Fatal("debit did not change UpdatedAt")
	}
	tooMuch, _ := ParseMoney("20.00", "BRL")
	if err := wallet.Debit(tooMuch, now); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatal("insufficient balance not rejected")
	}
	usd, _ := ParseMoney("1.00", "USD")
	if err := wallet.Credit(usd, now); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatal("currency mismatch not rejected")
	}
	rehydrated, err := RehydrateWallet("w", "p", "BRL", wallet.Balance(), wallet.Version(), now, now)
	if err != nil || rehydrated.Version() != wallet.Version() {
		t.Fatal("rehydration failed")
	}
}
