package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Advisory lock ID for migration serialisation. Chosen arbitrarily;
// must be consistent across all indexer instances sharing the same DB.
const migrationLockID int64 = 0x626173_6d696772 // "bas" + "migr"

const cleanupTimeout = 5 * time.Second

// cleanupCtx returns a bounded context detached from the caller, ensuring
// cleanup operations (unlock, rollback) aren't blocked by a canceled parent.
func cleanupCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), cleanupTimeout)
}

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate runs all pending SQL migrations in lexicographic order.
// Each migration runs in its own transaction. DDL is executed via
// the simple query protocol to support multi-statement files.
// Applied migrations are tracked in schema_migrations for idempotency.
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
		cctx, cancel := cleanupCtx()
		defer cancel()
		if _, unlockErr := conn.Exec(cctx, "SELECT pg_advisory_unlock($1)", migrationLockID); unlockErr != nil {
			conn.Conn().Close(cctx)
			slog.Error("migration lock release failed; connection destroyed", "err", unlockErr)
		}
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

		// Read and execute the migration.
		sql, err := migrationFS.ReadFile(path.Join("migrations", name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		pgConn := conn.Conn().PgConn()

		rollback := func() {
			rctx, cancel := cleanupCtx()
			defer cancel()
			if _, rbErr := pgConn.Exec(rctx, "ROLLBACK").ReadAll(); rbErr != nil {
				conn.Conn().Close(rctx)
				slog.Error("migration rollback failed; connection destroyed", "migration", name, "err", rbErr)
			}
		}

		// Wrap DDL + tracking INSERT in a transaction.
		// BEGIN/COMMIT use the simple query protocol (pgconn) to stay
		// on the same connection; the INSERT uses pgx's extended
		// protocol for parameterised safety.
		if _, err := pgConn.Exec(ctx, "BEGIN").ReadAll(); err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}

		if _, err := pgConn.Exec(ctx, string(sql)).ReadAll(); err != nil {
			rollback()
			return fmt.Errorf("execute migration %s: %w", name, err)
		}

		if _, err := conn.Exec(ctx,
			"INSERT INTO schema_migrations (filename) VALUES ($1)", name,
		); err != nil {
			rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if _, err := pgConn.Exec(ctx, "COMMIT").ReadAll(); err != nil {
			rollback()
			return fmt.Errorf("commit migration %s: %w", name, err)
		}

		slog.Info("applied migration", "file", name)
	}

	return nil
}
