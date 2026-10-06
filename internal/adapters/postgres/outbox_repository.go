package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/jackc/pgx/v5"
)

func validPayload(payload []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(payload, &object) == nil
}

type OutboxRepository struct{}

func (OutboxRepository) Insert(ctx context.Context, tx pgx.Tx, e application.OutboxEvent) error {
	if !validPayload(e.Payload) {
		return errors.New("outbox payload must be a JSON object")
	}
	_, err := tx.Exec(ctx, `INSERT INTO outbox_events (id,aggregate_id,transaction_id,event_type,event_version,correlation_id,payload) VALUES ($1,$2,$3,$4,$5,$6,$7)`, e.ID, e.AggregateID, e.TransactionID, e.EventType, e.EventVersion, e.CorrelationID, e.Payload)
	return classify(err)
}
