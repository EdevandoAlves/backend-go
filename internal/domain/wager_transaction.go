package domain

import (
	"strings"
	"time"
)

type WagerTransactionType string

const (
	TransactionOpening  WagerTransactionType = "OPENING"
	TransactionBet      WagerTransactionType = "BET"
	TransactionWin      WagerTransactionType = "WIN"
	TransactionLoss     WagerTransactionType = "LOSS"
	TransactionRefund   WagerTransactionType = "REFUND"
	TransactionRollback WagerTransactionType = "ROLLBACK"
)

type WagerTransactionStatus string

const (
	TransactionPending          WagerTransactionStatus = "PENDING"
	TransactionPendingReference WagerTransactionStatus = "PENDING_REFERENCE"
	TransactionProcessed        WagerTransactionStatus = "PROCESSED"
	TransactionRejected         WagerTransactionStatus = "REJECTED"
	TransactionFailed           WagerTransactionStatus = "FAILED"
)

type WagerTransaction struct {
	id, externalID, providerID, playerID, walletID string
	idempotencyKey, payloadHash, gameID, roundID   string
	type_                                          WagerTransactionType
	amount                                         Money
	status                                         WagerTransactionStatus
	referenceExternalID, referenceTransactionID    string
	failureCode                                    string
	result                                         *WagerTransactionResult
	createdAt, updatedAt                           time.Time
}

type WagerTransactionResult struct {
	Balance       Money
	WalletVersion int64
}

type WagerTransactionInput struct {
	ID, ExternalID, ProviderID, PlayerID, WalletID string
	IdempotencyKey, PayloadHash, GameID, RoundID   string
	Kind                                           WagerTransactionType
	Amount                                         Money
	ReferenceExternalID                            string
}

type RehydratedWagerTransaction struct {
	WagerTransactionInput
	Status                 WagerTransactionStatus
	ReferenceTransactionID string
	FailureCode            string
	Result                 *WagerTransactionResult
	CreatedAt, UpdatedAt   time.Time
}

