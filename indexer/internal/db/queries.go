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

// ClampListParams normalises pagination values to safe defaults.
// It is exported so the API handler can clamp before calling ListOperations
// and echo the effective values in the response.
func ClampListParams(p *ListParams) {
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

// ListOperations returns a page of user operations enriched with the
// denormalized accountDeployed / revertReason fields, ordered newest-first.
func ListOperations(ctx context.Context, pool *pgxpool.Pool, p ListParams) ([]UserOperation, int64, error) {
	if pool == nil {
		return nil, 0, errors.New("pool is required")
	}
	ClampListParams(&p)

	const selectCols = `
		uo.id, uo.user_op_hash, uo.sender, uo.paymaster, uo.target, uo.calldata,
		uo.nonce, uo.success, uo.actual_gas_cost, uo.actual_gas_used,
		uo.tx_hash, uo.block_number, uo.block_timestamp, uo.log_index,
		(ad.user_op_hash IS NOT NULL) AS account_deployed,
		ur.revert_reason,
		count(*) OVER() AS total
	`
	const joinClause = `
		FROM user_operations uo
		LEFT JOIN account_deployments      ad ON ad.user_op_hash = uo.user_op_hash
		LEFT JOIN user_operation_reverts   ur ON ur.user_op_hash = uo.user_op_hash
	`

	var rows pgx.Rows
	var err error

	if p.Sender != nil {
		rows, err = pool.Query(ctx,
			`SELECT `+selectCols+joinClause+`
			 WHERE uo.sender = $1
			 ORDER BY uo.block_number DESC, uo.log_index DESC
			 LIMIT $2 OFFSET $3`,
			p.Sender, p.Limit, p.Offset,
		)
	} else {
		rows, err = pool.Query(ctx,
			`SELECT `+selectCols+joinClause+`
			 ORDER BY uo.block_number DESC, uo.log_index DESC
			 LIMIT $1 OFFSET $2`,
			p.Limit, p.Offset,
		)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("query operations: %w", err)
	}
	defer rows.Close()

	var total int64
	var ops []UserOperation
	for rows.Next() {
		var op UserOperation
		var revertReason []byte // nullable
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
			&op.AccountDeployed,
			&revertReason,
			&total,
		); err != nil {
			return nil, 0, fmt.Errorf("scan operation: %w", err)
		}
		op.RevertReason = revertReason
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate operations: %w", err)
	}

	return ops, total, nil
}

// GetOperationByHash returns a single user operation enriched with
// accountDeployed / revertReason. Returns nil, nil when no match.
func GetOperationByHash(ctx context.Context, pool *pgxpool.Pool, hash []byte) (*UserOperation, error) {
	if pool == nil {
		return nil, errors.New("pool is required")
	}
	var op UserOperation
	var revertReason []byte
	err := pool.QueryRow(ctx, `
		SELECT uo.id, uo.user_op_hash, uo.sender, uo.paymaster, uo.target, uo.calldata,
		       uo.nonce, uo.success, uo.actual_gas_cost, uo.actual_gas_used,
		       uo.tx_hash, uo.block_number, uo.block_timestamp, uo.log_index,
		       (ad.user_op_hash IS NOT NULL) AS account_deployed,
		       ur.revert_reason
		FROM user_operations uo
		LEFT JOIN account_deployments     ad ON ad.user_op_hash = uo.user_op_hash
		LEFT JOIN user_operation_reverts  ur ON ur.user_op_hash = uo.user_op_hash
		WHERE uo.user_op_hash = $1
		ORDER BY uo.block_number DESC, uo.log_index DESC
		LIMIT 1`,
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
		&op.AccountDeployed,
		&revertReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query operation by hash: %w", err)
	}
	op.RevertReason = revertReason
	return &op, nil
}

// Stats holds aggregate statistics for indexed user operations.
type Stats struct {
	TotalOps              int64
	SuccessCount          int64
	SponsoredCount        int64
	UniqueSenders         int64
	AccountsDeployedCount int64
}

// zeroPaymaster is the 20-byte zero address used to identify self-funded
// (non-sponsored) operations. Any other paymaster value is considered sponsored.
// Declared as a fixed-size array so each [:] call produces a fresh slice header
// without risk of append or reslice changing the underlying length.
var zeroPaymaster = [20]byte{}

// GetStats returns aggregate statistics across all indexed operations.
// AccountsDeployedCount counts user_operations rows that have a matching
// account_deployments row, joined on user_op_hash.
func GetStats(ctx context.Context, pool *pgxpool.Pool) (Stats, error) {
	if pool == nil {
		return Stats{}, errors.New("pool is required")
	}
	var s Stats
	err := pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE uo.success),
		       count(*) FILTER (WHERE uo.paymaster != $1),
		       count(DISTINCT uo.sender),
		       count(*) FILTER (WHERE ad.user_op_hash IS NOT NULL)
		FROM user_operations uo
		LEFT JOIN account_deployments ad ON ad.user_op_hash = uo.user_op_hash`,
		zeroPaymaster[:],
	).Scan(&s.TotalOps, &s.SuccessCount, &s.SponsoredCount, &s.UniqueSenders, &s.AccountsDeployedCount)
	if err != nil {
		return Stats{}, fmt.Errorf("query stats: %w", err)
	}
	return s, nil
}
