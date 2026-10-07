-- Allow an operation currency to differ from the wallet currency while it is
-- being rejected. Financial ledger rows remain tied to the wallet currency.
DO $$
DECLARE c record;
BEGIN
    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'wager_transactions'::regclass
          AND contype = 'f'
          AND pg_get_constraintdef(oid) LIKE '%wallet_id%currency%'
    LOOP
        EXECUTE format('ALTER TABLE wager_transactions DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE wager_transactions
    ADD CONSTRAINT wager_transactions_wallet_id_fkey
    FOREIGN KEY (wallet_id) REFERENCES wallets (id);

DO $$
DECLARE c record;
BEGIN
    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'wager_transactions'::regclass
          AND contype = 'c'
    LOOP
        EXECUTE format('ALTER TABLE wager_transactions DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE wager_transactions
    ADD CONSTRAINT wager_tx_origin_check CHECK (origin IN ('INTERNAL', 'EXTERNAL')),
    ADD CONSTRAINT wager_tx_kind_check CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    ADD CONSTRAINT wager_tx_amount_nonnegative_check CHECK (amount_minor >= 0),
    ADD CONSTRAINT wager_tx_currency_check CHECK (currency IN ('BRL', 'USD')),
    ADD CONSTRAINT wager_tx_status_check CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    ADD CONSTRAINT wager_tx_result_balance_nonnegative_check CHECK (result_balance_minor IS NULL OR result_balance_minor >= 0),
    ADD CONSTRAINT wager_tx_result_currency_check CHECK (result_currency IS NULL OR result_currency IN ('BRL', 'USD')),
    ADD CONSTRAINT wager_tx_result_version_check CHECK (result_wallet_version IS NULL OR result_wallet_version >= 1),
    ADD CONSTRAINT wager_tx_attempts_nonnegative_check CHECK (attempt_count >= 0),
    ADD CONSTRAINT wager_tx_identity_hash_check CHECK (
        length(idempotency_key) > 0 OR idempotency_key IS NULL
    ),
    ADD CONSTRAINT wager_tx_payload_hash_check CHECK (
        payload_hash IS NULL OR payload_hash ~ '^[0-9a-f]{64}$'
    ),
    ADD CONSTRAINT wager_tx_origin_shape_check CHECK (
        (origin = 'INTERNAL'
            AND kind = 'OPENING' AND status = 'PROCESSED' AND amount_minor > 0
            AND external_id IS NULL AND provider_id IS NULL
            AND idempotency_key IS NULL AND payload_hash IS NULL
            AND game_id IS NULL AND round_id IS NULL
            AND reference_external_id IS NULL AND reference_transaction_id IS NULL
            AND failure_code IS NULL AND result_currency = currency
            AND result_balance_minor IS NOT NULL
            AND result_balance_minor = amount_minor
            AND result_wallet_version IS NOT NULL)
        OR
        (origin = 'EXTERNAL' AND kind <> 'OPENING'
            AND provider_id IS NOT NULL AND btrim(provider_id) <> ''
            AND external_id IS NOT NULL AND btrim(external_id) <> ''
            AND idempotency_key IS NOT NULL AND btrim(idempotency_key) <> ''
            AND payload_hash IS NOT NULL AND payload_hash ~ '^[0-9a-f]{64}$'
            AND game_id IS NOT NULL AND btrim(game_id) <> ''
            AND round_id IS NOT NULL AND btrim(round_id) <> '')
    ),
    ADD CONSTRAINT wager_tx_amount_check CHECK (
        (kind = 'LOSS' AND amount_minor = 0) OR
        (kind <> 'LOSS' AND amount_minor > 0)
    ),
    ADD CONSTRAINT wager_tx_reference_external_check CHECK (
        (kind IN ('REFUND', 'ROLLBACK')
            AND reference_external_id IS NOT NULL
            AND btrim(reference_external_id) <> '')
        OR (kind = 'WIN'
            AND (reference_external_id IS NULL OR btrim(reference_external_id) <> ''))
        OR (kind IN ('BET', 'LOSS', 'OPENING') AND reference_external_id IS NULL)
    ),
    ADD CONSTRAINT wager_tx_pending_attempt_check CHECK (
        (status = 'PENDING_REFERENCE') = (next_attempt_at IS NOT NULL)
    ),
    ADD CONSTRAINT wager_tx_attempt_count_check CHECK (
        status = 'PENDING_REFERENCE' OR attempt_count = 0 OR next_attempt_at IS NOT NULL
    ),
    ADD CONSTRAINT wager_tx_failure_check CHECK (
        (status IN ('REJECTED', 'FAILED')
            AND failure_code IS NOT NULL
            AND failure_code ~ '^[A-Z][A-Z0-9_]*$')
        OR (status NOT IN ('REJECTED', 'FAILED') AND failure_code IS NULL)
    ),
    ADD CONSTRAINT wager_tx_result_check CHECK (
        (status = 'PROCESSED'
            AND result_balance_minor IS NOT NULL
            AND result_currency IS NOT NULL
            AND result_currency = currency
            AND result_wallet_version IS NOT NULL)
        OR (status = 'REJECTED'
            AND result_balance_minor IS NOT NULL
            AND result_currency IS NOT NULL
            AND result_wallet_version IS NOT NULL)
        OR (status IN ('PENDING', 'PENDING_REFERENCE', 'FAILED')
            AND result_balance_minor IS NULL
            AND result_currency IS NULL
            AND result_wallet_version IS NULL)
    ),
    ADD CONSTRAINT wager_tx_reference_state_check CHECK (
        status = 'PENDING_REFERENCE'
        OR reference_transaction_id IS NOT NULL
        OR kind NOT IN ('REFUND', 'ROLLBACK')
    ),
    ADD CONSTRAINT wager_tx_pending_reference_shape_check CHECK (
        status <> 'PENDING_REFERENCE'
        OR (origin = 'EXTERNAL'
            AND kind IN ('REFUND', 'ROLLBACK')
            AND reference_transaction_id IS NULL
            AND next_attempt_at IS NOT NULL)
    );

DO $$
DECLARE c record;
BEGIN
    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'wallet_ledger_entries'::regclass
          AND contype = 'f'
          AND pg_get_constraintdef(oid) LIKE '%transaction_id%'
    LOOP
        EXECUTE format('ALTER TABLE wallet_ledger_entries DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE wallet_ledger_entries
    ADD CONSTRAINT wallet_ledger_entries_transaction_wallet_currency_fkey
    FOREIGN KEY (transaction_id, wallet_id, currency)
    REFERENCES wager_transactions (id, wallet_id, currency);