func CreateExternalWagerTransaction(input WagerTransactionInput, now time.Time) (WagerTransaction, error) {
	amount, err := validateTransactionMoney(input.Amount)
	if err != nil || blank(input.ID) || blank(input.ExternalID) || blank(input.ProviderID) || blank(input.PlayerID) || blank(input.WalletID) || blank(input.IdempotencyKey) || blank(input.PayloadHash) || blank(input.GameID) || blank(input.RoundID) || !validTransactionType(input.Kind) || input.Kind == TransactionOpening || !validExternalAmount(input.Kind, amount) || !validReference(input.Kind, input.ReferenceExternalID) {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	return WagerTransaction{id: input.ID, externalID: input.ExternalID, providerID: input.ProviderID, playerID: input.PlayerID, walletID: input.WalletID, idempotencyKey: input.IdempotencyKey, payloadHash: input.PayloadHash, gameID: input.GameID, roundID: input.RoundID, type_: input.Kind, amount: amount, status: TransactionPending, referenceExternalID: input.ReferenceExternalID, createdAt: now, updatedAt: now}, nil
}

func CreateWagerTransaction(id, externalID, providerID, playerID, walletID string, kind WagerTransactionType, amount Money, now time.Time) (WagerTransaction, error) {
	return WagerTransaction{}, ErrInvalidTransaction
}

func CreateOpeningWagerTransaction(id, playerID, walletID string, amount Money, now time.Time) (WagerTransaction, error) {
	amount, err := validateTransactionMoney(amount)
	if err != nil || id == "" || playerID == "" || walletID == "" || amount.MinorUnits() <= 0 {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	return WagerTransaction{id: id, playerID: playerID, walletID: walletID, type_: TransactionOpening, amount: amount, status: TransactionProcessed, result: &WagerTransactionResult{Balance: amount, WalletVersion: 1}, createdAt: now, updatedAt: now}, nil
}

func RehydrateExternalWagerTransaction(input RehydratedWagerTransaction) (WagerTransaction, error) {
	amount, err := validateTransactionMoney(input.Amount)
	if err != nil {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	if input.Kind == TransactionOpening {
		if blank(input.ID) || blank(input.PlayerID) || blank(input.WalletID) || input.ExternalID != "" || input.ProviderID != "" || input.IdempotencyKey != "" || input.PayloadHash != "" || input.GameID != "" || input.RoundID != "" || input.ReferenceExternalID != "" || input.ReferenceTransactionID != "" || input.FailureCode != "" || input.Status != TransactionProcessed || amount.MinorUnits() <= 0 || !validResult(input.Result, amount) || input.Result.Balance.MinorUnits() != amount.MinorUnits() || input.Result.WalletVersion != 1 {
			return WagerTransaction{}, ErrInvalidTransaction
		}
		result := *input.Result
		result.Balance = amount
		return WagerTransaction{id: input.ID, playerID: input.PlayerID, walletID: input.WalletID, type_: input.Kind, amount: amount, status: input.Status, result: &result, createdAt: input.CreatedAt, updatedAt: input.UpdatedAt}, nil
	}
	tx, err := CreateExternalWagerTransaction(input.WagerTransactionInput, input.CreatedAt)
	if err != nil || !validTransactionStatus(input.Status) || !validReference(input.Kind, input.ReferenceExternalID) {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	if input.Status == TransactionProcessed || input.Status == TransactionRejected {
		if !validResult(input.Result, amount, input.Status == TransactionRejected && input.FailureCode == "CURRENCY_MISMATCH") || input.FailureCode != "" && input.Status == TransactionProcessed {
			return WagerTransaction{}, ErrInvalidTransaction
		}
	} else if input.Status == TransactionFailed {
		if input.Result != nil || !validFailureCode(input.FailureCode) {
			return WagerTransaction{}, ErrInvalidTransaction
		}
	} else if input.Result != nil || input.FailureCode != "" {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	if input.Status == TransactionRejected && !validFailureCode(input.FailureCode) {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	if input.Result != nil {
		resultAmount, resultErr := validateTransactionMoney(input.Result.Balance)
		if resultErr != nil || input.Result.WalletVersion < 1 {
			return WagerTransaction{}, ErrInvalidTransaction
		}
		input.Result.Balance = resultAmount
	}
	tx.status, tx.referenceTransactionID, tx.failureCode, tx.result, tx.updatedAt = input.Status, input.ReferenceTransactionID, input.FailureCode, input.Result, input.UpdatedAt
	return tx, nil
}

func RehydrateWagerTransaction(id, externalID, providerID, playerID, walletID string, kind WagerTransactionType, amount Money, status WagerTransactionStatus, referenceExternalID string, createdAt, updatedAt time.Time) (WagerTransaction, error) {
	return WagerTransaction{}, ErrInvalidTransaction
}

func blank(value string) bool { return strings.TrimSpace(value) == "" }

func validReference(kind WagerTransactionType, reference string) bool {
	hasReference := !blank(reference)
	switch kind {
	case TransactionRefund, TransactionRollback:
		return hasReference
	case TransactionWin:
		return reference == "" || hasReference
	case TransactionBet, TransactionLoss:
		return !hasReference
	default:
		return false
	}
}

func validResult(result *WagerTransactionResult, amount Money, allowCurrencyMismatch ...bool) bool {
	if result == nil || result.WalletVersion < 1 || result.Balance.MinorUnits() < 0 {
		return false
	}
	return len(allowCurrencyMismatch) > 0 && allowCurrencyMismatch[0] || result.Balance.Currency() == amount.Currency()
}

func validFailureCode(code string) bool {
	if blank(code) {
		return false
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func validateTransactionMoney(amount Money) (Money, error) {
	return NewMoneyForInternal(amount.MinorUnits(), amount.Currency())
}

func (t WagerTransaction) ID() string                     { return t.id }
func (t WagerTransaction) ExternalID() string             { return t.externalID }
func (t WagerTransaction) ProviderID() string             { return t.providerID }
func (t WagerTransaction) PlayerID() string               { return t.playerID }
func (t WagerTransaction) WalletID() string               { return t.walletID }
func (t WagerTransaction) Type() WagerTransactionType     { return t.type_ }
func (t WagerTransaction) Amount() Money                  { return t.amount }
func (t WagerTransaction) Status() WagerTransactionStatus { return t.status }
func (t WagerTransaction) ReferenceExternalID() string    { return t.referenceExternalID }
func (t WagerTransaction) IdempotencyKey() string         { return t.idempotencyKey }
func (t WagerTransaction) PayloadHash() string            { return t.payloadHash }
func (t WagerTransaction) GameID() string                 { return t.gameID }
func (t WagerTransaction) RoundID() string                { return t.roundID }
func (t WagerTransaction) ReferenceTransactionID() string { return t.referenceTransactionID }
func (t WagerTransaction) FailureCode() string            { return t.failureCode }
func (t WagerTransaction) Result() (WagerTransactionResult, bool) {
	if t.result == nil {
		return WagerTransactionResult{}, false
	}
	return *t.result, true
}
func (t WagerTransaction) CreatedAt() time.Time { return t.createdAt }
func (t WagerTransaction) UpdatedAt() time.Time { return t.updatedAt }

func (t *WagerTransaction) Process(result WagerTransactionResult, now time.Time) error {
	if t.status != TransactionPending {
		return ErrTerminalTransaction
	}
	if !validResult(&result, t.amount) {
		return ErrInvalidTransaction
	}
	t.status, t.result, t.updatedAt = TransactionProcessed, &result, now
	return nil
}

func (t *WagerTransaction) Reject(failureCode string, result WagerTransactionResult, now time.Time) error {
	if t.status != TransactionPending {
		return ErrTerminalTransaction
	}
	if !validFailureCode(failureCode) || !validResult(&result, t.amount, failureCode == "CURRENCY_MISMATCH") {
		return ErrInvalidTransaction
	}
	t.status, t.failureCode, t.result, t.updatedAt = TransactionRejected, failureCode, &result, now
	return nil
}

func (t *WagerTransaction) Transition(status WagerTransactionStatus, now time.Time) error {
	if !validTransactionStatus(status) {
		return ErrInvalidTransition
	}
	if t.status == TransactionProcessed || t.status == TransactionRejected || t.status == TransactionFailed {
		return ErrTerminalTransaction
	}
	if t.status == TransactionPending && status == TransactionPending {
		return ErrInvalidTransition
	}
	if t.status == TransactionPendingReference && status != TransactionProcessed && status != TransactionRejected && status != TransactionFailed {
		return ErrInvalidTransition
	}
	t.status, t.updatedAt = status, now
	return nil
}

func validTransactionType(kind WagerTransactionType) bool {
	switch kind {
	case TransactionOpening, TransactionBet, TransactionWin, TransactionLoss, TransactionRefund, TransactionRollback:
		return true
	default:
		return false
	}
}

func validExternalAmount(kind WagerTransactionType, amount Money) bool {
	if kind == TransactionLoss {
		return amount.MinorUnits() == 0
	}
	return amount.MinorUnits() > 0
}

func validTransactionStatus(status WagerTransactionStatus) bool {
	return status == TransactionPending || status == TransactionPendingReference || status == TransactionProcessed || status == TransactionRejected || status == TransactionFailed
}
