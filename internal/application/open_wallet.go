package application

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
)

type OpenWalletCommand struct {
	WalletID, PlayerID string
	InitialBalance     domain.Money
	Now                time.Time
	OpeningID          string
	LedgerID           string
	ProcessedEventID   string
	BalanceEventID     string
}

type OpenWalletService struct {
	Transactions OpeningWriter
	Wallets      WalletWriter
	Ledger       LedgerWriter
	Outbox       OutboxWriter
	Manager      TransactionRunner
	AfterLedger  func() error
}

type moneyPayload struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type processedPayload struct {
	TransactionID    string       `json:"transactionId"`
	WalletID         string       `json:"walletId"`
	State            string       `json:"state"`
	OperationType    string       `json:"operationType"`
	Money            moneyPayload `json:"money"`
	ResultingBalance moneyPayload `json:"resultingBalance"`
}

type balanceChangedPayload struct {
	WalletID      string       `json:"walletId"`
	TransactionID string       `json:"transactionId"`
	Direction     string       `json:"direction"`
	Money         moneyPayload `json:"money"`
	BalanceBefore moneyPayload `json:"balanceBefore"`
	BalanceAfter  moneyPayload `json:"balanceAfter"`
	WalletVersion int64        `json:"walletVersion"`
}

func moneyAmount(m domain.Money) string {
	return strconv.FormatInt(m.MinorUnits()/100, 10) + "." + strconv.FormatInt(m.MinorUnits()%100/10, 10) + strconv.FormatInt(m.MinorUnits()%10, 10)
}

func (s OpenWalletService) Execute(ctx context.Context, command OpenWalletCommand) error {
	if command.Now.IsZero() || command.WalletID == "" || command.PlayerID == "" {
		return domain.ErrInvalidWallet
	}
	initial, err := domain.NewMoneyForInternal(command.InitialBalance.MinorUnits(), command.InitialBalance.Currency())
	if err != nil || initial.MinorUnits() < 0 {
		return domain.ErrInvalidWallet
	}
	if initial.MinorUnits() > 0 && (command.OpeningID == "" || command.LedgerID == "" || command.ProcessedEventID == "" || command.BalanceEventID == "") {
		return domain.ErrInvalidWallet
	}
	return s.Manager.WithinTransaction(ctx, func(tx pgx.Tx) error {
		wallet, err := domain.CreateWallet(command.WalletID, command.PlayerID, initial, command.Now)
		if err != nil {
			return err
		}
		if err := s.Wallets.Insert(ctx, tx, wallet); err != nil {
			return err
		}
		if initial.MinorUnits() == 0 {
			return nil
		}
		opening, err := domain.CreateOpeningWagerTransaction(command.OpeningID, command.PlayerID, command.WalletID, initial, command.Now)
		if err != nil {
			return err
		}
		if err := s.Transactions.InsertOpening(ctx, tx, opening, initial, 1); err != nil {
			return err
		}
		zero, _ := domain.Zero(initial.Currency())
		entry, err := domain.NewWalletLedgerEntry(command.LedgerID, command.WalletID, command.OpeningID, domain.LedgerCredit, initial, zero, initial, command.Now)
		if err != nil {
			return err
		}
		if err := s.Ledger.Insert(ctx, tx, entry); err != nil {
			return err
		}
		if s.AfterLedger != nil {
			if err := s.AfterLedger(); err != nil {
				return err
			}
		}
		money := moneyPayload{Amount: moneyAmount(initial), Currency: initial.Currency()}
		processed, err := json.Marshal(processedPayload{TransactionID: command.OpeningID, WalletID: command.WalletID, State: string(domain.TransactionProcessed), OperationType: string(domain.TransactionOpening), Money: money, ResultingBalance: money})
		if err != nil {
			return err
		}
		balance, err := json.Marshal(balanceChangedPayload{WalletID: command.WalletID, TransactionID: command.OpeningID, Direction: string(domain.LedgerCredit), Money: money, BalanceBefore: moneyPayload{Amount: "0.00", Currency: initial.Currency()}, BalanceAfter: money, WalletVersion: 1})
		if err != nil {
			return err
		}
		for _, event := range []OutboxEvent{{ID: command.ProcessedEventID, AggregateID: command.WalletID, TransactionID: command.OpeningID, EventType: "WagerTransactionProcessed", CorrelationID: command.OpeningID, EventVersion: 1, Payload: processed}, {ID: command.BalanceEventID, AggregateID: command.WalletID, TransactionID: command.OpeningID, EventType: "WalletBalanceChanged", CorrelationID: command.OpeningID, EventVersion: 1, Payload: balance}} {
			if err := s.Outbox.Insert(ctx, tx, event); err != nil {
				return err
			}
		}
		return nil
	})
}
