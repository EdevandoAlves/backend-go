package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
)

type fakeInternalIdentity struct{ err error }

func (f fakeInternalIdentity) AuthenticateInternal(*http.Request) (InternalPrincipal, error) {
	return InternalPrincipal{}, f.err
}

type fakeWalletOpener struct {
	command application.OpenWalletCommand
	err     error
}

func (f *fakeWalletOpener) Execute(_ context.Context, command application.OpenWalletCommand) error {
	f.command = command
	return f.err
}

func TestInternalWalletHandlerOpensPositiveWallet(t *testing.T) {
	amount, _ := domain.ParseMoney("10.00", "BRL")
	opener := &fakeWalletOpener{}
	handler := InternalWalletHandler{Identity: fakeInternalIdentity{}, Opener: opener, Now: func() time.Time { return time.Unix(10, 0) }, ID: func() string { return "generated" }}
	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"playerId":"player","initialBalance":{"amount":"10.00","currency":"BRL"}}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated || opener.command.PlayerID != "player" || opener.command.InitialBalance != amount || opener.command.OpeningID == "" {
		t.Fatalf("response=%d body=%s command=%#v", res.Code, res.Body.String(), opener.command)
	}
}

func TestInternalWalletHandlerZeroSkipsFinancialArtifacts(t *testing.T) {
	zero, _ := domain.Zero("BRL")
	opener := &fakeWalletOpener{}
	handler := InternalWalletHandler{Identity: fakeInternalIdentity{}, Opener: opener, Now: time.Now, ID: func() string { return "generated" }}
	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"playerId":"player","initialBalance":{"amount":"0.00","currency":"BRL"}}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated || opener.command.InitialBalance != zero || opener.command.OpeningID != "" {
		t.Fatalf("response=%d command=%#v", res.Code, opener.command)
	}
}

func TestInternalWalletHandlerRejectsProviderOrMissingScope(t *testing.T) {
	for _, identityErr := range []error{ErrForbidden, ErrUnauthenticated} {
		handler := InternalWalletHandler{Identity: fakeInternalIdentity{err: identityErr}, Opener: &fakeWalletOpener{}, Now: time.Now, ID: func() string { return "id" }}
		req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{}`))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		want := http.StatusForbidden
		if errors.Is(identityErr, ErrUnauthenticated) {
			want = http.StatusUnauthorized
		}
		if res.Code != want {
			t.Fatalf("error=%v status=%d want=%d", identityErr, res.Code, want)
		}
	}
}

func TestInternalWalletHandlerMapsConflict(t *testing.T) {
	handler := InternalWalletHandler{Identity: fakeInternalIdentity{}, Opener: &fakeWalletOpener{err: application.ErrConflict}, Now: time.Now, ID: func() string { return "id" }}
	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"playerId":"player","initialBalance":{"amount":"0.00","currency":"BRL"}}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("status=%d", res.Code)
	}
}
