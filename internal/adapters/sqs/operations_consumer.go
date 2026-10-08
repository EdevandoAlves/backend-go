package sqs

import (
	"context"
	"errors"
	"log/slog"

	"github.com/EdevandoAlves/backend-go/internal/adapters/postgres"
	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5"
)

type Consumer struct {
	Client            *Client
	Service           application.ProcessWagerService
	Inbox             postgres.InboxRepository
	ConsumerName      string
	Logger            *slog.Logger
	AfterProcessWager func() error
}

func DeleteDecision(status domain.WagerTransactionStatus, replay bool, err error) bool {
	if err != nil {
		return false
	}
	return replay || status == domain.TransactionProcessed || status == domain.TransactionRejected || status == domain.TransactionPendingReference
}

func (c Consumer) ProcessMessage(ctx context.Context, msg types.Message) (bool, error) {
	if msg.Body == nil || msg.MessageId == nil {
		return true, ErrInvalidEnvelope
	}
	e, command, hash, err := ParseEnvelope(*msg.Body)
	if err != nil {
		return true, err
	}
	name := c.ConsumerName
	if name == "" {
		name = "wager-transactions"
	}
	var result application.ProcessWagerResult
	replay := false
	err = c.Service.Manager.WithinTransaction(ctx, func(tx pgx.Tx) error {
		fresh, completed, err := c.Inbox.TryBegin(ctx, tx, name, e.MessageID, command.ProviderID, hash)
		if err != nil {
			return err
		}
		if !fresh {
			if completed {
				replay = true
				return nil
			}
			return errors.New("inbox message in progress")
		}
		result, err = c.Service.ExecuteResultTx(ctx, tx, command)
		if err != nil {
			return err
		}
		if c.AfterProcessWager != nil {
			if err := c.AfterProcessWager(); err != nil {
				return err
			}
		}
		return c.Inbox.Complete(ctx, tx, name, e.MessageID, result.TransactionID)
	})
	if err != nil {
		return errors.Is(err, postgres.ErrInboxPayloadConflict), err
	}
	return DeleteDecision(result.Status, replay, nil), nil
}

func (c Consumer) Run(ctx context.Context) error {
	for {
		out, err := c.Client.API.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: &c.Client.QueueURL, MaxNumberOfMessages: 1, WaitTimeSeconds: 20, VisibilityTimeout: 30})
		if err != nil {
			return err
		}
		for _, msg := range out.Messages {
			del, err := c.ProcessMessage(ctx, msg)
			if err != nil {
				if c.Logger != nil {
					c.Logger.Error("sqs message processing failed", "error", err, "delete", del)
				}
				if !del {
					continue
				}
			}
			if del && msg.ReceiptHandle != nil {
				if _, err = c.Client.API.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: &c.Client.QueueURL, ReceiptHandle: msg.ReceiptHandle}); err != nil {
					if c.Logger != nil {
						c.Logger.Error("sqs message delete failed", "error", err)
					}
					return err
				}
			}
		}
	}
}
