package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func validPayload(payload []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(payload, &object) == nil
}

type OutboxRepository struct{ pool *pgxpool.Pool }

type OutboxClaim struct {
	ID, AggregateID, TransactionID, EventType, CorrelationID string
	EventVersion                                             int64
	Payload                                                  []byte
	OccurredAt                                               time.Time
	ClaimedBy                                                string
}

func NewOutboxRepository(pool *pgxpool.Pool) OutboxRepository { return OutboxRepository{pool: pool} }

// Claim reserves one due event durably. Expired reservations are deliberately
// eligible again so a worker crash cannot strand an event.
func (r OutboxRepository) Claim(ctx context.Context, worker string, lease time.Duration) (OutboxClaim, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return OutboxClaim{}, err
	}
	defer tx.Rollback(ctx)
	var c OutboxClaim
	err = tx.QueryRow(ctx, `
		UPDATE outbox_events SET claimed_by=$1, claim_expires_at=now()+$2::interval
		WHERE id=(SELECT id FROM outbox_events
		 WHERE published_at IS NULL AND next_attempt_at <= now()
		   AND (claimed_by IS NULL OR claim_expires_at <= now())
		 ORDER BY created_at, id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING id, aggregate_id, transaction_id, event_type, event_version, correlation_id, payload, created_at, claimed_by`, worker, fmt.Sprintf("%f seconds", lease.Seconds())).Scan(&c.ID, &c.AggregateID, &c.TransactionID, &c.EventType, &c.EventVersion, &c.CorrelationID, &c.Payload, &c.OccurredAt, &c.ClaimedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OutboxClaim{}, pgx.ErrNoRows
		}
		return OutboxClaim{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return OutboxClaim{}, err
	}
	return c, nil
}

func (r OutboxRepository) MarkPublished(ctx context.Context, id, worker string) error {
	result, err := r.pool.Exec(ctx, `UPDATE outbox_events SET published_at=now(), claimed_by=NULL, claim_expires_at=NULL WHERE id=$1 AND claimed_by=$2 AND published_at IS NULL`, id, worker)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (r OutboxRepository) MarkFailed(ctx context.Context, id, worker string, cause error) error {
	message := cause.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	result, err := r.pool.Exec(ctx, `UPDATE outbox_events SET attempts=attempts+1, next_attempt_at=now()+LEAST((power(2, attempts)::bigint * interval '1 second'), interval '300 seconds'), last_error=$3, claimed_by=NULL, claim_expires_at=NULL WHERE id=$1 AND claimed_by=$2 AND published_at IS NULL`, id, worker, message)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (OutboxRepository) Insert(ctx context.Context, tx pgx.Tx, e application.OutboxEvent) error {
	if !validPayload(e.Payload) {
		return errors.New("outbox payload must be a JSON object")
	}
	_, err := tx.Exec(ctx, `INSERT INTO outbox_events (id,aggregate_id,transaction_id,event_type,event_version,correlation_id,payload) VALUES ($1,$2,$3,$4,$5,$6,$7)`, e.ID, e.AggregateID, e.TransactionID, e.EventType, e.EventVersion, e.CorrelationID, e.Payload)
	return classify(err)
}
