package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrInboxPayloadConflict = errors.New("inbox payload conflict")

type InboxRepository struct{}

func (InboxRepository) TryBegin(ctx context.Context, tx pgx.Tx, consumer, messageID, providerID, hash string) (fresh, completed bool, err error) {
	var inserted bool
	err = tx.QueryRow(ctx, `INSERT INTO inbox_messages (consumer_name,message_id,provider_id,payload_hash) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING RETURNING true`, consumer, messageID, providerID, hash).Scan(&inserted)
	if err == nil {
		return true, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, false, err
	}
	var oldHash string
	err = tx.QueryRow(ctx, `SELECT payload_hash, completed_at IS NOT NULL FROM inbox_messages WHERE consumer_name=$1 AND message_id=$2 FOR UPDATE`, consumer, messageID).Scan(&oldHash, &completed)
	if err != nil {
		return false, false, err
	}
	if oldHash != hash {
		return false, false, ErrInboxPayloadConflict
	}
	return false, completed, nil
}

func (InboxRepository) Complete(ctx context.Context, tx pgx.Tx, consumer, messageID, transactionID string) error {
	_, err := tx.Exec(ctx, `UPDATE inbox_messages SET transaction_id=$3, completed_at=now() WHERE consumer_name=$1 AND message_id=$2 AND completed_at IS NULL`, consumer, messageID, transactionID)
	return err
}
