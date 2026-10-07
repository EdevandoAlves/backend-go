package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/EdevandoAlves/backend-go/internal/application"
	"github.com/EdevandoAlves/backend-go/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TransactionRepository struct{}

func (TransactionRepository) TryInsertExternalPending(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction) (bool, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,reference_external_id,created_at,updated_at) VALUES ($1,'EXTERNAL',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'PENDING',NULLIF($13,''),$14,$14) ON CONFLICT DO NOTHING RETURNING id`, t.ID(), t.ExternalID(), t.ProviderID(), t.IdempotencyKey(), t.PayloadHash(), t.PlayerID(), t.WalletID(), t.GameID(), t.RoundID(), t.Type(), t.Amount().MinorUnits(), t.Amount().Currency(), t.ReferenceExternalID(), t.CreatedAt()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, classifyApplication(err)
	}
	return true, nil
}

func (r TransactionRepository) GetExternalByIdempotencyKey(ctx context.Context, tx pgx.Tx, provider, key string) (domain.WagerTransaction, error) {
	return r.getExternal(ctx, tx, `WHERE origin='EXTERNAL' AND provider_id=$1 AND idempotency_key=$2`, provider, key)
}
func (r TransactionRepository) GetExternalByExternalID(ctx context.Context, tx pgx.Tx, provider, id string) (domain.WagerTransaction, error) {
	return r.getExternal(ctx, tx, `WHERE origin='EXTERNAL' AND provider_id=$1 AND external_id=$2`, provider, id)
}
func (r TransactionRepository) ClaimPendingReference(ctx context.Context, tx pgx.Tx, now time.Time) (domain.WagerTransaction, error) {
	return r.getExternal(ctx, tx, `WHERE origin='EXTERNAL' AND status='PENDING_REFERENCE' AND next_attempt_at <= $1 ORDER BY next_attempt_at, created_at LIMIT 1 FOR UPDATE SKIP LOCKED`, now)
}
func (r TransactionRepository) FindProcessedReversal(ctx context.Context, tx pgx.Tx, ref string) (domain.WagerTransaction, error) {
	return r.getExternal(ctx, tx, `WHERE reference_transaction_id=$1 AND status='PROCESSED'`, ref)
}
func (TransactionRepository) getExternal(ctx context.Context, tx pgx.Tx, suffix string, args ...any) (domain.WagerTransaction, error) {
	var id, external, provider, key, hash, player, wallet, game, round, kind, currency, status string
	var ref, refTx, failure *string
	var amount int64
	var balance *int64
	var resultCurrency *string
	var version *int64
	var created, updated time.Time
	var attempts int
	err := tx.QueryRow(ctx, `SELECT id,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,reference_external_id,reference_transaction_id,failure_code,result_balance_minor,result_currency,result_wallet_version,attempt_count,created_at,updated_at FROM wager_transactions `+suffix, args...).Scan(&id, &external, &provider, &key, &hash, &player, &wallet, &game, &round, &kind, &amount, &currency, &status, &ref, &refTx, &failure, &balance, &resultCurrency, &version, &attempts, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WagerTransaction{}, errors.Join(application.ErrNotFound, ErrNotFound, err)
	}
	if err != nil {
		return domain.WagerTransaction{}, err
	}
	m, err := domain.NewMoneyForInternal(amount, currency)
	if err != nil {
		return domain.WagerTransaction{}, err
	}
	var result *domain.WagerTransactionResult
	if balance != nil && resultCurrency != nil && version != nil {
		rm, e := domain.NewMoneyForInternal(*balance, *resultCurrency)
		if e != nil {
			return domain.WagerTransaction{}, e
		}
		result = &domain.WagerTransactionResult{Balance: rm, WalletVersion: *version}
	}
	in := domain.RehydratedWagerTransaction{WagerTransactionInput: domain.WagerTransactionInput{ID: id, ExternalID: external, ProviderID: provider, PlayerID: player, WalletID: wallet, IdempotencyKey: key, PayloadHash: hash, GameID: game, RoundID: round, Kind: domain.WagerTransactionType(kind), Amount: m}, Status: domain.WagerTransactionStatus(status), Result: result, CreatedAt: created, UpdatedAt: updated, AttemptCount: attempts}
	if ref != nil {
		in.ReferenceExternalID = *ref
	}
	if refTx != nil {
		in.ReferenceTransactionID = *refTx
	}
	if failure != nil {
		in.FailureCode = *failure
	}
	return domain.RehydrateExternalWagerTransaction(in)
}

func (TransactionRepository) ReschedulePendingReference(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction, next time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE wager_transactions SET attempt_count=$1,next_attempt_at=$2,updated_at=$3 WHERE id=$4 AND status='PENDING_REFERENCE'`, t.AttemptCount()+1, next, next, t.ID())
	if err != nil {
		return classifyApplication(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrConflict
	}
	return nil
}

