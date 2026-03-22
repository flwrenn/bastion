package db

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGetStateRejectsNilPool(t *testing.T) {
	t.Parallel()

	_, _, err := GetState(context.Background(), nil, "cursor")
	if err == nil {
		t.Fatal("expected nil-pool error")
	}
	if !strings.Contains(err.Error(), "pool is required") {
		t.Fatalf("expected pool error, got %v", err)
	}
}

func TestTrimOperationsAboveBlockAndSetCursorRejectsNilPool(t *testing.T) {
	t.Parallel()

	err := TrimOperationsAboveBlockAndSetCursor(context.Background(), nil, "cursor", 1)
	if err == nil {
		t.Fatal("expected nil-pool error")
	}
	if !strings.Contains(err.Error(), "pool is required") {
		t.Fatalf("expected pool error, got %v", err)
	}
}

func TestTrimOperationsAboveBlockAndSetCursorRejectsOverflow(t *testing.T) {
	t.Parallel()

	err := TrimOperationsAboveBlockAndSetCursor(
		context.Background(),
		&pgxpool.Pool{},
		"cursor",
		^uint64(0),
	)
	if err == nil {
		t.Fatal("expected overflow error")
	}
	if !strings.Contains(err.Error(), "overflows int64") {
		t.Fatalf("expected overflow error, got %v", err)
	}
}
