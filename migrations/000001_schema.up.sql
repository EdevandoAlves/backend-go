-- Fase 4: persistent identity, financial records, inbox and transactional outbox.

CREATE TABLE wallets (
    id TEXT PRIMARY KEY CHECK (btrim(id) <> ''),
    player_id TEXT NOT NULL CHECK (btrim(player_id) <> ''),
    currency TEXT NOT NULL CHECK (currency IN ('BRL', 'USD')),
    balance_minor BIGINT NOT NULL CHECK (balance_minor >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (updated_at >= created_at),
    UNIQUE (player_id, currency),
    UNIQUE (id, currency)
);

CREATE TABLE wager_transactions (
    id TEXT PRIMARY KEY CHECK (btrim(id) <> ''),
    origin TEXT NOT NULL CHECK (origin IN ('INTERNAL', 'EXTERNAL')),
    external_id TEXT,
    provider_id TEXT,
    idempotency_key TEXT,
    payload_hash TEXT,
    player_id TEXT NOT NULL CHECK (btrim(player_id) <> ''),
    wallet_id TEXT NOT NULL,
    game_id TEXT,
    round_id TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    currency TEXT NOT NULL CHECK (currency IN ('BRL', 'USD')),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    failure_code TEXT,
    result_code TEXT,
    result_balance_minor BIGINT CHECK (result_balance_minor >= 0),
    result_currency TEXT CHECK (result_currency IN ('BRL', 'USD')),
    result_wallet_version BIGINT CHECK (result_wallet_version >= 1),
    reference_external_id TEXT,
    reference_transaction_id TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (wallet_id, currency) REFERENCES wallets (id, currency),
    CHECK (length(idempotency_key) > 0 OR idempotency_key IS NULL),
    CHECK (payload_hash IS NULL OR payload_hash ~ '^[0-9a-f]{64}$'),
    CHECK (
        (origin = 'INTERNAL'
            AND kind = 'OPENING' AND status = 'PROCESSED' AND amount_minor > 0
            AND external_id IS NULL AND provider_id IS NULL AND idempotency_key IS NULL
            AND payload_hash IS NULL AND game_id IS NULL AND round_id IS NULL
            AND reference_external_id IS NULL AND reference_transaction_id IS NULL
            AND failure_code IS NULL AND result_currency = currency
            AND result_balance_minor = amount_minor AND result_wallet_version IS NOT NULL)
        OR
        (origin = 'EXTERNAL' AND kind <> 'OPENING'
            AND btrim(provider_id) <> '' AND btrim(external_id) <> ''
            AND btrim(idempotency_key) <> '' AND payload_hash ~ '^[0-9a-f]{64}$'
            AND btrim(game_id) <> '' AND btrim(round_id) <> '')
    ),
    CHECK ((kind = 'LOSS' AND amount_minor = 0) OR (kind <> 'LOSS' AND amount_minor > 0)),
    CHECK ((kind IN ('REFUND', 'ROLLBACK') AND reference_external_id IS NOT NULL AND btrim(reference_external_id) <> '')
        OR (kind NOT IN ('REFUND', 'ROLLBACK') AND reference_external_id IS NULL)),
    CHECK ((status = 'PENDING_REFERENCE') = (next_attempt_at IS NOT NULL)),
    CHECK (status = 'PENDING_REFERENCE' OR attempt_count = 0 OR next_attempt_at IS NOT NULL),
    CHECK ((status IN ('REJECTED', 'FAILED') AND failure_code IS NOT NULL AND failure_code ~ '^[A-Z][A-Z0-9_]*$')
        OR (status NOT IN ('REJECTED', 'FAILED') AND failure_code IS NULL)),
    CHECK ((status IN ('PROCESSED', 'REJECTED') AND result_balance_minor IS NOT NULL AND result_currency = currency AND result_wallet_version IS NOT NULL)
        OR (status IN ('PENDING', 'PENDING_REFERENCE', 'FAILED') AND result_balance_minor IS NULL AND result_currency IS NULL AND result_wallet_version IS NULL)),
    CHECK (status = 'PENDING_REFERENCE' OR reference_transaction_id IS NOT NULL OR kind NOT IN ('REFUND', 'ROLLBACK')),
    CHECK (status <> 'PENDING_REFERENCE' OR (origin = 'EXTERNAL' AND kind IN ('REFUND', 'ROLLBACK') AND reference_transaction_id IS NULL AND next_attempt_at IS NOT NULL)),
    UNIQUE (id, wallet_id, currency),
    FOREIGN KEY (reference_transaction_id) REFERENCES wager_transactions (id)
);

CREATE UNIQUE INDEX wager_transactions_provider_key_uq
    ON wager_transactions (provider_id, idempotency_key)
    WHERE origin = 'EXTERNAL';
CREATE UNIQUE INDEX wager_transactions_provider_external_uq
    ON wager_transactions (provider_id, external_id)
    WHERE origin = 'EXTERNAL';
CREATE UNIQUE INDEX wager_transactions_opening_uq
    ON wager_transactions (wallet_id)
    WHERE origin = 'INTERNAL' AND kind = 'OPENING';
CREATE UNIQUE INDEX wager_transactions_reversal_uq
    ON wager_transactions (reference_transaction_id)
    WHERE kind IN ('REFUND', 'ROLLBACK') AND status = 'PROCESSED';
CREATE INDEX wager_transactions_wallet_idx ON wager_transactions (wallet_id, created_at);
CREATE INDEX wager_transactions_pending_reference_idx
    ON wager_transactions (next_attempt_at, created_at)
    WHERE status = 'PENDING_REFERENCE';

CREATE FUNCTION validate_wager_transaction_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.origin IS DISTINCT FROM OLD.origin
       OR NEW.external_id IS DISTINCT FROM OLD.external_id
       OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.payload_hash IS DISTINCT FROM OLD.payload_hash
       OR NEW.player_id IS DISTINCT FROM OLD.player_id
       OR NEW.wallet_id IS DISTINCT FROM OLD.wallet_id
       OR NEW.game_id IS DISTINCT FROM OLD.game_id
       OR NEW.round_id IS DISTINCT FROM OLD.round_id
       OR NEW.kind IS DISTINCT FROM OLD.kind
       OR NEW.amount_minor IS DISTINCT FROM OLD.amount_minor
       OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.reference_external_id IS DISTINCT FROM OLD.reference_external_id
    THEN
        RAISE EXCEPTION 'wager transaction identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.reference_transaction_id IS DISTINCT FROM OLD.reference_transaction_id THEN
        IF OLD.status <> 'PENDING_REFERENCE'
           OR NEW.status NOT IN ('PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')
           OR OLD.reference_transaction_id IS NOT NULL
           OR NEW.reference_transaction_id IS NULL
           OR btrim(NEW.reference_transaction_id) = ''
        THEN
            RAISE EXCEPTION 'wager transaction reference is immutable' USING ERRCODE = '55000';
        END IF;
    END IF;
    IF OLD.status IN ('PROCESSED', 'REJECTED', 'FAILED') THEN
        RAISE EXCEPTION 'terminal wager transaction is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'PENDING' AND NEW.status NOT IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED') THEN
        RAISE EXCEPTION 'invalid pending transition' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'PENDING_REFERENCE' AND NEW.status NOT IN ('PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED') THEN
        RAISE EXCEPTION 'invalid pending reference transition' USING ERRCODE = '55000';
    END IF;
    IF OLD.status NOT IN ('PENDING', 'PENDING_REFERENCE') THEN
        RAISE EXCEPTION 'invalid wager transition' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER wager_transaction_state_guard
    BEFORE UPDATE ON wager_transactions
    FOR EACH ROW EXECUTE FUNCTION validate_wager_transaction_update();

