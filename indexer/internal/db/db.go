// Package db manages the PostgreSQL connection pool and schema migrations.
package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a connection pool from a PostgreSQL DSN and verifies
// connectivity with a ping. The caller must call pool.Close() when done.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	slog.Info("connected to database", "host", config.ConnConfig.Host, "database", config.ConnConfig.Database)
	return pool, nil
}
