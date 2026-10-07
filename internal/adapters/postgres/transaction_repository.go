package postgres

import (
	"context"
	"errors"

	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TransactionRepository struct{}

func (TransactionRepository) InsertExternalPending(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction) error {
	_, err := tx.Exec(ctx, `INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,created_at,updated_at) VALUES ($1,'EXTERNAL',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'PENDING',$13,$13)`, t.ID(), t.ExternalID(), t.ProviderID(), t.IdempotencyKey(), t.PayloadHash(), t.PlayerID(), t.WalletID(), t.GameID(), t.RoundID(), t.Type(), t.Amount().MinorUnits(), t.Amount().Currency(), t.CreatedAt())
	return classifyApplication(err)
}
func (TransactionRepository) UpdateTerminal(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction) error {
	r, ok := t.Result()
	if !ok {
		return errors.New("missing result")
	}
	tag, err := tx.Exec(ctx, `UPDATE wager_transactions SET status=$1,failure_code=$2,result_balance_minor=$3,result_currency=$4,result_wallet_version=$5,updated_at=$6 WHERE id=$7 AND status='PENDING'`, t.Status(), nilIfEmpty(t.FailureCode()), r.Balance.MinorUnits(), r.Balance.Currency(), r.WalletVersion, t.UpdatedAt(), t.ID())
	if err != nil {
		return classify(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrConflict
	}
	return nil
}
func nilIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func classifyApplication(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errors.Join(application.ErrConflict, err)
	}
	return err
}

var _ application.ExternalTransactionWriter = TransactionRepository{}

func (TransactionRepository) InsertOpening(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction, resultingBalance domain.Money, walletVersion int64) error {
	if t.Type() != domain.TransactionOpening || t.Status() != domain.TransactionProcessed || t.ExternalID() != "" || t.ProviderID() != "" || t.Amount().MinorUnits() <= 0 || resultingBalance.Currency() != t.Amount().Currency() || walletVersion < 1 {
		return errors.New("invalid opening transaction")
	}
	_, err := tx.Exec(ctx, `INSERT INTO wager_transactions (id, origin, player_id, wallet_id, kind, amount_minor, currency, status, result_balance_minor, result_currency, result_wallet_version, created_at, updated_at) VALUES ($1,'INTERNAL',$2,$3,'OPENING',$4,$5,'PROCESSED',$6,$7,$8,$9,$9)`, t.ID(), t.PlayerID(), t.WalletID(), t.Amount().MinorUnits(), t.Amount().Currency(), resultingBalance.MinorUnits(), resultingBalance.Currency(), walletVersion, t.CreatedAt())
	return classify(err)
}
