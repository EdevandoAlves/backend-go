package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"
)

type ProcessWagerCommand struct {
	ID, ProviderID, ExternalID, IdempotencyKey, PlayerID, WalletID, GameID, RoundID string
	Kind                                                                            domain.WagerTransactionType
	Amount                                                                          domain.Money
	Now                                                                             time.Time
	TransactionID, ProcessedEventID, RejectedEventID, BalanceEventID                string
}
type ProcessWagerService struct {
	Manager TransactionRunner
	Wallet  interface {
		GetForUpdate(context.Context, pgx.Tx, string) (domain.Wallet, error)
		Save(context.Context, pgx.Tx, domain.Wallet, int64) error
	}
	Transactions ExternalTransactionWriter
	Ledger       LedgerWriter
	Outbox       OutboxWriter
	AfterStep    func(string) error
	IsNotFound   func(error) bool
}

type wagerMoneyPayload struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}
type wagerProcessedPayload struct {
	TransactionID    string            `json:"transactionId"`
	WalletID         string            `json:"walletId"`
	State            string            `json:"state"`
	OperationType    string            `json:"operationType"`
	Money            wagerMoneyPayload `json:"money"`
	ResultingBalance wagerMoneyPayload `json:"resultingBalance"`
}
type wagerBalancePayload struct {
	WalletID      string            `json:"walletId"`
	TransactionID string            `json:"transactionId"`
	Direction     string            `json:"direction"`
	Money         wagerMoneyPayload `json:"money"`
	BalanceBefore wagerMoneyPayload `json:"balanceBefore"`
	BalanceAfter  wagerMoneyPayload `json:"balanceAfter"`
	WalletVersion int64             `json:"walletVersion"`
}
type wagerRejectedPayload struct {
	TransactionID    string            `json:"transactionId"`
	WalletID         string            `json:"walletId"`
	State            string            `json:"state"`
	OperationType    string            `json:"operationType"`
	FailureCode      string            `json:"failureCode"`
	ResultingBalance wagerMoneyPayload `json:"resultingBalance"`
}

func wagerMoney(m domain.Money) wagerMoneyPayload {
	n := m.MinorUnits()
	return wagerMoneyPayload{Amount: strconv.FormatInt(n/100, 10) + "." + strconv.FormatInt((n%100)/10, 10) + strconv.FormatInt(n%10, 10), Currency: m.Currency()}
}

