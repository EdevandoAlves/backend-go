package application

import (
	"context"
	"errors"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrWalletNotFound = errors.New("wallet not found")
var ErrConflict = errors.New("conflict")

type TransactionRunner interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

type WalletWriter interface {
	Insert(context.Context, pgx.Tx, domain.Wallet) error
}

type OpeningWriter interface {
	InsertOpening(context.Context, pgx.Tx, domain.WagerTransaction, domain.Money, int64) error
}

type ExternalTransactionWriter interface {
	InsertExternalPending(context.Context, pgx.Tx, domain.WagerTransaction) error
	UpdateTerminal(context.Context, pgx.Tx, domain.WagerTransaction) error
}

type LedgerWriter interface {
	Insert(context.Context, pgx.Tx, domain.WalletLedgerEntry) error
}

type OutboxEvent struct {
	ID, AggregateID, TransactionID, EventType, CorrelationID string
	EventVersion                                             int64
	Payload                                                  []byte
}

type OutboxWriter interface {
	Insert(context.Context, pgx.Tx, OutboxEvent) error
}
