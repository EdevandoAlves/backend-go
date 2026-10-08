package sqs

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
)

var ErrInvalidEnvelope = errors.New("invalid wager request envelope")

type Envelope struct {
	MessageID  string    `json:"messageId"`
	Version    int       `json:"version"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurredAt"`
	Data       WagerData `json:"data"`
}
type WagerData struct {
	ProviderID          string                      `json:"providerId"`
	ExternalID          string                      `json:"externalTransactionId"`
	IdempotencyKey      string                      `json:"idempotencyKey"`
	PlayerID            string                      `json:"playerId"`
	WalletID            string                      `json:"walletId"`
	RoundID             string                      `json:"roundId"`
	GameID              string                      `json:"gameId"`
	Kind                domain.WagerTransactionType `json:"kind"`
	Money               domain.Money                `json:"money"`
	ReferenceExternalID string                      `json:"referenceExternalTransactionId"`
}

func ParseEnvelope(body string) (Envelope, application.ProcessWagerCommand, string, error) {
	var e Envelope
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil || e.MessageID == "" || e.Version != 1 || e.Type != "WagerTransactionRequested" || e.OccurredAt.IsZero() || e.Data.ProviderID == "" || e.Data.ExternalID == "" || e.Data.IdempotencyKey == "" || e.Data.PlayerID == "" || e.Data.WalletID == "" || e.Data.RoundID == "" || e.Data.GameID == "" {
		return Envelope{}, application.ProcessWagerCommand{}, "", ErrInvalidEnvelope
	}
	c := application.ProcessWagerCommand{ID: e.MessageID, ProviderID: e.Data.ProviderID, ExternalID: e.Data.ExternalID, IdempotencyKey: e.Data.IdempotencyKey, PlayerID: e.Data.PlayerID, WalletID: e.Data.WalletID, GameID: e.Data.GameID, RoundID: e.Data.RoundID, Kind: e.Data.Kind, Amount: e.Data.Money, ReferenceExternalID: e.Data.ReferenceExternalID, Now: time.Now().UTC(), TransactionID: e.MessageID + "-ledger", ProcessedEventID: e.MessageID + "-processed", RejectedEventID: e.MessageID + "-rejected", BalanceEventID: e.MessageID + "-balance"}
	hash, err := application.CanonicalWagerHash(c)
	if err != nil {
		return Envelope{}, application.ProcessWagerCommand{}, "", err
	}
	return e, c, hash, nil
}
