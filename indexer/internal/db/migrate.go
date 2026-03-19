package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Advisory lock ID for migration serialisation. Chosen arbitrarily;
// must be consistent across all indexer instances sharing the same DB.
const migrationLockID int64 = 0x626173_6d696772 // "bas" + "migr"

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate runs all pending SQL migrations in lexicographic order.
// Each migration runs in its own transaction. Applied migrations are
// tracked in the schema_migrations table to ensure idempotency.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Acquire an advisory lock so concurrent instances don't race.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for migration lock: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	// Ensure the tracking table exists.
	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	// Sort by filename to guarantee execution order.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		// Check if already applied.
		var exists bool
		err := conn.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename = $1)",
			name,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		// Read and execute the migration in a transaction.
		sql, err := migrationFS.ReadFile(path.Join("migrations", name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", name, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (filename) VALUES ($1)", name,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}

		slog.Info("applied migration", "file", name)
	}

	return nil
}
