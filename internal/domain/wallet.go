package domain

import (
	"time"
)

type Wallet struct {
	id        string
	playerID  string
	currency  string
	balance   Money
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

func CreateWallet(id, playerID string, balance Money, now time.Time) (Wallet, error) {
	if id == "" || playerID == "" || balance.Currency() == "" || balance.MinorUnits() < 0 {
		return Wallet{}, ErrInvalidWallet
	}
	if _, err := NewMoneyForInternal(balance.MinorUnits(), balance.Currency()); err != nil {
		return Wallet{}, err
	}
	return Wallet{id: id, playerID: playerID, currency: balance.Currency(), balance: balance, version: 1, createdAt: now, updatedAt: now}, nil
}

func RehydrateWallet(id, playerID, currency string, balance Money, version int64, createdAt, updatedAt time.Time) (Wallet, error) {
	if id == "" || playerID == "" || version < 1 || balance.Currency() != currency || balance.MinorUnits() < 0 {
		return Wallet{}, ErrInvalidWallet
	}
	if _, err := NewMoneyForInternal(balance.MinorUnits(), currency); err != nil {
		return Wallet{}, err
	}
	return Wallet{id: id, playerID: playerID, currency: currency, balance: balance, version: version, createdAt: createdAt, updatedAt: updatedAt}, nil
}

func (w Wallet) ID() string           { return w.id }
func (w Wallet) PlayerID() string     { return w.playerID }
func (w Wallet) Currency() string     { return w.currency }
func (w Wallet) Balance() Money       { return w.balance }
func (w Wallet) Version() int64       { return w.version }
func (w Wallet) CreatedAt() time.Time { return w.createdAt }
func (w Wallet) UpdatedAt() time.Time { return w.updatedAt }

func (w *Wallet) Credit(amount Money, now time.Time) error {
	if err := sameCurrency(w.balance, amount); err != nil {
		return err
	}
	if amount.MinorUnits() < 0 {
		return ErrInvalidMoney
	}
	updated, err := w.balance.Add(amount)
	if err != nil {
		return err
	}
	if amount.MinorUnits() == 0 {
		return nil
	}
	w.balance, w.version, w.updatedAt = updated, w.version+1, now
	return nil
}

func (w *Wallet) Debit(amount Money, now time.Time) error {
	if err := sameCurrency(w.balance, amount); err != nil {
		return err
	}
	if amount.MinorUnits() < 0 {
		return ErrInvalidMoney
	}
	if amount.MinorUnits() > w.balance.MinorUnits() {
		return ErrInsufficientBalance
	}
	updated, err := w.balance.Subtract(amount)
	if err != nil {
		return err
	}
	if amount.MinorUnits() == 0 {
		return nil
	}
	w.balance, w.version, w.updatedAt = updated, w.version+1, now
	return nil
}
