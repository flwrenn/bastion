package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ListParams controls pagination and filtering for ListOperations.
type ListParams struct {
	Sender []byte // nil = no filter
	Limit  int
	Offset int
}

func clampListParams(p *ListParams) {
	if p.Limit <= 0 {
		p.Limit = 20
	}
	if p.Limit > 100 {
		p.Limit = 100
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Offset > 10000 {
		p.Offset = 10000
	}
}

// ListOperations returns a page of user operations ordered newest-first,
// along with the total count matching the filter.
func ListOperations(ctx context.Context, pool *pgxpool.Pool, p ListParams) ([]UserOperation, int64, error) {
	if pool == nil {
		return nil, 0, errors.New("pool is required")
	}
	clampListParams(&p)

	var total int64
	var rows pgx.Rows
	var err error

	if p.Sender != nil {
		err = pool.QueryRow(ctx,
			"SELECT count(*) FROM user_operations WHERE sender = $1",
			p.Sender,
		).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("count operations: %w", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, user_op_hash, sender, paymaster, target, calldata,
			       nonce, success, actual_gas_cost, actual_gas_used,
			       tx_hash, block_number, block_timestamp, log_index
			FROM user_operations
			WHERE sender = $1
			ORDER BY block_number DESC, log_index DESC
			LIMIT $2 OFFSET $3`,
			p.Sender, p.Limit, p.Offset,
		)
	} else {
		err = pool.QueryRow(ctx,
			"SELECT count(*) FROM user_operations",
		).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("count operations: %w", err)
		}

		rows, err = pool.Query(ctx, `
			SELECT id, user_op_hash, sender, paymaster, target, calldata,
			       nonce, success, actual_gas_cost, actual_gas_used,
			       tx_hash, block_number, block_timestamp, log_index
			FROM user_operations
			ORDER BY block_number DESC, log_index DESC
			LIMIT $1 OFFSET $2`,
			p.Limit, p.Offset,
		)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("query operations: %w", err)
	}
	defer rows.Close()

	var ops []UserOperation
	for rows.Next() {
		var op UserOperation
		if err := rows.Scan(
			&op.ID,
			&op.UserOpHash,
			&op.Sender,
			&op.Paymaster,
			&op.Target,
			&op.Calldata,
			&op.Nonce,
			&op.Success,
			&op.ActualGasCost,
			&op.ActualGasUsed,
			&op.TxHash,
			&op.BlockNumber,
			&op.BlockTimestamp,
			&op.LogIndex,
		); err != nil {
			return nil, 0, fmt.Errorf("scan operation: %w", err)
		}
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate operations: %w", err)
	}

	return ops, total, nil
}

// GetOperationByHash returns a single user operation by its userOpHash.
// Returns nil, nil when no matching row exists.
func GetOperationByHash(ctx context.Context, pool *pgxpool.Pool, hash []byte) (*UserOperation, error) {
	if pool == nil {
		return nil, errors.New("pool is required")
	}
	var op UserOperation
	err := pool.QueryRow(ctx, `
		SELECT id, user_op_hash, sender, paymaster, target, calldata,
		       nonce, success, actual_gas_cost, actual_gas_used,
		       tx_hash, block_number, block_timestamp, log_index
		FROM user_operations
		WHERE user_op_hash = $1`,
		hash,
	).Scan(
		&op.ID,
		&op.UserOpHash,
		&op.Sender,
		&op.Paymaster,
		&op.Target,
		&op.Calldata,
		&op.Nonce,
		&op.Success,
		&op.ActualGasCost,
		&op.ActualGasUsed,
		&op.TxHash,
		&op.BlockNumber,
		&op.BlockTimestamp,
		&op.LogIndex,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query operation by hash: %w", err)
	}
	return &op, nil
}

// Stats holds aggregate statistics for indexed user operations.
type Stats struct {
	TotalOps      int64
	SuccessCount  int64
	UniqueSenders int64
}

// GetStats returns aggregate statistics across all indexed operations.
func GetStats(ctx context.Context, pool *pgxpool.Pool) (Stats, error) {
	if pool == nil {
		return Stats{}, errors.New("pool is required")
	}
	var s Stats
	err := pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE success),
		       count(DISTINCT sender)
		FROM user_operations`,
	).Scan(&s.TotalOps, &s.SuccessCount, &s.UniqueSenders)
	if err != nil {
		return Stats{}, fmt.Errorf("query stats: %w", err)
	}
	return s, nil
}
