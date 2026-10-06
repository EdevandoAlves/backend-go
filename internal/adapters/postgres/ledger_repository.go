package postgres

import (
	"context"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
)

type LedgerRepository struct{}

func (LedgerRepository) Insert(ctx context.Context, tx pgx.Tx, e domain.WalletLedgerEntry) error {
	_, err := tx.Exec(ctx, `INSERT INTO wallet_ledger_entries (id,wallet_id,transaction_id,currency,direction,amount_minor,before_minor,after_minor) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, e.ID(), e.WalletID(), e.TransactionID(), e.Amount().Currency(), e.Direction(), e.Amount().MinorUnits(), e.Before().MinorUnits(), e.After().MinorUnits())
	return classify(err)
}