func (TransactionRepository) InsertExternalPending(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction) error {
	_, err := tx.Exec(ctx, `INSERT INTO wager_transactions(id,origin,external_id,provider_id,idempotency_key,payload_hash,player_id,wallet_id,game_id,round_id,kind,amount_minor,currency,status,reference_external_id,created_at,updated_at) VALUES ($1,'EXTERNAL',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'PENDING',NULLIF($13,''),$14,$14)`, t.ID(), t.ExternalID(), t.ProviderID(), t.IdempotencyKey(), t.PayloadHash(), t.PlayerID(), t.WalletID(), t.GameID(), t.RoundID(), t.Type(), t.Amount().MinorUnits(), t.Amount().Currency(), t.ReferenceExternalID(), t.CreatedAt())
	return classifyApplication(err)
}
func (TransactionRepository) UpdateTerminal(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction) error {
	return (TransactionRepository{}).UpdateTerminalWithReference(ctx, tx, t, "")
}
func (TransactionRepository) UpdateTerminalWithReference(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction, ref string) error {
	return (TransactionRepository{}).UpdateTerminalFrom(ctx, tx, t, domain.TransactionPending, ref)
}
func (TransactionRepository) UpdateTerminalFrom(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction, expected domain.WagerTransactionStatus, ref string) error {
	r, ok := t.Result()
	if !ok {
		return errors.New("missing result")
	}
	tag, err := tx.Exec(ctx, `UPDATE wager_transactions SET status=$1,failure_code=$2,reference_transaction_id=NULLIF($3,''),result_balance_minor=$4,result_currency=$5,result_wallet_version=$6,attempt_count=CASE WHEN status='PENDING_REFERENCE' THEN attempt_count+1 ELSE attempt_count END,next_attempt_at=NULL,updated_at=$7 WHERE id=$8 AND status=$9`, t.Status(), nilIfEmpty(t.FailureCode()), ref, r.Balance.MinorUnits(), r.Balance.Currency(), r.WalletVersion, t.UpdatedAt(), t.ID(), expected)
	if err != nil {
		return classify(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrConflict
	}
	return nil
}
func nilIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func classifyApplication(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errors.Join(application.ErrConflict, err)
	}
	return err
}

var _ application.ExternalTransactionWriter = TransactionRepository{}

func (TransactionRepository) InsertOpening(ctx context.Context, tx pgx.Tx, t domain.WagerTransaction, resultingBalance domain.Money, walletVersion int64) error {
	if t.Type() != domain.TransactionOpening || t.Status() != domain.TransactionProcessed || t.ExternalID() != "" || t.ProviderID() != "" || t.Amount().MinorUnits() <= 0 || resultingBalance.Currency() != t.Amount().Currency() || walletVersion < 1 {
		return errors.New("invalid opening transaction")
	}
	_, err := tx.Exec(ctx, `INSERT INTO wager_transactions (id, origin, player_id, wallet_id, kind, amount_minor, currency, status, result_balance_minor, result_currency, result_wallet_version, created_at, updated_at) VALUES ($1,'INTERNAL',$2,$3,'OPENING',$4,$5,'PROCESSED',$6,$7,$8,$9,$9)`, t.ID(), t.PlayerID(), t.WalletID(), t.Amount().MinorUnits(), t.Amount().Currency(), resultingBalance.MinorUnits(), resultingBalance.Currency(), walletVersion, t.CreatedAt())
	return classify(err)
}
