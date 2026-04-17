package db

import (
	"context"
	"fmt"
	"strconv"

	"github.com/flwrenn/bastion/indexer/internal/numconv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const upsertUserOperationSQL = `
	INSERT INTO user_operations (
		user_op_hash,
		sender,
		paymaster,
		target,
		calldata,
		nonce,
		success,
		actual_gas_cost,
		actual_gas_used,
		tx_hash,
		block_number,
		block_timestamp,
		log_index
	)
	VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
	)
	ON CONFLICT (tx_hash, log_index) DO UPDATE SET
		user_op_hash = EXCLUDED.user_op_hash,
		sender = EXCLUDED.sender,
		paymaster = EXCLUDED.paymaster,
		target = EXCLUDED.target,
		calldata = EXCLUDED.calldata,
		nonce = EXCLUDED.nonce,
		success = EXCLUDED.success,
		actual_gas_cost = EXCLUDED.actual_gas_cost,
		actual_gas_used = EXCLUDED.actual_gas_used,
		block_number = EXCLUDED.block_number,
		block_timestamp = EXCLUDED.block_timestamp
`

const upsertAccountDeploymentSQL = `
	INSERT INTO account_deployments (
		user_op_hash,
		sender,
		factory,
		paymaster,
		tx_hash,
		block_number,
		block_timestamp,
		log_index
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (tx_hash, log_index) DO UPDATE SET
		user_op_hash = EXCLUDED.user_op_hash,
		sender = EXCLUDED.sender,
		factory = EXCLUDED.factory,
		paymaster = EXCLUDED.paymaster,
		block_number = EXCLUDED.block_number,
		block_timestamp = EXCLUDED.block_timestamp
`

const upsertUserOperationRevertSQL = `
	INSERT INTO user_operation_reverts (
		user_op_hash,
		sender,
		nonce,
		revert_reason,
		tx_hash,
		block_number,
		block_timestamp,
		log_index
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (tx_hash, log_index) DO UPDATE SET
		user_op_hash = EXCLUDED.user_op_hash,
		sender = EXCLUDED.sender,
		nonce = EXCLUDED.nonce,
		revert_reason = EXCLUDED.revert_reason,
		block_number = EXCLUDED.block_number,
		block_timestamp = EXCLUDED.block_timestamp
`

// ReplaceEventsAndSetCursor atomically reindexes the given block range across
// the three event tables (user_operations, account_deployments,
// user_operation_reverts) and advances the cursor. Existing rows in the range
// are deleted before the new rows are inserted, which gives us reorg-safety
// without requiring any per-table ordering or foreign keys.
func ReplaceEventsAndSetCursor(
	ctx context.Context,
	pool *pgxpool.Pool,
	stateKey string,
	fromBlock uint64,
	toBlock uint64,
	cursor uint64,
	operations []UserOperation,
	deployments []AccountDeployment,
	reverts []UserOperationRevert,
) error {
	if stateKey == "" {
		return fmt.Errorf("state key is required")
	}
	if pool == nil {
		return fmt.Errorf("pool is required")
	}
	if fromBlock > toBlock {
		return fmt.Errorf("invalid block range: from %d > to %d", fromBlock, toBlock)
	}

	from, err := toInt64(fromBlock)
	if err != nil {
		return fmt.Errorf("convert from block: %w", err)
	}
	to, err := toInt64(toBlock)
	if err != nil {
		return fmt.Errorf("convert to block: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete existing rows in the range across all three tables.
	for _, table := range []string{"user_operations", "account_deployments", "user_operation_reverts"} {
		if _, err := tx.Exec(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE block_number BETWEEN $1 AND $2", table),
			from,
			to,
		); err != nil {
			return fmt.Errorf("delete %s in range [%d,%d]: %w", table, fromBlock, toBlock, err)
		}
	}

	// Batch-upsert each table.
	if len(operations) > 0 {
		batch := &pgx.Batch{}
		for i := range operations {
			op := operations[i]
			batch.Queue(
				upsertUserOperationSQL,
				op.UserOpHash,
				op.Sender,
				op.Paymaster,
				op.Target,
				op.Calldata,
				op.Nonce,
				op.Success,
				op.ActualGasCost,
				op.ActualGasUsed,
				op.TxHash,
				op.BlockNumber,
				op.BlockTimestamp,
				op.LogIndex,
			)
		}
		results := tx.SendBatch(ctx, batch)
		for i := range operations {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert user_operation at index %d: %w", i, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close user_operations batch: %w", err)
		}
	}

	if len(deployments) > 0 {
		batch := &pgx.Batch{}
		for i := range deployments {
			d := deployments[i]
			batch.Queue(
				upsertAccountDeploymentSQL,
				d.UserOpHash,
				d.Sender,
				d.Factory,
				d.Paymaster,
				d.TxHash,
				d.BlockNumber,
				d.BlockTimestamp,
				d.LogIndex,
			)
		}
		results := tx.SendBatch(ctx, batch)
		for i := range deployments {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert account_deployment at index %d: %w", i, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close account_deployments batch: %w", err)
		}
	}

	if len(reverts) > 0 {
		batch := &pgx.Batch{}
		for i := range reverts {
			r := reverts[i]
			batch.Queue(
				upsertUserOperationRevertSQL,
				r.UserOpHash,
				r.Sender,
				r.Nonce,
				r.RevertReason,
				r.TxHash,
				r.BlockNumber,
				r.BlockTimestamp,
				r.LogIndex,
			)
		}
		results := tx.SendBatch(ctx, batch)
		for i := range reverts {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("insert user_operation_revert at index %d: %w", i, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close user_operation_reverts batch: %w", err)
		}
	}

	if err := setStateTx(ctx, tx, stateKey, strconv.FormatUint(cursor, 10)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func toInt64(v uint64) (int64, error) {
	return numconv.Uint64ToInt64(v)
}
