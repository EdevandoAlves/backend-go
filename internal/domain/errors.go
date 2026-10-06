package domain

import "errors"

var (
	ErrInvalidMoney     = errors.New("invalid money")
	ErrInvalidCurrency  = errors.New("invalid currency")
	ErrMoneyOverflow    = errors.New("money overflow")
	ErrCurrencyMismatch = errors.New("currency mismatch")
)
