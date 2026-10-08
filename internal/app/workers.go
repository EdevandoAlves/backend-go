package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/adapters/httpapi"
	"github.com/EdevandoAlves/backend-go/internal/adapters/sqs"
	"go.uber.org/fx"
)

func RegisterWorkers(lifecycle fx.Lifecycle, readiness *httpapi.Readiness, consumer *sqs.Consumer, publisher sqs.OutboxPublisher, logger *slog.Logger) {
	var cancel context.CancelFunc
	var wg sync.WaitGroup
	lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			ctx, stop := context.WithCancel(context.Background())
			cancel = stop
			wg.Add(2)
			go func() { defer wg.Done(); runConsumer(ctx, consumer, logger) }()
			go func() { defer wg.Done(); runPublisher(ctx, publisher, logger) }()
			readiness.Set(true)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
}

func runConsumer(ctx context.Context, consumer *sqs.Consumer, logger *slog.Logger) {
	for ctx.Err() == nil {
		if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("sqs consumer stopped; restarting", "error", err)
		}
		if ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func runPublisher(ctx context.Context, publisher sqs.OutboxPublisher, logger *slog.Logger) {
	for ctx.Err() == nil {
		if err := publisher.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("outbox publisher stopped; restarting", "error", err)
		}
		if ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}
