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
	AfterStep    func(string) error
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
		if pending.Type() != domain.TransactionRefund && pending.Type() != domain.TransactionRollback {
			return nil
		}
		ref, err := s.Transactions.GetExternalByExternalID(ctx, tx, pending.ProviderID(), pending.ReferenceExternalID())
		if err != nil {
			if pending.AttemptCount() >= 9 {
				w, e := s.Wallet.GetForUpdate(ctx, tx, pending.WalletID())
				if e != nil {
					return e
				}
				r := domain.WagerTransactionResult{Balance: w.Balance(), WalletVersion: w.Version()}
				terminal, e := domain.RehydrateExternalWagerTransaction(domain.RehydratedWagerTransaction{WagerTransactionInput: domain.WagerTransactionInput{ID: pending.ID(), ExternalID: pending.ExternalID(), ProviderID: pending.ProviderID(), PlayerID: pending.PlayerID(), WalletID: pending.WalletID(), IdempotencyKey: pending.IdempotencyKey(), PayloadHash: pending.PayloadHash(), GameID: pending.GameID(), RoundID: pending.RoundID(), Kind: pending.Type(), Amount: pending.Amount(), ReferenceExternalID: pending.ReferenceExternalID()}, Status: domain.TransactionRejected, FailureCode: "REFERENCE_NOT_FOUND", Result: &r, CreatedAt: pending.CreatedAt(), UpdatedAt: now})
				if e != nil {
					return e
				}
				if e = s.Transactions.UpdateTerminalFrom(ctx, tx, terminal, domain.TransactionPendingReference, ""); e != nil {
					return e
				}
				payload, _ := json.Marshal(map[string]any{"transactionId": pending.ID(), "walletId": w.ID(), "state": "REJECTED", "operationType": string(pending.Type()), "failureCode": "REFERENCE_NOT_FOUND", "resultingBalance": map[string]string{"amount": formatMoney(w.Balance()), "currency": w.Currency()}})
				return s.Outbox.Insert(ctx, tx, OutboxEvent{ID: pending.ID() + ":rejected", AggregateID: w.ID(), TransactionID: pending.ID(), EventType: "WagerTransactionRejected", CorrelationID: pending.ID(), EventVersion: 1, Payload: payload})
			}
			delay := time.Second << pending.AttemptCount()
			if delay > 300*time.Second {
				delay = 300 * time.Second
			}
			return s.Transactions.ReschedulePendingReference(ctx, tx, pending, now.Add(delay))
		}
		matches := ref.Status() == domain.TransactionProcessed && ref.PlayerID() == pending.PlayerID() && ref.WalletID() == pending.WalletID() && ref.Amount().MinorUnits() == pending.Amount().MinorUnits() && ref.Amount().Currency() == pending.Amount().Currency() && ref.RoundID() == pending.RoundID()
		failure := ""
		if pending.Type() == domain.TransactionRefund && ref.Type() != domain.TransactionBet {
			matches = false
		}
		if pending.Type() == domain.TransactionRollback {
			switch ref.Type() {
			case domain.TransactionBet, domain.TransactionWin, domain.TransactionRefund:
			default:
				matches = false
			}
		}
		w, err := s.Wallet.GetForUpdate(ctx, tx, pending.WalletID())
		if err != nil {
			return err
		}
		before := w.Balance()
		if !matches {
			failure = "REFERENCE_MISMATCH"
		}
		if matches && pending.Type() == domain.TransactionRollback && (ref.Type() == domain.TransactionWin || ref.Type() == domain.TransactionRefund) {
			if err = w.Debit(pending.Amount(), now); err != nil {
				failure = "INSUFFICIENT_FUNDS"
			}
		} else if matches {
			err = w.Credit(pending.Amount(), now)
		}
		if failure != "" {
			result := domain.WagerTransactionResult{Balance: before, WalletVersion: w.Version()}
			terminal, e := rehydrateReferenceTerminal(pending, domain.TransactionRejected, failure, result, now, ref.ID())
			if e != nil {
				return e
			}
			if e = s.Transactions.UpdateTerminalFrom(ctx, tx, terminal, domain.TransactionPendingReference, ref.ID()); e != nil {
				return e
			}
			payload, _ := json.Marshal(map[string]any{"transactionId": pending.ID(), "walletId": w.ID(), "state": "REJECTED", "operationType": string(pending.Type()), "failureCode": failure, "resultingBalance": map[string]string{"amount": formatMoney(before), "currency": before.Currency()}})
			return s.Outbox.Insert(ctx, tx, OutboxEvent{ID: pending.ID() + ":rejected", AggregateID: w.ID(), TransactionID: pending.ID(), EventType: "WagerTransactionRejected", CorrelationID: pending.ID(), EventVersion: 1, Payload: payload})
		}
		if err != nil {
			return err
		}
		if pending.Type() == domain.TransactionRollback {
			if existing, e := s.Transactions.FindProcessedReversal(ctx, tx, ref.ID()); e == nil {
				_ = existing
				result := domain.WagerTransactionResult{Balance: before, WalletVersion: w.Version()}
				terminal, e := rehydrateReferenceTerminal(pending, domain.TransactionRejected, "REFERENCE_ALREADY_REVERSED", result, now, ref.ID())
				if e != nil {
					return e
				}
				if e = s.Transactions.UpdateTerminalFrom(ctx, tx, terminal, domain.TransactionPendingReference, ref.ID()); e != nil {
					return e
				}
				payload, _ := json.Marshal(map[string]any{"transactionId": pending.ID(), "walletId": w.ID(), "state": "REJECTED", "operationType": string(pending.Type()), "failureCode": "REFERENCE_ALREADY_REVERSED", "resultingBalance": map[string]string{"amount": formatMoney(before), "currency": before.Currency()}})
				return s.Outbox.Insert(ctx, tx, OutboxEvent{ID: pending.ID() + ":rejected", AggregateID: w.ID(), TransactionID: pending.ID(), EventType: "WagerTransactionRejected", CorrelationID: pending.ID(), EventVersion: 1, Payload: payload})
			}
		}
		if err = s.Wallet.Save(ctx, tx, w, w.Version()-1); err != nil {
			return err
		}
		if s.AfterStep != nil {
			if err = s.AfterStep("afterWallet"); err != nil {
				return err
			}
		}
		result := domain.WagerTransactionResult{Balance: w.Balance(), WalletVersion: w.Version()}
		terminal, err := rehydrateReferenceTerminal(pending, domain.TransactionProcessed, "", result, now, ref.ID())
		if err != nil {
			return err
		}
		if err = s.Transactions.UpdateTerminalFrom(ctx, tx, terminal, domain.TransactionPendingReference, ref.ID()); err != nil {
			return err
		}
		direction := domain.LedgerCredit
		directionName := "CREDIT"
		if pending.Type() == domain.TransactionRollback && (ref.Type() == domain.TransactionWin || ref.Type() == domain.TransactionRefund) {
			direction = domain.LedgerDebit
			directionName = "DEBIT"
		}
		entry, err := domain.NewWalletLedgerEntry(ids.LedgerID, w.ID(), pending.ID(), direction, pending.Amount(), before, w.Balance(), now)
		if err != nil {
			return err
		}
		if err = s.Ledger.Insert(ctx, tx, entry); err != nil {
			return err
		}
		if s.AfterStep != nil {
			if err = s.AfterStep("afterLedger"); err != nil {
				return err
			}
		}
		money := func(m domain.Money) map[string]string {
			return map[string]string{"amount": formatMoney(m), "currency": m.Currency()}
		}
		processed, _ := json.Marshal(map[string]any{"transactionId": pending.ID(), "walletId": w.ID(), "state": "PROCESSED", "operationType": string(pending.Type()), "money": money(pending.Amount()), "resultingBalance": money(w.Balance())})
		if err = s.Outbox.Insert(ctx, tx, OutboxEvent{ID: ids.ProcessedEventID, AggregateID: w.ID(), TransactionID: pending.ID(), EventType: "WagerTransactionProcessed", CorrelationID: pending.ID(), EventVersion: 1, Payload: processed}); err != nil {
			return err
		}
		balance, _ := json.Marshal(map[string]any{"walletId": w.ID(), "transactionId": pending.ID(), "direction": directionName, "money": money(pending.Amount()), "balanceBefore": money(before), "balanceAfter": money(w.Balance()), "walletVersion": w.Version()})
		return s.Outbox.Insert(ctx, tx, OutboxEvent{ID: ids.BalanceEventID, AggregateID: w.ID(), TransactionID: pending.ID(), EventType: "WalletBalanceChanged", CorrelationID: pending.ID(), EventVersion: 1, Payload: balance})
	})
	return claimed, err
}

func rehydrateReferenceTerminal(t domain.WagerTransaction, status domain.WagerTransactionStatus, failure string, result domain.WagerTransactionResult, now time.Time, ref string) (domain.WagerTransaction, error) {
	return domain.RehydrateExternalWagerTransaction(domain.RehydratedWagerTransaction{WagerTransactionInput: domain.WagerTransactionInput{ID: t.ID(), ExternalID: t.ExternalID(), ProviderID: t.ProviderID(), PlayerID: t.PlayerID(), WalletID: t.WalletID(), IdempotencyKey: t.IdempotencyKey(), PayloadHash: t.PayloadHash(), GameID: t.GameID(), RoundID: t.RoundID(), Kind: t.Type(), Amount: t.Amount(), ReferenceExternalID: t.ReferenceExternalID()}, Status: status, FailureCode: failure, ReferenceTransactionID: ref, Result: &result, CreatedAt: t.CreatedAt(), UpdatedAt: now})
}

func formatMoney(m domain.Money) string {
	n := m.MinorUnits()
	return fmt.Sprintf("%d.%02d", n/100, n%100)
}
