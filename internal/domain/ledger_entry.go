package domain

type LedgerDirection string

const (
	LedgerDebit  LedgerDirection = "DEBIT"
	LedgerCredit LedgerDirection = "CREDIT"
)

type WalletLedgerEntry struct {
	id, walletID, transactionID string
	direction                   LedgerDirection
	amount, before, after       Money
}

func NewWalletLedgerEntry(id, walletID, transactionID string, direction LedgerDirection, amount, before, after Money) (WalletLedgerEntry, error) {
	if id == "" || walletID == "" || transactionID == "" || (direction != LedgerDebit && direction != LedgerCredit) || amount.MinorUnits() <= 0 || before.Currency() != amount.Currency() || after.Currency() != amount.Currency() {
		return WalletLedgerEntry{}, ErrInvalidLedgerEntry
	}
	var expected Money
	var err error
	if direction == LedgerCredit {
		expected, err = before.Add(amount)
	} else {
		expected, err = before.Subtract(amount)
	}
	if err != nil || expected.MinorUnits() != after.MinorUnits() {
		return WalletLedgerEntry{}, ErrInvalidLedgerEntry
	}
	return WalletLedgerEntry{id: id, walletID: walletID, transactionID: transactionID, direction: direction, amount: amount, before: before, after: after}, nil
}

func (e WalletLedgerEntry) ID() string                 { return e.id }
func (e WalletLedgerEntry) WalletID() string           { return e.walletID }
func (e WalletLedgerEntry) TransactionID() string      { return e.transactionID }
func (e WalletLedgerEntry) Direction() LedgerDirection { return e.direction }
func (e WalletLedgerEntry) Amount() Money              { return e.amount }
func (e WalletLedgerEntry) Before() Money              { return e.before }
func (e WalletLedgerEntry) After() Money               { return e.after }
