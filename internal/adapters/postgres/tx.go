package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrConflict = errors.New("database conflict")

type TxManager struct{ pool *pgxpool.Pool }

func NewTxManager(pool *Pool) *TxManager { return &TxManager{pool: pool.pool} }

func (m *TxManager) WithinTransaction(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
