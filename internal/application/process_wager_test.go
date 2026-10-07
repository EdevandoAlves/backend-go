package application

import (
	"context"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"testing"
	"time"
)

func TestCanonicalWagerHashExcludesIdempotencyKey(t *testing.T) {
	m, _ := domain.ParseMoney("1.00", "BRL")
	a := ProcessWagerCommand{ProviderID: "p", ExternalID: "e", IdempotencyKey: "a", PlayerID: "pl", WalletID: "w", GameID: "g", RoundID: "r", Kind: domain.TransactionBet, Amount: m, Now: time.Now()}
	b := a
	b.IdempotencyKey = "b"
	ha, _ := CanonicalWagerHash(a)
	hb, _ := CanonicalWagerHash(b)
	if ha != hb {
		t.Fatal("hash includes idempotency key")
	}
}

func TestCanonicalWagerHashGolden(t *testing.T) {
	m, _ := domain.ParseMoney("1.00", "BRL")
	c := ProcessWagerCommand{ProviderID: "p", ExternalID: "e", PlayerID: "pl", WalletID: "w", GameID: "g", RoundID: "r", Kind: domain.TransactionBet, Amount: m}
	h, err := CanonicalWagerHash(c)
	if err != nil || h != "1966d3d9bf77d14fcd0fc10e3ab80bb272756fcd44ae1eeb702ed79ff4ff6c6a" {
		t.Fatalf("hash=%s err=%v", h, err)
	}
}

func TestCanonicalWagerHashChangesBusinessFields(t *testing.T) {
	m, _ := domain.ParseMoney("1.00", "BRL")
	base := ProcessWagerCommand{ProviderID: "p", ExternalID: "e", PlayerID: "pl", WalletID: "w", GameID: "g", RoundID: "r", Kind: domain.TransactionBet, Amount: m}
	h, _ := CanonicalWagerHash(base)
	for name, change := range map[string]func(*ProcessWagerCommand){
		"provider": func(c *ProcessWagerCommand) { c.ProviderID = "other" },
		"amount":   func(c *ProcessWagerCommand) { c.Amount, _ = domain.ParseMoney("2.00", "BRL") },
		"currency": func(c *ProcessWagerCommand) { c.Amount, _ = domain.ParseMoney("1.00", "USD") },
		"round":    func(c *ProcessWagerCommand) { c.RoundID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			c := base
			change(&c)
			got, _ := CanonicalWagerHash(c)
			if got == h {
				t.Fatal("business field did not change hash")
			}
		})
	}
}

func TestProcessWagerRejectsInvalidCommand(t *testing.T) {
	if err := (ProcessWagerService{}).Execute(context.Background(), ProcessWagerCommand{}); err == nil {
		t.Fatal("invalid command accepted")
	}
}
