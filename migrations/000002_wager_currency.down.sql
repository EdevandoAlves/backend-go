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
    ADD CONSTRAINT wallet_ledger_entries_transaction_id_wallet_id_currency_fkey
    FOREIGN KEY (transaction_id, wallet_id, currency)
    REFERENCES wager_transactions (id, wallet_id, currency);

DO $$
DECLARE c record;
BEGIN
    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'wager_transactions'::regclass
          AND contype = 'f'
          AND pg_get_constraintdef(oid) LIKE '%wallet_id%'
    LOOP
        EXECUTE format('ALTER TABLE wager_transactions DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE wager_transactions
    ADD CONSTRAINT wager_transactions_wallet_id_currency_fkey
    FOREIGN KEY (wallet_id, currency) REFERENCES wallets (id, currency);

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
    ADD CONSTRAINT wager_tx_legacy_origin_check CHECK (origin IN ('INTERNAL', 'EXTERNAL')),
    ADD CONSTRAINT wager_tx_legacy_kind_check CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    ADD CONSTRAINT wager_tx_legacy_amount_nonnegative_check CHECK (amount_minor >= 0),
    ADD CONSTRAINT wager_tx_legacy_currency_check CHECK (currency IN ('BRL', 'USD')),
    ADD CONSTRAINT wager_tx_legacy_status_check CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    ADD CONSTRAINT wager_tx_legacy_result_balance_nonnegative_check CHECK (result_balance_minor IS NULL OR result_balance_minor >= 0),
    ADD CONSTRAINT wager_tx_legacy_result_currency_check CHECK (result_currency IS NULL OR result_currency IN ('BRL', 'USD')),
    ADD CONSTRAINT wager_tx_legacy_result_version_check CHECK (result_wallet_version IS NULL OR result_wallet_version >= 1),
    ADD CONSTRAINT wager_tx_legacy_attempts_nonnegative_check CHECK (attempt_count >= 0),
    ADD CONSTRAINT wager_tx_legacy_identity_hash_check CHECK (length(idempotency_key) > 0 OR idempotency_key IS NULL),
    ADD CONSTRAINT wager_tx_legacy_payload_hash_check CHECK (payload_hash IS NULL OR payload_hash ~ '^[0-9a-f]{64}$'),
    ADD CONSTRAINT wager_tx_legacy_origin_shape_check CHECK (
        (origin = 'INTERNAL' AND kind = 'OPENING' AND status = 'PROCESSED' AND amount_minor > 0 AND external_id IS NULL AND provider_id IS NULL AND idempotency_key IS NULL AND payload_hash IS NULL AND game_id IS NULL AND round_id IS NULL AND reference_external_id IS NULL AND reference_transaction_id IS NULL AND failure_code IS NULL AND result_currency = currency AND result_balance_minor IS NOT NULL AND result_balance_minor = amount_minor AND result_wallet_version IS NOT NULL)
        OR (origin = 'EXTERNAL' AND kind <> 'OPENING' AND provider_id IS NOT NULL AND btrim(provider_id) <> '' AND external_id IS NOT NULL AND btrim(external_id) <> '' AND idempotency_key IS NOT NULL AND btrim(idempotency_key) <> '' AND payload_hash IS NOT NULL AND payload_hash ~ '^[0-9a-f]{64}$' AND game_id IS NOT NULL AND btrim(game_id) <> '' AND round_id IS NOT NULL AND btrim(round_id) <> '')
    ),
    ADD CONSTRAINT wager_tx_legacy_amount_check CHECK ((kind = 'LOSS' AND amount_minor = 0) OR (kind <> 'LOSS' AND amount_minor > 0)),
    ADD CONSTRAINT wager_tx_legacy_result_check CHECK ((status IN ('PROCESSED', 'REJECTED') AND result_balance_minor IS NOT NULL AND result_currency = currency AND result_wallet_version IS NOT NULL) OR (status IN ('PENDING', 'PENDING_REFERENCE', 'FAILED') AND result_balance_minor IS NULL AND result_currency IS NULL AND result_wallet_version IS NULL));
