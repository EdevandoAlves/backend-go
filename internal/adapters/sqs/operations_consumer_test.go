package sqs

import (
	"errors"
	"testing"

	"github.com/EdevandoAlves/backend-go/internal/domain"
)

func TestParseEnvelopeUsesOfficialShapeAndCanonicalHash(t *testing.T) {
	body := `{"messageId":"m-1","version":1,"type":"WagerTransactionRequested","occurredAt":"2026-09-08T12:00:00Z","data":{"providerId":"p","externalTransactionId":"tx","idempotencyKey":"k","playerId":"pl","walletId":"w","roundId":"r","gameId":"g","kind":"BET","money":{"amount":"25.00","currency":"BRL"},"referenceExternalTransactionId":null}}`
	e, command, hash, err := ParseEnvelope(body)
	if err != nil || e.MessageID != "m-1" || command.ProviderID != "p" || len(hash) != 64 {
		t.Fatalf("parse result: envelope=%+v command=%+v hash=%q err=%v", e, command, hash, err)
	}
}

func TestParseEnvelopeRejectsWrongTypeAndUnknownFields(t *testing.T) {
	body := `{"messageId":"m-1","type":"Other","occurredAt":"2026-09-08T12:00:00Z","data":{}}`
	if _, _, _, err := ParseEnvelope(body); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected invalid envelope, got %v", err)
	}
}

func TestDeleteDecision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status domain.WagerTransactionStatus
		replay bool
		err    error
		want   bool
	}{
		{"processed", domain.TransactionProcessed, false, nil, true},
		{"rejected", domain.TransactionRejected, false, nil, true},
		{"pending reference", domain.TransactionPendingReference, false, nil, true},
		{"completed replay", domain.TransactionPending, true, nil, true},
		{"transient", domain.TransactionPending, false, errors.New("connection reset"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeleteDecision(tc.status, tc.replay, tc.err); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
