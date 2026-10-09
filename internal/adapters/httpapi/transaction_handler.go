package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"io"
	"mime"
	"net/http"
	"time"
)

type WagerExecutor interface {
	ExecuteResult(context.Context, application.ProcessWagerCommand) (application.ProcessWagerResult, error)
}
type IDFunc func() string
type TransactionHandler struct {
	Identity ProviderIdentity
	Executor WagerExecutor
	Now      func() time.Time
	ID       IDFunc
	MaxBytes int64
}
type wagerRequest struct {
	ProviderID          string                      `json:"providerId"`
	ExternalID          string                      `json:"externalTransactionId"`
	PlayerID            string                      `json:"playerId"`
	WalletID            string                      `json:"walletId"`
	GameID              string                      `json:"gameId"`
	RoundID             string                      `json:"roundId"`
	Kind                domain.WagerTransactionType `json:"kind"`
	Amount              domain.Money                `json:"money"`
	ReferenceExternalID string                      `json:"referenceExternalTransactionId"`
}
type wagerResponse struct {
	TransactionID    string       `json:"transactionId"`
	Status           string       `json:"status"`
	Balance          domain.Money `json:"balance"`
	IdempotentReplay bool         `json:"idempotentReplay"`
	FailureCode      string       `json:"failureCode,omitempty"`
}

func (h TransactionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if h.Identity == nil {
		http.Error(w, "identity unavailable", 503)
		return
	}
	p, e := h.Identity.Authenticate(r)
	if e != nil {
		if errors.Is(e, ErrUnauthenticated) {
			http.Error(w, "unauthorized", 401)
		} else {
			http.Error(w, "identity unavailable", 503)
		}
		return
	}
	mediaType, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || mediaType != "application/json" {
		http.Error(w, "invalid content type", 400)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		http.Error(w, "missing idempotency key", 400)
		return
	}
	max := h.MaxBytes
	if max == 0 {
		max = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in wagerRequest
	if e = dec.Decode(&in); e != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	var extra any
	if e = dec.Decode(&extra); e != io.EOF {
		http.Error(w, "one json document required", 400)
		return
	}
	if in.ProviderID != p.ProviderID {
		http.Error(w, "provider mismatch", 403)
		return
	}
	if in.Kind != domain.TransactionBet && in.Kind != domain.TransactionWin && in.Kind != domain.TransactionLoss && in.Kind != domain.TransactionRefund && in.Kind != domain.TransactionRollback {
		http.Error(w, "invalid kind", 400)
		return
	}
	if ((in.Kind == domain.TransactionRefund || in.Kind == domain.TransactionRollback) && in.ReferenceExternalID == "") || ((in.Kind == domain.TransactionBet || in.Kind == domain.TransactionLoss) && in.ReferenceExternalID != "") {
		http.Error(w, "invalid reference", 400)
		return
	}
	if h.Executor == nil || h.Now == nil || h.ID == nil {
		http.Error(w, "service unavailable", 503)
		return
	}
	id := h.ID()
	now := h.Now()
	res, e := h.Executor.ExecuteResult(r.Context(), application.ProcessWagerCommand{ID: id, ProviderID: p.ProviderID, ExternalID: in.ExternalID, IdempotencyKey: key, PlayerID: in.PlayerID, WalletID: in.WalletID, GameID: in.GameID, RoundID: in.RoundID, Kind: in.Kind, Amount: in.Amount, ReferenceExternalID: in.ReferenceExternalID, Now: now, TransactionID: id + "-ledger", ProcessedEventID: id + "-processed", RejectedEventID: id + "-rejected", BalanceEventID: id + "-balance"})
	if e != nil {
		switch {
		case errors.Is(e, application.ErrWalletNotFound):
			http.Error(w, "not found", 404)
		case errors.Is(e, application.ErrConflict), errors.Is(e, application.ErrIdempotencyConflict), errors.Is(e, application.ErrExternalIDConflict):
			http.Error(w, "conflict", 409)
		case errors.Is(e, domain.ErrInvalidTransaction):
			http.Error(w, "invalid transaction", 400)
		default:
			http.Error(w, "internal error", 500)
		}
		return
	}
	status := 201
	responseID := res.TransactionID
	if responseID == "" {
		responseID = id
	}
	if res.IdempotentReplay {
		status = 200
	}
	if res.Status == domain.TransactionRejected && !res.IdempotentReplay {
		status = 422
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(wagerResponse{TransactionID: responseID, Status: string(res.Status), Balance: res.Balance, IdempotentReplay: res.IdempotentReplay, FailureCode: res.FailureCode})
}
