package domain

import "errors"

var (
	ErrInvalidMoney        = errors.New("invalid money")
	ErrInvalidCurrency     = errors.New("invalid currency")
	ErrMoneyOverflow       = errors.New("money overflow")
	ErrCurrencyMismatch    = errors.New("currency mismatch")
	ErrInvalidWallet       = errors.New("invalid wallet")
	ErrInvalidTransaction  = errors.New("invalid transaction")
	ErrInvalidLedgerEntry  = errors.New("invalid ledger entry")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrInvalidTransition   = errors.New("invalid transaction transition")
	ErrTerminalTransaction = errors.New("transaction is terminal")
)
