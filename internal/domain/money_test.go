package domain

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestParseMoney(t *testing.T) {
	money, err := ParseMoney("25.00", "BRL")
	if err != nil {
		t.Fatalf("ParseMoney returned error: %v", err)
	}
	if got := money.String(); got != "25.00 BRL" {
		t.Fatalf("String() = %q, want %q", got, "25.00 BRL")
	}
}

func TestMoneyParsingAndValidation(t *testing.T) {
	for _, tc := range []struct {
		input string
		cents int64
	}{{"0.00", 0}, {"25.00", 2500}} {
		money, err := ParseMoney(tc.input, "BRL")
		if err != nil || money.MinorUnits() != tc.cents {
			t.Fatalf("ParseMoney(%q) = %#v, %v", tc.input, money, err)
		}
	}
	for _, input := range []string{"", " ", "-1.00", "1", "1.0", "1.000", "1,00", "1e2", "NaN", "Infinity"} {
		if _, err := ParseMoney(input, "BRL"); !errors.Is(err, ErrInvalidMoney) {
			t.Errorf("ParseMoney(%q) error = %v", input, err)
		}
	}
	for _, currency := range []string{"", "brl", "BR", "BRLL", "B1L"} {
		if _, err := ParseMoney("1.00", currency); !errors.Is(err, ErrInvalidCurrency) {
			t.Errorf("currency %q error = %v", currency, err)
		}
	}
	if _, err := ParseMoney("92233720368547758.08", "BRL"); !errors.Is(err, ErrMoneyOverflow) {
		t.Fatalf("parse overflow error = %v", err)
	}
}

func TestParseMoneyMaxInt64Boundary(t *testing.T) {
	money, err := ParseMoney("92233720368547758.07", "BRL")
	if err != nil {
		t.Fatalf("ParseMoney returned error: %v", err)
	}
	if money.MinorUnits() != math.MaxInt64 {
		t.Fatalf("MinorUnits() = %d, want %d", money.MinorUnits(), int64(math.MaxInt64))
	}
}

func TestMoneyOperations(t *testing.T) {
	a, _ := NewMoneyForInternal(2500, "BRL")
	b, _ := NewMoneyForInternal(500, "BRL")
	if got, _ := a.Add(b); got.MinorUnits() != 3000 {
		t.Fatal("Add failed")
	}
	if got, _ := a.Subtract(b); got.MinorUnits() != 2000 {
		t.Fatal("Subtract failed")
	}
	if got, _ := a.Negate(); got.String() != "-25.00 BRL" {
		t.Fatalf("Negate = %q", got.String())
	}
	if got, _ := a.Compare(b); got != 1 {
		t.Fatal("Compare failed")
	}
	zero, _ := Zero("BRL")
	if zero.String() != "0.00 BRL" {
		t.Fatal("Zero failed")
	}
	other, _ := NewMoneyForInternal(1, "USD")
	if _, err := a.Add(other); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatal("mismatch not rejected")
	}
	if _, err := a.Compare(other); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatal("compare mismatch not rejected")
	}
}

func TestMoneyOperationOverflow(t *testing.T) {
	max, _ := NewMoneyForInternal(math.MaxInt64, "BRL")
	min, _ := NewMoneyForInternal(math.MinInt64, "BRL")
	one, _ := NewMoneyForInternal(1, "BRL")
	if _, err := max.Add(one); !errors.Is(err, ErrMoneyOverflow) {
		t.Error("add overflow not detected")
	}
	if _, err := min.Subtract(one); !errors.Is(err, ErrMoneyOverflow) {
		t.Error("subtract overflow not detected")
	}
	if _, err := min.Negate(); !errors.Is(err, ErrMoneyOverflow) {
		t.Error("negate overflow not detected")
	}
}

func TestMoneyJSONUsesStringAmount(t *testing.T) {
	money, _ := ParseMoney("25.00", "BRL")
	data, err := json.Marshal(money)
	if err != nil || string(data) != `{"amount":"25.00","currency":"BRL"}` {
		t.Fatalf("marshal = %s, %v", data, err)
	}
	var decoded Money
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.MinorUnits() != 2500 {
		t.Fatalf("unmarshal = %#v, %v", decoded, err)
	}
	if err := json.Unmarshal([]byte(`{"amount":25.00,"currency":"BRL"}`), &decoded); !errors.Is(err, ErrInvalidMoney) {
		t.Fatalf("number amount error = %v", err)
	}
}
