package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTrimOperationsAboveBlockAndSetCursorRejectsOverflow(t *testing.T) {
	t.Parallel()

	err := TrimOperationsAboveBlockAndSetCursor(
		context.Background(),
		(*pgxpool.Pool)(nil),
		"cursor",
		^uint64(0),
	)
	if err == nil {
		t.Fatal("expected overflow error")
	}
}
