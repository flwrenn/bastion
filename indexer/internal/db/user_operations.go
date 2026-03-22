package db

import (
	"context"
	"fmt"
	"math"
	"strconv"

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

func ReplaceOperationsAndSetCursor(
	ctx context.Context,
	pool *pgxpool.Pool,
	stateKey string,
	fromBlock uint64,
	toBlock uint64,
	cursor uint64,
	operations []UserOperation,
) error {
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

	if _, err := tx.Exec(ctx,
		"DELETE FROM user_operations WHERE block_number BETWEEN $1 AND $2",
		from,
		to,
	); err != nil {
		return fmt.Errorf("delete operations in range [%d,%d]: %w", fromBlock, toBlock, err)
	}

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
				return fmt.Errorf("insert operation at index %d: %w", i, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("close operation batch: %w", err)
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
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("value %d overflows int64", v)
	}
	return int64(v), nil
}