CREATE TABLE wallet_ledger_entries (
    id TEXT PRIMARY KEY CHECK (btrim(id) <> ''),
    wallet_id TEXT NOT NULL,
    transaction_id TEXT NOT NULL,
    currency TEXT NOT NULL CHECK (currency IN ('BRL', 'USD')),
    direction TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    before_minor BIGINT NOT NULL CHECK (before_minor >= 0),
    after_minor BIGINT NOT NULL CHECK (after_minor >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (wallet_id, currency) REFERENCES wallets (id, currency),
    FOREIGN KEY (transaction_id, wallet_id, currency)
        REFERENCES wager_transactions (id, wallet_id, currency),
    UNIQUE (transaction_id),
    CHECK (
        (direction = 'CREDIT' AND after_minor::NUMERIC = before_minor::NUMERIC + amount_minor::NUMERIC)
        OR (direction = 'DEBIT' AND before_minor >= amount_minor
            AND after_minor::NUMERIC = before_minor::NUMERIC - amount_minor::NUMERIC)
    )
);

CREATE FUNCTION reject_ledger_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'wallet ledger is append-only' USING ERRCODE = '55000';
END;
$$;
CREATE TRIGGER wallet_ledger_append_only
    BEFORE UPDATE OR DELETE ON wallet_ledger_entries
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE inbox_messages (
    consumer_name TEXT NOT NULL CHECK (btrim(consumer_name) <> ''),
    message_id TEXT NOT NULL CHECK (btrim(message_id) <> ''),
    provider_id TEXT,
    payload_hash TEXT NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    transaction_id TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (consumer_name, message_id),
    FOREIGN KEY (transaction_id) REFERENCES wager_transactions (id),
    CHECK ((completed_at IS NULL AND transaction_id IS NULL)
        OR (completed_at IS NOT NULL AND transaction_id IS NOT NULL))
);

CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY CHECK (btrim(id) <> ''),
    transaction_id TEXT NOT NULL REFERENCES wager_transactions (id),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'WagerTransactionProcessed', 'WagerTransactionRejected',
        'WalletBalanceChanged', 'WagerTransactionPendingReference')),
    event_version BIGINT NOT NULL CHECK (event_version >= 1),
    aggregate_id TEXT NOT NULL CHECK (btrim(aggregate_id) <> ''),
    correlation_id TEXT NOT NULL CHECK (btrim(correlation_id) <> ''),
    causation_id TEXT,
    payload JSONB NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    last_error TEXT,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transaction_id, event_type),
    CHECK (claimed_by IS NULL OR claim_expires_at IS NOT NULL)
);
CREATE INDEX outbox_events_pending_idx ON outbox_events (next_attempt_at, created_at)
    WHERE published_at IS NULL;

CREATE FUNCTION protect_outbox_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.transaction_id IS DISTINCT FROM OLD.transaction_id
       OR NEW.event_type IS DISTINCT FROM OLD.event_type
       OR NEW.event_version IS DISTINCT FROM OLD.event_version
       OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
       OR NEW.correlation_id IS DISTINCT FROM OLD.correlation_id
       OR NEW.causation_id IS DISTINCT FROM OLD.causation_id
       OR NEW.payload IS DISTINCT FROM OLD.payload
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'outbox event snapshot is immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER outbox_snapshot_guard
    BEFORE UPDATE ON outbox_events
    FOR EACH ROW EXECUTE FUNCTION protect_outbox_snapshot();
