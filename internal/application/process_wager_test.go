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
	if CanonicalWagerHash(a) != CanonicalWagerHash(b) {
		t.Fatal("hash includes idempotency key")
	}
}

func TestProcessWagerRejectsInvalidCommand(t *testing.T) {
	if err := (ProcessWagerService{}).Execute(context.Background(), ProcessWagerCommand{}); err == nil {
		t.Fatal("invalid command accepted")
	}
}
