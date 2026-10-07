package application

import (
	"context"
	"errors"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrWalletNotFound = errors.New("wallet not found")
var ErrConflict = errors.New("conflict")
var ErrIdempotencyConflict = errors.New("idempotency conflict")
var ErrExternalIDConflict = errors.New("external id conflict")
var ErrNotFound = errors.New("not found")

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
	TryInsertExternalPending(context.Context, pgx.Tx, domain.WagerTransaction) (bool, error)
	UpdateTerminal(context.Context, pgx.Tx, domain.WagerTransaction) error
	GetExternalByIdempotencyKey(context.Context, pgx.Tx, string, string) (domain.WagerTransaction, error)
	GetExternalByExternalID(context.Context, pgx.Tx, string, string) (domain.WagerTransaction, error)
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
