package postgres

import (
	"context"
	"errors"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
)

type TransactionRepository struct{}

func (TransactionRepository) InsertOpening(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction, resultingBalance domain.Money, walletVersion int64) error {
	if t.Type() != domain.TransactionOpening || t.Status() != domain.TransactionProcessed || t.ExternalID() != "" || t.ProviderID() != "" || t.Amount().MinorUnits() <= 0 || resultingBalance.Currency() != t.Amount().Currency() || walletVersion < 1 {
		return errors.New("invalid opening transaction")
	}
	_, err := tx.Exec(ctx, `INSERT INTO wager_transactions (id, origin, player_id, wallet_id, kind, amount_minor, currency, status, result_balance_minor, result_currency, result_wallet_version, created_at, updated_at) VALUES ($1,'INTERNAL',$2,$3,'OPENING',$4,$5,'PROCESSED',$6,$7,$8,$9,$9)`, t.ID(), t.PlayerID(), t.WalletID(), t.Amount().MinorUnits(), t.Amount().Currency(), resultingBalance.MinorUnits(), resultingBalance.Currency(), walletVersion, t.CreatedAt())
	return classify(err)
}
