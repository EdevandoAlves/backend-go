package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/adapters/httpapi"
	oidcadapter "github.com/EdevandoAlves/backend-go/internal/adapters/oidc"
	"github.com/EdevandoAlves/backend-go/internal/adapters/postgres"
	"github.com/EdevandoAlves/backend-go/internal/adapters/sqs"
	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/config"
	"go.uber.org/fx"
)

func NewLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stderr, nil)) }

func NewHandler(readiness *httpapi.Readiness) http.Handler {
	return httpapi.NewHealthHandler(readiness)
}

func NewPool(cfg config.Config) (*postgres.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir("migrations")
	if err != nil {
		p.Close()
		return nil, err
	}
	if err := p.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		p.Close()
		return nil, err
	}
	for i := 1; i <= 3; i++ {
		prefix := fmt.Sprintf("%06d_", i)
		var file string
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), ".up.sql") {
				file = filepath.Join("migrations", entry.Name())
				break
			}
		}
		if file == "" {
			p.Close()
			return nil, fmt.Errorf("migration %06d not found", i)
		}
		var applied bool
		if err := p.Pool().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version=$1)`, i).Scan(&applied); err != nil {
			p.Close()
			return nil, err
		}
		if applied {
			continue
		}
		sql, err := os.ReadFile(file)
		if err != nil {
			p.Close()
			return nil, err
		}
		if err := p.Exec(ctx, string(sql)); err != nil {
			p.Close()
			return nil, fmt.Errorf("migration %s: %w", file, err)
		}
		if err := p.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, i); err != nil {
			p.Close()
			return nil, err
		}
	}
	return p, nil
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}

func NewProviderIdentity(cfg config.Config) (httpapi.ProviderIdentity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.OIDCDiscoveryTimeout)
	defer cancel()
	verifier, err := oidcadapter.NewTokenVerifier(ctx, cfg.OIDCIssuerURL, cfg.OIDCAudience)
	if err != nil {
		return nil, err
	}
	return httpapi.OIDCProviderIdentity{Verifier: verifier}, nil
}

func NewInternalIdentity(cfg config.Config) (httpapi.InternalIdentity, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.OIDCDiscoveryTimeout)
	defer cancel()
	verifier, err := oidcadapter.NewTokenVerifier(ctx, cfg.OIDCIssuerURL, cfg.OIDCAudience)
	if err != nil {
		return nil, err
	}
	return httpapi.OIDCInternalIdentity{Verifier: verifier}, nil
}

func NewTransactionHandler(pool *postgres.Pool, identity httpapi.ProviderIdentity) *httpapi.TransactionHandler {
	return &httpapi.TransactionHandler{Identity: identity, Executor: NewProcessWagerService(pool), Now: time.Now, ID: NewID}
}

func NewOpenWalletService(pool *postgres.Pool) application.OpenWalletService {
	return application.OpenWalletService{Manager: postgres.NewTxManager(pool), Wallets: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}}
}

func NewInternalWalletHandler(identity httpapi.InternalIdentity, opener application.OpenWalletService) *httpapi.InternalWalletHandler {
	return &httpapi.InternalWalletHandler{Identity: identity, Opener: opener, Now: time.Now, ID: NewID}
}

func NewProcessWagerService(pool *postgres.Pool) application.ProcessWagerService {
	return application.ProcessWagerService{Manager: postgres.NewTxManager(pool), Wallet: postgres.WalletRepository{}, Transactions: postgres.TransactionRepository{}, Ledger: postgres.LedgerRepository{}, Outbox: postgres.OutboxRepository{}, IsNotFound: func(err error) bool { return errors.Is(err, postgres.ErrNotFound) }}
}

func NewSQSClients(cfg config.Config) (*SQSClients, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	operations, err := sqs.NewClient(ctx, cfg.SQSRegion, cfg.SQSEndpoint, cfg.SQSOperationsQueueURL)
	if err != nil {
		return nil, err
	}
	events, err := sqs.NewClient(ctx, cfg.SQSRegion, cfg.SQSEndpoint, cfg.SQSEventsQueueURL)
	if err != nil {
		return nil, err
	}
	return &SQSClients{Operations: operations, Events: events}, nil
}

type SQSClients struct {
	Operations *sqs.Client
	Events     *sqs.Client
}

func NewOperationsConsumer(clients *SQSClients, service application.ProcessWagerService, logger *slog.Logger) *sqs.Consumer {
	return &sqs.Consumer{Client: clients.Operations, Service: service, Inbox: postgres.InboxRepository{}, ConsumerName: "wager-transactions", Logger: logger}
}

func NewOutboxPublisher(clients *SQSClients, pool *postgres.Pool) sqs.OutboxPublisher {
	return sqs.OutboxPublisher{Client: clients.Events, Repository: postgres.NewOutboxRepository(pool.Pool()), WorkerID: "outbox-publisher"}
}

func NewRouter(readiness *httpapi.Readiness, transactions *httpapi.TransactionHandler, wallets *httpapi.InternalWalletHandler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/health/", httpapi.NewHealthHandler(readiness))
	mux.Handle("/wagering/transactions", transactions)
	mux.Handle("/wallets", wallets)
	return mux
}

func Module() fx.Option {
	return fx.Options(
		fx.Provide(config.Load, NewLogger, httpapi.NewReadiness, NewPool, NewProviderIdentity, NewInternalIdentity, NewRouter, NewProcessWagerService, NewTransactionHandler, NewOpenWalletService, NewInternalWalletHandler, NewSQSClients, NewOperationsConsumer, NewOutboxPublisher),
		fx.Invoke(RegisterHTTPServer, RegisterPoolLifecycle, RegisterWorkers),
	)
}
