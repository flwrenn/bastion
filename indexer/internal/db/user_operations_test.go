package db

import (
	"context"
	"strings"
	"testing"
)

func TestReplaceOperationsAndSetCursorRejectsNilPool(t *testing.T) {
	t.Parallel()

	err := ReplaceOperationsAndSetCursor(
		context.Background(),
		nil,
		"user_operations.last_indexed_block",
		0,
		0,
		0,
		nil,
	)
	if err == nil {
		t.Fatal("expected nil-pool error")
	}
	if !strings.Contains(err.Error(), "pool is required") {
		t.Fatalf("expected pool error, got %v", err)
	}
}

func TestReplaceOperationsAndSetCursorRejectsEmptyStateKey(t *testing.T) {
	t.Parallel()

	err := ReplaceOperationsAndSetCursor(
		context.Background(),
		nil,
		"",
		0,
		0,
		0,
		nil,
	)
	if err == nil {
		t.Fatal("expected state key error")
	}
	if !strings.Contains(err.Error(), "state key is required") {
		t.Fatalf("expected state key error, got %v", err)
	}
}
