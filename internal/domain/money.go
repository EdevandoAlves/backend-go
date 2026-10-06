package domain

import (
	"encoding/json"
	"errors"
	"strconv"
)

type Money struct {
	cents    int64
	currency string
}

func ParseMoney(amount, currency string) (Money, error) {
	if err := validateCurrency(currency); err != nil {
		return Money{}, err
	}
	if len(amount) < 4 || amount[len(amount)-3] != '.' {
		return Money{}, ErrInvalidMoney
	}
	whole, fraction := amount[:len(amount)-3], amount[len(amount)-2:]
	if whole == "" || fraction[0] < '0' || fraction[0] > '9' || fraction[1] < '0' || fraction[1] > '9' {
		return Money{}, ErrInvalidMoney
	}
	var wholeUnits int64
	maxDollars := (maxInt64 - int64(fraction[0]-'0')*10 - int64(fraction[1]-'0')) / 100
	for i := 0; i < len(whole); i++ {
		if whole[i] < '0' || whole[i] > '9' {
			return Money{}, ErrInvalidMoney
		}
		digit := int64(whole[i] - '0')
		if wholeUnits > (maxDollars-digit)/10 {
			return Money{}, ErrMoneyOverflow
		}
		wholeUnits = wholeUnits*10 + digit
	}
	minor := int64(fraction[0]-'0')*10 + int64(fraction[1]-'0')
	if wholeUnits > (maxInt64-minor)/100 {
		return Money{}, ErrMoneyOverflow
	}
	return Money{cents: wholeUnits*100 + minor, currency: currency}, nil
}

const (
	maxInt64 = int64(^uint64(0) >> 1)
	minInt64 = -maxInt64 - 1
)

func NewMoneyForInternal(minor int64, currency string) (Money, error) {
	if err := validateCurrency(currency); err != nil {
		return Money{}, err
	}
	return Money{cents: minor, currency: currency}, nil
}

func Zero(currency string) (Money, error) { return NewMoneyForInternal(0, currency) }

func (m Money) MinorUnits() int64 { return m.cents }
func (m Money) Currency() string  { return m.currency }

func (m Money) String() string {
	return amountString(m.cents) + " " + m.currency
}

func (m Money) Add(other Money) (Money, error) {
	if err := m.validate(); err != nil {
		return Money{}, err
	}
	if err := other.validate(); err != nil {
		return Money{}, err
	}
	if err := sameCurrency(m, other); err != nil {
		return Money{}, err
	}
	if other.cents > 0 && m.cents > maxInt64-other.cents {
		return Money{}, ErrMoneyOverflow
	}
	if other.cents < 0 && m.cents < minInt64-other.cents {
		return Money{}, ErrMoneyOverflow
	}
	return Money{cents: m.cents + other.cents, currency: m.currency}, nil
}

func (m Money) Subtract(other Money) (Money, error) {
	if err := m.validate(); err != nil {
		return Money{}, err
	}
	if err := other.validate(); err != nil {
		return Money{}, err
	}
	if err := sameCurrency(m, other); err != nil {
		return Money{}, err
	}
	if other.cents == minInt64 {
		if m.cents >= 0 {
			return Money{}, ErrMoneyOverflow
		}
		return Money{cents: maxInt64 + (m.cents + 1), currency: m.currency}, nil
	}
	if other.cents < 0 && m.cents > maxInt64+other.cents {
		return Money{}, ErrMoneyOverflow
	}
	if other.cents > 0 && m.cents < minInt64+other.cents {
		return Money{}, ErrMoneyOverflow
	}
	return Money{cents: m.cents - other.cents, currency: m.currency}, nil
}

func (m Money) Negate() (Money, error) {
	if err := m.validate(); err != nil {
		return Money{}, err
	}
	if m.cents == minInt64 {
		return Money{}, ErrMoneyOverflow
	}
	return Money{cents: -m.cents, currency: m.currency}, nil
}

func (m Money) Compare(other Money) (int, error) {
	if err := sameCurrency(m, other); err != nil {
		return 0, err
	}
	if m.cents < other.cents {
		return -1, nil
	}
	if m.cents > other.cents {
		return 1, nil
	}
	return 0, nil
}

func (m Money) MarshalJSON() ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}{amountString(m.cents), m.currency})
}

func (m *Money) UnmarshalJSON(data []byte) error {
	var raw struct {
		Amount   json.RawMessage `json:"amount"`
		Currency string          `json:"currency"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return errors.Join(ErrInvalidMoney, err)
	}
	if len(raw.Amount) == 0 || raw.Amount[0] != '"' {
		return ErrInvalidMoney
	}
	var amount string
	if err := json.Unmarshal(raw.Amount, &amount); err != nil {
		return errors.Join(ErrInvalidMoney, err)
	}
	parsed, err := ParseMoney(amount, raw.Currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func validateCurrency(currency string) error {
	if currency != "BRL" && currency != "USD" {
		return ErrInvalidCurrency
	}
	return nil
}

func sameCurrency(a, b Money) error {
	if err := a.validate(); err != nil {
		return err
	}
	if err := b.validate(); err != nil {
		return err
	}
	if a.currency != b.currency {
		return ErrCurrencyMismatch
	}
	return nil
}

func (m Money) validate() error {
	if m.currency != "BRL" && m.currency != "USD" {
		return ErrInvalidMoney
	}
	return nil
}
func amountString(cents int64) string {
	if cents >= 0 {
		return strconv.FormatInt(cents/100, 10) + "." + twoDigits(cents%100)
	}
	whole := -(cents / 100)
	fraction := -(cents % 100)
	return "-" + strconv.FormatInt(whole, 10) + "." + twoDigits(fraction)
}
func twoDigits(n int64) string { return string([]byte{'0' + byte(n/10), '0' + byte(n%10)}) }

var _ json.Marshaler = Money{}
var _ json.Unmarshaler = (*Money)(nil)
