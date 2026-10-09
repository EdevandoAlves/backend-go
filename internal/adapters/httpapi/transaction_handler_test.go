package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
)

type fakeIdentity struct {
	principal ProviderPrincipal
	err       error
}

func (f fakeIdentity) Authenticate(*http.Request) (ProviderPrincipal, error) {
	return f.principal, f.err
}

type fakeExecutor struct {
	result  application.ProcessWagerResult
	err     error
	calls   int
	command application.ProcessWagerCommand
}

func (f *fakeExecutor) ExecuteResult(_ context.Context, c application.ProcessWagerCommand) (application.ProcessWagerResult, error) {
	f.calls++
	f.command = c
	return f.result, f.err
}

func request(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "idem")
	return r
}
func handler(exec *fakeExecutor, identity ProviderIdentity) TransactionHandler {
	return TransactionHandler{Identity: identity, Executor: exec, Now: func() time.Time { return time.Unix(100, 0) }, ID: func() string { return "generated" }}
}
func TestTransactionHandlerSecurityAndValidation(t *testing.T) {
	valid := `{"providerId":"provider","externalTransactionId":"external","playerId":"player","walletId":"wallet","gameId":"game","roundId":"round","kind":"BET","money":{"amount":"1.00","currency":"BRL"}}`
	tests := []struct {
		name, body, content string
		identity            ProviderIdentity
		want                int
		calls               int
	}{
		{"unavailable", valid, "application/json", fakeIdentity{err: ErrIdentityUnavailable}, 503, 0},
		{"unauthenticated", valid, "application/json", fakeIdentity{err: ErrUnauthenticated}, 401, 0},
		{"mismatch", strings.Replace(valid, `"providerId":"provider"`, `"providerId":"other"`, 1), "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 403, 0},
		{"number", strings.Replace(valid, `{"amount":"1.00","currency":"BRL"}`, `{"amount":1,"currency":"BRL"}`, 1), "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"unknown", strings.TrimSuffix(valid, "}") + `,"unknown":true}`, "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"legacy external id", strings.Replace(valid, "externalTransactionId", "externalId", 1), "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"second document", valid + valid, "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"content type", valid, "text/plain", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"refund requires reference", strings.Replace(valid, `"kind":"BET"`, `"kind":"REFUND"`, 1), "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"rollback requires reference", strings.Replace(valid, `"kind":"BET"`, `"kind":"ROLLBACK"`, 1), "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"bet rejects reference", strings.TrimSuffix(valid, "}") + `,"referenceExternalTransactionId":"reference"}`, "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
		{"loss rejects reference", strings.Replace(strings.TrimSuffix(valid, "}")+`,"referenceExternalTransactionId":"reference"}`, `"kind":"BET"`, `"kind":"LOSS"`, 1), "application/json", fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}, 400, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &fakeExecutor{}
			h := handler(e, tc.identity)
			r := request(tc.body)
			r.Header.Set("Content-Type", tc.content)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body)
			}
			if e.calls != tc.calls {
				t.Fatalf("calls=%d want=%d", e.calls, tc.calls)
			}
		})
	}
	t.Run("missing idempotency", func(t *testing.T) {
		e := &fakeExecutor{}
		r := request(valid)
		r.Header.Del("Idempotency-Key")
		w := httptest.NewRecorder()
		handler(e, fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}).ServeHTTP(w, r)
		if w.Code != 400 || e.calls != 0 {
			t.Fatalf("status=%d calls=%d", w.Code, e.calls)
		}
	})
}
func TestTransactionHandlerResponsesAndCommand(t *testing.T) {
	valid := `{"providerId":"provider","externalTransactionId":"external","playerId":"player","walletId":"wallet","gameId":"game","roundId":"round","kind":"BET","money":{"amount":"1.00","currency":"BRL"}}`
	t.Run("success", func(t *testing.T) {
		e := &fakeExecutor{result: application.ProcessWagerResult{Status: domain.TransactionProcessed, Balance: mustMoney(t, "9.00")}}
		r := request(valid)
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
		w := httptest.NewRecorder()
		handler(e, fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}).ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatal(w.Code)
		}
		if e.command.ProviderID != "provider" || e.command.ID != "generated" || e.command.TransactionID != "generated-ledger" {
			t.Fatalf("command=%+v", e.command)
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out["balance"].(map[string]any)["amount"] != "9.00" {
			t.Fatalf("response=%v", out)
		}
	})
	for _, tc := range []struct {
		kind, reference string
	}{
		{"WIN", ""},
		{"WIN", "win-reference"},
		{"REFUND", "refund-reference"},
		{"ROLLBACK", "rollback-reference"},
	} {
		t.Run(tc.kind+tc.reference, func(t *testing.T) {
			e := &fakeExecutor{result: application.ProcessWagerResult{Status: domain.TransactionProcessed}}
			body := strings.Replace(valid, `"kind":"BET"`, `"kind":"`+tc.kind+`"`, 1)
			if tc.reference != "" {
				body = strings.TrimSuffix(body, "}") + `,"referenceExternalTransactionId":"` + tc.reference + `"}`
			}
			w := httptest.NewRecorder()
			handler(e, fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}).ServeHTTP(w, request(body))
			if w.Code != http.StatusCreated || e.command.ReferenceExternalID != tc.reference || e.command.Kind != domain.WagerTransactionType(tc.kind) {
				t.Fatalf("status=%d command=%+v", w.Code, e.command)
			}
		})
	}
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{{"rejected", nil, 422}, {"not found", application.ErrWalletNotFound, 404}, {"conflict", application.ErrConflict, 409}, {"unexpected", errors.New("boom"), 500}} {
		t.Run(tc.name, func(t *testing.T) {
			e := &fakeExecutor{err: tc.err}
			if tc.name == "rejected" {
				e.result.Status = domain.TransactionRejected
				e.result.FailureCode = "INSUFFICIENT_FUNDS"
			}
			w := httptest.NewRecorder()
			handler(e, fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}).ServeHTTP(w, request(valid))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
		})
	}
}
func TestTransactionHandlerMethodAndNilDependencies(t *testing.T) {
	e := &fakeExecutor{}
	h := handler(e, fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	r = request(`{}`)
	w = httptest.NewRecorder()
	TransactionHandler{Executor: e, Now: h.Now, ID: h.ID}.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestTransactionHandlerReplayRejectedUsesPersistedResult(t *testing.T) {
	e := &fakeExecutor{result: application.ProcessWagerResult{
		TransactionID:    "original",
		Status:           domain.TransactionRejected,
		FailureCode:      "INSUFFICIENT_FUNDS",
		Balance:          mustMoney(t, "0.00"),
		IdempotentReplay: true,
	}}
	w := httptest.NewRecorder()
	handler(e, fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}).ServeHTTP(w, request(`{"providerId":"provider","externalTransactionId":"external","playerId":"player","walletId":"wallet","gameId":"game","roundId":"round","kind":"BET","money":{"amount":"1.00","currency":"BRL"}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out wagerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.TransactionID != "original" || !out.IdempotentReplay || out.FailureCode != "INSUFFICIENT_FUNDS" {
		t.Fatalf("response=%+v", out)
	}
}

func TestTransactionHandlerMapsIdempotencyAndExternalConflicts(t *testing.T) {
	for _, conflict := range []error{application.ErrIdempotencyConflict, application.ErrExternalIDConflict} {
		t.Run(conflict.Error(), func(t *testing.T) {
			e := &fakeExecutor{err: conflict}
			w := httptest.NewRecorder()
			handler(e, fakeIdentity{principal: ProviderPrincipal{ProviderID: "provider"}}).ServeHTTP(w, request(`{"providerId":"provider","externalTransactionId":"external","playerId":"player","walletId":"wallet","gameId":"game","roundId":"round","kind":"BET","money":{"amount":"1.00","currency":"BRL"}}`))
			if w.Code != http.StatusConflict {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}
func mustMoney(t *testing.T, s string) domain.Money {
	t.Helper()
	m, e := domain.ParseMoney(s, "BRL")
	if e != nil {
		t.Fatal(e)
	}
	return m
}
