package domain

import "time"

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
	type_                                          WagerTransactionType
	amount                                         Money
	status                                         WagerTransactionStatus
	referenceExternalID                            string
	createdAt, updatedAt                           time.Time
}

func CreateWagerTransaction(id, externalID, providerID, playerID, walletID string, kind WagerTransactionType, amount Money, now time.Time) (WagerTransaction, error) {
	if id == "" || externalID == "" || providerID == "" || playerID == "" || walletID == "" || !validTransactionType(kind) || amount.MinorUnits() < 0 {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	return WagerTransaction{id: id, externalID: externalID, providerID: providerID, playerID: playerID, walletID: walletID, type_: kind, amount: amount, status: TransactionPending, createdAt: now, updatedAt: now}, nil
}

func RehydrateWagerTransaction(id, externalID, providerID, playerID, walletID string, kind WagerTransactionType, amount Money, status WagerTransactionStatus, referenceExternalID string, createdAt, updatedAt time.Time) (WagerTransaction, error) {
	tx, err := CreateWagerTransaction(id, externalID, providerID, playerID, walletID, kind, amount, createdAt)
	if err != nil || !validTransactionStatus(status) {
		return WagerTransaction{}, ErrInvalidTransaction
	}
	tx.status, tx.referenceExternalID, tx.updatedAt = status, referenceExternalID, updatedAt
	return tx, nil
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
func (t WagerTransaction) CreatedAt() time.Time           { return t.createdAt }
func (t WagerTransaction) UpdatedAt() time.Time           { return t.updatedAt }

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
func validTransactionStatus(status WagerTransactionStatus) bool {
	return status == TransactionPending || status == TransactionPendingReference || status == TransactionProcessed || status == TransactionRejected || status == TransactionFailed
}
