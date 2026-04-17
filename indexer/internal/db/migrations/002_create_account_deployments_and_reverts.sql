-- Migration 002: Index AccountDeployed and UserOperationRevertReason events
--
-- Both tables mirror the reorg-safe pattern used by user_operations:
-- primary reorg key is (tx_hash, log_index), with block_number indexed for
-- range deletes. No foreign key to user_operations — AccountDeployed emits
-- before UserOperationEvent in the same tx, and the batch insert flow does
-- not guarantee insert order between the two tables.
--
-- user_op_hash carries a UNIQUE constraint on each table. EntryPoint v0.7
-- emits AccountDeployed and UserOperationRevertReason at most once per UserOp
-- (by protocol), so enforcing this at the schema level is a real invariant
-- rather than a defensive extra. It also lets the API LEFT JOIN enrichment
-- queries stay straightforward: no DISTINCT ON, no LATERAL LIMIT 1, no risk
-- of row multiplication inflating count(*) OVER() or breaking pagination.
-- A unique index also replaces the plain user_op_hash lookup index.

CREATE TABLE account_deployments (
    id               BIGSERIAL    PRIMARY KEY,
    user_op_hash     BYTEA        NOT NULL CHECK (octet_length(user_op_hash) = 32),
    sender           BYTEA        NOT NULL CHECK (octet_length(sender) = 20),
    factory          BYTEA        NOT NULL CHECK (octet_length(factory) = 20),
    paymaster        BYTEA        NOT NULL CHECK (octet_length(paymaster) = 20),
    tx_hash          BYTEA        NOT NULL CHECK (octet_length(tx_hash) = 32),
    block_number     BIGINT       NOT NULL,
    block_timestamp  BIGINT       NOT NULL,
    log_index        INTEGER      NOT NULL,

    UNIQUE (tx_hash, log_index),
    UNIQUE (user_op_hash)
);

CREATE INDEX idx_account_deployments_block_number ON account_deployments (block_number);

CREATE TABLE user_operation_reverts (
    id               BIGSERIAL    PRIMARY KEY,
    user_op_hash     BYTEA        NOT NULL CHECK (octet_length(user_op_hash) = 32),
    sender           BYTEA        NOT NULL CHECK (octet_length(sender) = 20),
    nonce            NUMERIC(78,0) NOT NULL CHECK (nonce >= 0),
    revert_reason    BYTEA        NOT NULL,
    tx_hash          BYTEA        NOT NULL CHECK (octet_length(tx_hash) = 32),
    block_number     BIGINT       NOT NULL,
    block_timestamp  BIGINT       NOT NULL,
    log_index        INTEGER      NOT NULL,

    UNIQUE (tx_hash, log_index),
    UNIQUE (user_op_hash)
);

CREATE INDEX idx_user_operation_reverts_block_number ON user_operation_reverts (block_number);