func CanonicalWagerHash(c ProcessWagerCommand) string {
	v := struct {
		ProviderID, ExternalID, PlayerID, WalletID, GameID, RoundID string
		Kind                                                        domain.WagerTransactionType
		Amount                                                      domain.Money
	}{c.ProviderID, c.ExternalID, c.PlayerID, c.WalletID, c.GameID, c.RoundID, c.Kind, c.Amount}
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func (s ProcessWagerService) Execute(ctx context.Context, c ProcessWagerCommand) error {
	if c.ID == "" || c.ProviderID == "" || c.ExternalID == "" || c.IdempotencyKey == "" || c.PlayerID == "" || c.WalletID == "" || c.GameID == "" || c.RoundID == "" || c.Now.IsZero() || (c.Kind != domain.TransactionBet && c.Kind != domain.TransactionWin && c.Kind != domain.TransactionLoss) {
		return domain.ErrInvalidTransaction
	}
	return s.Manager.WithinTransaction(ctx, func(tx pgx.Tx) error {
		w, e := s.Wallet.GetForUpdate(ctx, tx, c.WalletID)
		if e != nil {
			if s.IsNotFound != nil && s.IsNotFound(e) {
				return ErrWalletNotFound
			}
			return e
		}
		if w.PlayerID() != c.PlayerID {
			return ErrWalletNotFound
		}
		t, e := domain.CreateExternalWagerTransaction(domain.WagerTransactionInput{ID: c.ID, ExternalID: c.ExternalID, ProviderID: c.ProviderID, PlayerID: c.PlayerID, WalletID: c.WalletID, IdempotencyKey: c.IdempotencyKey, PayloadHash: CanonicalWagerHash(c), GameID: c.GameID, RoundID: c.RoundID, Kind: c.Kind, Amount: c.Amount}, c.Now)
		if e != nil {
			return e
		}
		if e = s.Transactions.InsertExternalPending(ctx, tx, t); e != nil {
			return e
		}
		if s.AfterStep != nil {
			if e = s.AfterStep("afterPending"); e != nil {
				return e
			}
		}
		r := domain.WagerTransactionResult{Balance: w.Balance(), WalletVersion: w.Version()}
		if c.Amount.Currency() != w.Currency() {
			if e = t.Reject("CURRENCY_MISMATCH", r, c.Now); e != nil {
				return e
			}
			if e = s.Transactions.UpdateTerminal(ctx, tx, t); e != nil {
				return e
			}
			return s.insertRejected(ctx, tx, c, t, r)
		}
		before := w.Balance()
		changed := false
		if c.Kind == domain.TransactionBet {
			if e = w.Debit(c.Amount, c.Now); e != nil {
				if e = t.Reject("INSUFFICIENT_FUNDS", r, c.Now); e != nil {
					return e
				}
				if e = s.Transactions.UpdateTerminal(ctx, tx, t); e != nil {
					return e
				}
				return s.insertRejected(ctx, tx, c, t, r)
			}
			changed = true
		} else if c.Kind == domain.TransactionWin {
			if e = w.Credit(c.Amount, c.Now); e != nil {
				return e
			}
			changed = true
		}
		if changed {
			if e = s.Wallet.Save(ctx, tx, w, w.Version()-1); e != nil {
				return e
			}
			if s.AfterStep != nil {
				if e = s.AfterStep("afterWallet"); e != nil {
					return e
				}
			}
			dir := domain.LedgerCredit
			if c.Kind == domain.TransactionBet {
				dir = domain.LedgerDebit
			}
			le, e := domain.NewWalletLedgerEntry(c.TransactionID, c.WalletID, c.ID, dir, c.Amount, before, w.Balance(), c.Now)
			if e != nil {
				return e
			}
			if e = s.Ledger.Insert(ctx, tx, le); e != nil {
				return e
			}
			if s.AfterStep != nil {
				if e = s.AfterStep("afterLedger"); e != nil {
					return e
				}
			}
		}
		if e = t.Process(domain.WagerTransactionResult{Balance: w.Balance(), WalletVersion: w.Version()}, c.Now); e != nil {
			return e
		}
		if e = s.Transactions.UpdateTerminal(ctx, tx, t); e != nil {
			return e
		}
		if s.AfterStep != nil {
			if e = s.AfterStep("afterTerminal"); e != nil {
				return e
			}
		}
		if e = s.insertProcessed(ctx, tx, c, t, before, w.Balance(), changed); e != nil {
			return e
		}
		if s.AfterStep != nil {
			if e = s.AfterStep("afterOutbox"); e != nil {
				return e
			}
		}
		return nil
	})
}

func (s ProcessWagerService) insertRejected(ctx context.Context, tx pgx.Tx, c ProcessWagerCommand, t domain.WagerTransaction, result domain.WagerTransactionResult) error {
	p, e := json.Marshal(wagerRejectedPayload{c.ID, c.WalletID, string(domain.TransactionRejected), string(c.Kind), t.FailureCode(), wagerMoney(result.Balance)})
	if e != nil {
		return e
	}
	return s.Outbox.Insert(ctx, tx, OutboxEvent{ID: c.RejectedEventID, AggregateID: c.WalletID, TransactionID: c.ID, EventType: "WagerTransactionRejected", CorrelationID: c.ID, EventVersion: 1, Payload: p})
}
func (s ProcessWagerService) insertProcessed(ctx context.Context, tx pgx.Tx, c ProcessWagerCommand, t domain.WagerTransaction, before, after domain.Money, changed bool) error {
	r, ok := t.Result()
	if !ok {
		return domain.ErrInvalidTransaction
	}
	p, e := json.Marshal(wagerProcessedPayload{c.ID, c.WalletID, string(domain.TransactionProcessed), string(c.Kind), wagerMoney(c.Amount), wagerMoney(r.Balance)})
	if e != nil {
		return e
	}
	if e = s.Outbox.Insert(ctx, tx, OutboxEvent{ID: c.ProcessedEventID, AggregateID: c.WalletID, TransactionID: c.ID, EventType: "WagerTransactionProcessed", CorrelationID: c.ID, EventVersion: 1, Payload: p}); e != nil {
		return e
	}
	if !changed {
		return nil
	}
	b, e := json.Marshal(wagerBalancePayload{c.WalletID, c.ID, map[bool]string{true: "DEBIT", false: "CREDIT"}[c.Kind == domain.TransactionBet], wagerMoney(c.Amount), wagerMoney(before), wagerMoney(after), r.WalletVersion})
	if e != nil {
		return e
	}
	return s.Outbox.Insert(ctx, tx, OutboxEvent{ID: c.BalanceEventID, AggregateID: c.WalletID, TransactionID: c.ID, EventType: "WalletBalanceChanged", CorrelationID: c.ID, EventVersion: 1, Payload: b})
}
