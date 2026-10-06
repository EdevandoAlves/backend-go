package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrNotFound = errors.New("not found")

func classify(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errors.Join(ErrConflict, err)
	}
	return err
}

type WalletRepository struct{}

func (WalletRepository) Insert(ctx context.Context, tx pgx.Tx, w domain.Wallet) error {
	_, err := tx.Exec(ctx, `INSERT INTO wallets (id, player_id, currency, balance_minor, version, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, w.ID(), w.PlayerID(), w.Currency(), w.Balance().MinorUnits(), w.Version(), w.CreatedAt(), w.UpdatedAt())
	return classify(err)
}

func (r WalletRepository) Get(ctx context.Context, tx pgx.Tx, id string) (domain.Wallet, error) {
	return r.get(ctx, tx, `SELECT id, player_id, currency, balance_minor, version, created_at, updated_at FROM wallets WHERE id=$1`, id)
}
func (r WalletRepository) GetForUpdate(ctx context.Context, tx pgx.Tx, id string) (domain.Wallet, error) {
	return r.get(ctx, tx, `SELECT id, player_id, currency, balance_minor, version, created_at, updated_at FROM wallets WHERE id=$1 FOR UPDATE`, id)
}

func (WalletRepository) get(ctx context.Context, tx pgx.Tx, query, id string) (domain.Wallet, error) {
	var walletID, playerID, currency string
	var balance, version int64
	var created, updated time.Time
	err := tx.QueryRow(ctx, query, id).Scan(&walletID, &playerID, &currency, &balance, &version, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, ErrNotFound
	}
	if err != nil {
		return domain.Wallet{}, err
	}
	money, err := domain.NewMoneyForInternal(balance, currency)
	if err != nil {
		return domain.Wallet{}, err
	}
	return domain.RehydrateWallet(walletID, playerID, currency, money, version, created, updated)
}

func (WalletRepository) Save(ctx context.Context, tx pgx.Tx, w domain.Wallet, expectedVersion int64) error {
	tag, err := tx.Exec(ctx, `UPDATE wallets SET balance_minor=$1, version=$2, updated_at=$3 WHERE id=$4 AND version=$5`, w.Balance().MinorUnits(), w.Version(), w.UpdatedAt(), w.ID(), expectedVersion)
	if err != nil {
		return classify(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}
