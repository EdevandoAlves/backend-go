package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/adapters/postgres"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type OutboxPublisher struct {
	Repository postgres.OutboxRepository
	Client     *Client
	WorkerID   string
	Lease      time.Duration
}

type outboxEnvelope struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   string          `json:"aggregateId"`
	TransactionID string          `json:"transactionId"`
	CorrelationID string          `json:"correlationId"`
	Version       int64           `json:"version"`
	OccurredAt    time.Time       `json:"occurredAt"`
	Data          json.RawMessage `json:"data"`
}

// PublishOnce claims and commits the claim before doing network I/O.
func (p OutboxPublisher) PublishOnce(ctx context.Context) error {
	lease := p.Lease
	if lease <= 0 {
		lease = 30 * time.Second
	}
	worker := p.WorkerID
	if worker == "" {
		worker = "outbox-publisher"
	}
	c, err := p.Repository.Claim(ctx, worker, lease)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	body, err := json.Marshal(outboxEnvelope{c.ID, c.EventType, c.AggregateID, c.TransactionID, c.CorrelationID, c.EventVersion, c.OccurredAt, c.Payload})
	if err != nil {
		_ = p.Repository.MarkFailed(ctx, c.ID, worker, err)
		return err
	}
	_, err = p.Client.API.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String(p.Client.QueueURL), MessageBody: aws.String(string(body)), MessageGroupId: aws.String(c.AggregateID), MessageDeduplicationId: aws.String(c.ID)})
	if err != nil {
		markErr := p.Repository.MarkFailed(ctx, c.ID, worker, err)
		if markErr != nil {
			return errors.Join(err, markErr)
		}
		return err
	}
	return p.Repository.MarkPublished(ctx, c.ID, worker)
}

func (p OutboxPublisher) Run(ctx context.Context) error {
	for {
		if err := p.PublishOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		t := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}
