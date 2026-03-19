-- Migration 001: Create user_operations table
--
-- Stores indexed UserOperationEvent data from EntryPoint v0.7.
-- Fields come from two sources:
--   Event topics/data: user_op_hash, sender, paymaster, nonce, success,
--                      actual_gas_cost, actual_gas_used
--   Transaction context: tx_hash, block_number, block_timestamp, log_index

CREATE TABLE IF NOT EXISTS user_operations (
    id               BIGSERIAL    PRIMARY KEY,
    user_op_hash     BYTEA        NOT NULL,
    sender           BYTEA        NOT NULL,
    paymaster        BYTEA        NOT NULL,
    nonce            NUMERIC      NOT NULL,
    success          BOOLEAN      NOT NULL,
    actual_gas_cost  NUMERIC      NOT NULL,
    actual_gas_used  NUMERIC      NOT NULL,
    tx_hash          BYTEA        NOT NULL,
    block_number     BIGINT       NOT NULL,
    block_timestamp  BIGINT       NOT NULL,
    log_index        INTEGER      NOT NULL,

    UNIQUE (tx_hash, log_index)
);

-- Query patterns: filter by sender, filter by block range, lookup by hash.
CREATE INDEX IF NOT EXISTS idx_user_operations_sender       ON user_operations (sender);
CREATE INDEX IF NOT EXISTS idx_user_operations_block_number ON user_operations (block_number);
CREATE INDEX IF NOT EXISTS idx_user_operations_user_op_hash ON user_operations (user_op_hash);

-- Tracks which block the indexer has processed up to, enabling
-- resumable backfill and gap detection after restarts.
CREATE TABLE IF NOT EXISTS indexer_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
