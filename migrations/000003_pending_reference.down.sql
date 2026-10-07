DO $$
DECLARE bad_count bigint;
BEGIN
    SELECT count(*) INTO bad_count
    FROM wager_transactions
    WHERE kind IN ('REFUND', 'ROLLBACK')
      AND (
        (status IN ('PENDING', 'PENDING_REFERENCE') AND reference_transaction_id IS NULL)
        OR (status = 'REJECTED' AND failure_code = 'REFERENCE_NOT_FOUND' AND reference_transaction_id IS NULL)
      );
    IF bad_count > 0 THEN
        RAISE EXCEPTION 'cannot downgrade pending reference rows' USING ERRCODE = '55000';
    END IF;
END $$;

ALTER TABLE wager_transactions
    DROP CONSTRAINT wager_tx_pending_attempt_check,
    DROP CONSTRAINT wager_tx_reference_state_check,
    DROP CONSTRAINT wager_tx_pending_reference_shape_check,
    DROP CONSTRAINT wager_tx_external_reference_self_check,
    DROP CONSTRAINT wager_tx_internal_reference_self_check;

ALTER TABLE wager_transactions
    ADD CONSTRAINT wager_tx_pending_attempt_check CHECK (
        (status = 'PENDING_REFERENCE') = (next_attempt_at IS NOT NULL)
    ),
    ADD CONSTRAINT wager_tx_attempt_count_check CHECK (
        status = 'PENDING_REFERENCE' OR attempt_count = 0 OR next_attempt_at IS NOT NULL
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

CREATE OR REPLACE FUNCTION validate_wager_transaction_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.origin IS DISTINCT FROM OLD.origin
       OR NEW.external_id IS DISTINCT FROM OLD.external_id OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key OR NEW.payload_hash IS DISTINCT FROM OLD.payload_hash
       OR NEW.player_id IS DISTINCT FROM OLD.player_id OR NEW.wallet_id IS DISTINCT FROM OLD.wallet_id
       OR NEW.game_id IS DISTINCT FROM OLD.game_id OR NEW.round_id IS DISTINCT FROM OLD.round_id
       OR NEW.kind IS DISTINCT FROM OLD.kind OR NEW.amount_minor IS DISTINCT FROM OLD.amount_minor
       OR NEW.currency IS DISTINCT FROM OLD.currency OR NEW.reference_external_id IS DISTINCT FROM OLD.reference_external_id THEN
        RAISE EXCEPTION 'wager transaction identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.reference_transaction_id IS DISTINCT FROM OLD.reference_transaction_id THEN
        IF OLD.status <> 'PENDING_REFERENCE' OR OLD.reference_transaction_id IS NOT NULL
           OR NEW.reference_transaction_id IS NULL OR btrim(NEW.reference_transaction_id) = '' THEN
            RAISE EXCEPTION 'wager transaction reference is immutable' USING ERRCODE = '55000';
        END IF;
    END IF;
    IF OLD.status IN ('PROCESSED', 'REJECTED', 'FAILED') THEN
        RAISE EXCEPTION 'terminal wager transaction is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'PENDING' AND NEW.status NOT IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED') THEN RAISE EXCEPTION 'invalid pending transition' USING ERRCODE = '55000'; END IF;
    IF OLD.status = 'PENDING_REFERENCE' AND NEW.status NOT IN ('PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED') THEN RAISE EXCEPTION 'invalid pending reference transition' USING ERRCODE = '55000'; END IF;
    IF OLD.status NOT IN ('PENDING', 'PENDING_REFERENCE') THEN RAISE EXCEPTION 'invalid wager transition' USING ERRCODE = '55000'; END IF;
    RETURN NEW;
END;
$$;
