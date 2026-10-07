package application

import (
	"context"
	"errors"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrWalletNotFound = errors.New("wallet not found")
var ErrConflict = errors.New("conflict")
var ErrIdempotencyConflict = errors.New("idempotency conflict")
var ErrExternalIDConflict = errors.New("external id conflict")
var ErrNotFound = errors.New("not found")
var ErrReferenceUnavailable = errors.New("reference unavailable")

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
	UpdateTerminalWithReference(context.Context, pgx.Tx, domain.WagerTransaction, string) error
	UpdateTerminalFrom(context.Context, pgx.Tx, domain.WagerTransaction, domain.WagerTransactionStatus, string) error
	FindProcessedReversal(context.Context, pgx.Tx, string) (domain.WagerTransaction, error)
	GetExternalByIdempotencyKey(context.Context, pgx.Tx, string, string) (domain.WagerTransaction, error)
	GetExternalByExternalID(context.Context, pgx.Tx, string, string) (domain.WagerTransaction, error)
	ClaimPendingReference(context.Context, pgx.Tx, time.Time) (domain.WagerTransaction, error)
	ReschedulePendingReference(context.Context, pgx.Tx, domain.WagerTransaction, time.Time) error
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
