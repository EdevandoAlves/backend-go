package app

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/EdevandoAlves/backend-go/internal/adapters/httpapi"
	"github.com/EdevandoAlves/backend-go/internal/config"
	"go.uber.org/fx"
)

func NewLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(os.Stderr, nil)) }

func NewHandler(readiness *httpapi.Readiness) http.Handler {
	return httpapi.NewHealthHandler(readiness)
}

func Module() fx.Option {
	return fx.Options(
		fx.Provide(config.Load, NewLogger, httpapi.NewReadiness, NewHandler),
		fx.Invoke(RegisterHTTPServer),
	)
}
