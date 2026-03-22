package db

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GetState(ctx context.Context, pool *pgxpool.Pool, key string) (string, bool, error) {
	if pool == nil {
		return "", false, fmt.Errorf("pool is required")
	}

	var value string
	err := pool.QueryRow(ctx, "SELECT value FROM indexer_state WHERE key = $1", key).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query state %q: %w", key, err)
	}
	return value, true, nil
}

func GetStateUint64(ctx context.Context, pool *pgxpool.Pool, key string) (uint64, bool, error) {
	value, ok, err := GetState(ctx, pool, key)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		return 0, false, nil
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse state %q value %q as uint64: %w", key, value, err)
	}

	return parsed, true, nil
}

func setStateTx(ctx context.Context, tx pgx.Tx, key, value string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO indexer_state (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key)
		DO UPDATE SET value = EXCLUDED.value
	`, key, value)
	if err != nil {
		return fmt.Errorf("upsert state %q: %w", key, err)
	}

	return nil
}

func TrimOperationsAboveBlockAndSetCursor(
	ctx context.Context,
	pool *pgxpool.Pool,
	stateKey string,
	safeHead uint64,
) error {
	if pool == nil {
		return fmt.Errorf("pool is required")
	}

	if safeHead > math.MaxInt64 {
		return fmt.Errorf("safe head %d overflows int64", safeHead)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		"DELETE FROM user_operations WHERE block_number > $1",
		int64(safeHead),
	); err != nil {
		return fmt.Errorf("delete operations above safe head %d: %w", safeHead, err)
	}

	if err := setStateTx(ctx, tx, stateKey, strconv.FormatUint(safeHead, 10)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}
