package app

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"testing"

	"github.com/EdevandoAlves/backend-go/internal/adapters/httpapi"
	"github.com/EdevandoAlves/backend-go/internal/config"
	"go.uber.org/fx"
)

func TestHTTPServerStartFailsWhenAddressIsOccupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	readiness := httpapi.NewReadiness()
	application := fx.New(
		fx.Supply(config.Config{HTTPAddr: listener.Addr().String()}),
		fx.Supply(readiness),
		fx.Provide(func() http.Handler {
			return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		}),
		fx.Supply(slog.Default()),
		fx.Invoke(RegisterHTTPServer),
	)

	if err := application.Start(context.Background()); err == nil {
		t.Fatal("Start() succeeded with an occupied HTTP address")
	}
	if readiness.Ready() {
		t.Fatal("readiness became true after a failed start")
	}
}
