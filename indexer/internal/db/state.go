package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GetState(ctx context.Context, pool *pgxpool.Pool, key string) (string, bool, error) {
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
