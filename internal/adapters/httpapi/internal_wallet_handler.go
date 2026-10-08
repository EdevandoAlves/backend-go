package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
)

type WalletOpener interface {
	Execute(context.Context, application.OpenWalletCommand) error
}
type InternalWalletHandler struct {
	Identity InternalIdentity
	Opener   WalletOpener
	Now      func() time.Time
	ID       IDFunc
}
type walletRequest struct {
	PlayerID       string       `json:"playerId"`
	InitialBalance domain.Money `json:"initialBalance"`
}
type walletResponse struct {
	WalletID string       `json:"walletId"`
	PlayerID string       `json:"playerId"`
	Balance  domain.Money `json:"balance"`
}

func (h InternalWalletHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Identity == nil || h.Opener == nil || h.Now == nil || h.ID == nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := h.Identity.AuthenticateInternal(r); err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		} else if errors.Is(err, ErrForbidden) {
			http.Error(w, "forbidden", http.StatusForbidden)
		} else {
			http.Error(w, "identity unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "invalid content type", http.StatusBadRequest)
		return
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	var in walletRequest
	if err := dec.Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		http.Error(w, "one json document required", http.StatusBadRequest)
		return
	}
	if in.PlayerID == "" || in.InitialBalance.MinorUnits() < 0 {
		http.Error(w, "invalid wallet", http.StatusBadRequest)
		return
	}
	now, walletID := h.Now(), h.ID()
	command := application.OpenWalletCommand{WalletID: walletID, PlayerID: in.PlayerID, InitialBalance: in.InitialBalance, Now: now}
	if in.InitialBalance.MinorUnits() > 0 {
		command.OpeningID, command.LedgerID, command.ProcessedEventID, command.BalanceEventID = h.ID(), h.ID(), h.ID(), h.ID()
	}
	if err := h.Opener.Execute(r.Context(), command); err != nil {
		if errors.Is(err, application.ErrConflict) {
			http.Error(w, "conflict", http.StatusConflict)
		} else if errors.Is(err, domain.ErrInvalidWallet) {
			http.Error(w, "invalid wallet", http.StatusBadRequest)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(walletResponse{WalletID: walletID, PlayerID: in.PlayerID, Balance: in.InitialBalance})
}
