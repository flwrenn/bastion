-- Migration 001: Create initial indexer schema
--
-- Tables:
--   user_operations — indexed UserOperationEvent data from EntryPoint v0.7.
--   indexer_state   — key-value store for indexer cursor / checkpoint.
--
-- user_operations fields come from three sources:
--   Event topics/data: user_op_hash, sender, paymaster, nonce, success,
--                      actual_gas_cost, actual_gas_used
--   Decoded calldata:  target, calldata
--   Transaction context: tx_hash, block_number, block_timestamp, log_index

CREATE TABLE user_operations (
    id               BIGSERIAL    PRIMARY KEY,
    user_op_hash     BYTEA        NOT NULL CHECK (octet_length(user_op_hash) = 32),
    sender           BYTEA        NOT NULL CHECK (octet_length(sender) = 20),
    paymaster        BYTEA        NOT NULL CHECK (octet_length(paymaster) = 20),
    target           BYTEA        CHECK (octet_length(target) = 20),
    calldata         BYTEA,
    nonce            NUMERIC(78,0) NOT NULL CHECK (nonce >= 0),
    success          BOOLEAN       NOT NULL,
    actual_gas_cost  NUMERIC(78,0) NOT NULL CHECK (actual_gas_cost >= 0),
    actual_gas_used  NUMERIC(78,0) NOT NULL CHECK (actual_gas_used >= 0),
    tx_hash          BYTEA        NOT NULL CHECK (octet_length(tx_hash) = 32),
    block_number     BIGINT       NOT NULL,
    block_timestamp  BIGINT       NOT NULL,
    log_index        INTEGER      NOT NULL,

    UNIQUE (tx_hash, log_index)
);

-- Query patterns: filter by sender, filter by block range, lookup by hash.
CREATE INDEX idx_user_operations_sender       ON user_operations (sender);
CREATE INDEX idx_user_operations_block_number ON user_operations (block_number);
CREATE INDEX idx_user_operations_user_op_hash ON user_operations (user_op_hash);
CREATE INDEX idx_user_operations_target       ON user_operations (target);

-- Tracks which block the indexer has processed up to, enabling
-- resumable backfill and gap detection after restarts.
CREATE TABLE indexer_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
