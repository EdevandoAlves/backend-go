package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
)

type ReconcileReferenceIDs struct{ ProcessedEventID, BalanceEventID, LedgerID string }

type ReconcileReferenceService struct {
	Manager TransactionRunner
	Wallet  interface {
		GetForUpdate(context.Context, pgx.Tx, string) (domain.Wallet, error)
		Save(context.Context, pgx.Tx, domain.Wallet, int64) error
	}
	Transactions ExternalTransactionWriter
	Ledger       LedgerWriter
	Outbox       OutboxWriter
}

func (s ReconcileReferenceService) ReconcileOne(ctx context.Context, now time.Time, ids ReconcileReferenceIDs) (bool, error) {
	claimed := false
	err := s.Manager.WithinTransaction(ctx, func(tx pgx.Tx) error {
		pending, err := s.Transactions.ClaimPendingReference(ctx, tx, now)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			return err
		}
		claimed = true
		if pending.Type() != domain.TransactionRefund {
			return nil
		}
		ref, err := s.Transactions.GetExternalByExternalID(ctx, tx, pending.ProviderID(), pending.ReferenceExternalID())
		if err != nil || ref.Status() != domain.TransactionProcessed || ref.Type() != domain.TransactionBet || ref.PlayerID() != pending.PlayerID() || ref.WalletID() != pending.WalletID() || ref.Amount().MinorUnits() != pending.Amount().MinorUnits() || ref.Amount().Currency() != pending.Amount().Currency() || ref.RoundID() != pending.RoundID() {
			return nil
		}
		w, err := s.Wallet.GetForUpdate(ctx, tx, pending.WalletID())
		if err != nil {
			return err
		}
		before := w.Balance()
		if err = w.Credit(pending.Amount(), now); err != nil {
			return err
		}
		if err = s.Wallet.Save(ctx, tx, w, w.Version()-1); err != nil {
			return err
		}
		result := domain.WagerTransactionResult{Balance: w.Balance(), WalletVersion: w.Version()}
		terminal, err := domain.RehydrateExternalWagerTransaction(domain.RehydratedWagerTransaction{WagerTransactionInput: domain.WagerTransactionInput{ID: pending.ID(), ExternalID: pending.ExternalID(), ProviderID: pending.ProviderID(), PlayerID: pending.PlayerID(), WalletID: pending.WalletID(), IdempotencyKey: pending.IdempotencyKey(), PayloadHash: pending.PayloadHash(), GameID: pending.GameID(), RoundID: pending.RoundID(), Kind: pending.Type(), Amount: pending.Amount(), ReferenceExternalID: pending.ReferenceExternalID()}, Status: domain.TransactionProcessed, ReferenceTransactionID: ref.ID(), Result: &result, CreatedAt: pending.CreatedAt(), UpdatedAt: now})
		if err != nil {
			return err
		}
		if err = s.Transactions.UpdateTerminalFrom(ctx, tx, terminal, domain.TransactionPendingReference, ref.ID()); err != nil {
			return err
		}
		entry, err := domain.NewWalletLedgerEntry(ids.LedgerID, w.ID(), pending.ID(), domain.LedgerCredit, pending.Amount(), before, w.Balance(), now)
		if err != nil {
			return err
		}
		if err = s.Ledger.Insert(ctx, tx, entry); err != nil {
			return err
		}
		money := func(m domain.Money) map[string]string {
			return map[string]string{"amount": formatMoney(m), "currency": m.Currency()}
		}
		processed, _ := json.Marshal(map[string]any{"transactionId": pending.ID(), "walletId": w.ID(), "state": "PROCESSED", "operationType": string(pending.Type()), "money": money(pending.Amount()), "resultingBalance": money(w.Balance())})
		if err = s.Outbox.Insert(ctx, tx, OutboxEvent{ID: ids.ProcessedEventID, AggregateID: w.ID(), TransactionID: pending.ID(), EventType: "WagerTransactionProcessed", CorrelationID: pending.ID(), EventVersion: 1, Payload: processed}); err != nil {
			return err
		}
		balance, _ := json.Marshal(map[string]any{"walletId": w.ID(), "transactionId": pending.ID(), "direction": "CREDIT", "money": money(pending.Amount()), "balanceBefore": money(before), "balanceAfter": money(w.Balance()), "walletVersion": w.Version()})
		return s.Outbox.Insert(ctx, tx, OutboxEvent{ID: ids.BalanceEventID, AggregateID: w.ID(), TransactionID: pending.ID(), EventType: "WalletBalanceChanged", CorrelationID: pending.ID(), EventVersion: 1, Payload: balance})
	})
	return claimed, err
}

func formatMoney(m domain.Money) string {
	n := m.MinorUnits()
	return fmt.Sprintf("%d.%02d", n/100, n%100)
}
