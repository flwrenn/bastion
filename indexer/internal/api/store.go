package api

import (
	"context"

	"github.com/flwrenn/bastion/indexer/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store abstracts the data layer so handlers can be tested without
// a real database. Each method mirrors a package-level db function.
type Store interface {
	ListOperations(ctx context.Context, p db.ListParams) ([]db.UserOperation, int64, error)
	GetOperationByHash(ctx context.Context, hash []byte) (*db.UserOperation, error)
	GetStats(ctx context.Context) (db.Stats, error)
}

// PgStore implements Store by delegating to the db package functions
// with a pgxpool.Pool.
type PgStore struct {
	Pool *pgxpool.Pool
}

func (s *PgStore) ListOperations(ctx context.Context, p db.ListParams) ([]db.UserOperation, int64, error) {
	return db.ListOperations(ctx, s.Pool, p)
}

func (s *PgStore) GetOperationByHash(ctx context.Context, hash []byte) (*db.UserOperation, error) {
	return db.GetOperationByHash(ctx, s.Pool, hash)
}

func (s *PgStore) GetStats(ctx context.Context) (db.Stats, error) {
	return db.GetStats(ctx, s.Pool)
}
