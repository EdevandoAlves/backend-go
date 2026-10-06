DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS inbox_messages;
DROP TABLE IF EXISTS wallet_ledger_entries;
DROP TABLE IF EXISTS wager_transactions;
DROP TABLE IF EXISTS wallets;
DROP FUNCTION IF EXISTS protect_outbox_snapshot();
DROP FUNCTION IF EXISTS reject_ledger_mutation();
DROP FUNCTION IF EXISTS validate_wager_transaction_update();
