package app

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"github.com/EdevandoAlves/backend-go/internal/adapters/httpapi"
	"github.com/EdevandoAlves/backend-go/internal/config"
	"go.uber.org/fx"
)

func RegisterHTTPServer(lifecycle fx.Lifecycle, cfg config.Config, readiness *httpapi.Readiness, handler http.Handler, logger *slog.Logger) {
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler}
	var listener net.Listener
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			var err error
			listener, err = (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
			if err != nil {
				return err
			}
			readiness.Set(true)
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Error("HTTP server stopped unexpectedly", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			readiness.Set(false)
			return server.Shutdown(ctx)
		},
	})
}
