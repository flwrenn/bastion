package db

import (
	"context"
	"strings"
	"testing"
)

func TestListOperationsRejectsNilPool(t *testing.T) {
	t.Parallel()

	_, _, err := ListOperations(context.Background(), nil, ListParams{})
	if err == nil {
		t.Fatal("expected nil-pool error")
	}
	if !strings.Contains(err.Error(), "pool is required") {
		t.Fatalf("expected pool error, got %v", err)
	}
}

func TestListOperationsClampsPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		limit      int
		offset     int
		wantLimit  int
		wantOffset int
	}{
		{"zero limit defaults to 20", 0, 0, 20, 0},
		{"negative limit defaults to 20", -5, 0, 20, 0},
		{"over-100 clamped to 100", 200, 0, 100, 0},
		{"negative offset clamped to 0", 10, -3, 10, 0},
		{"over-10000 offset clamped to 10000", 20, 20000, 20, 10000},
		{"valid values unchanged", 50, 10, 50, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := ListParams{Limit: tt.limit, Offset: tt.offset}
			ClampListParams(&p)
			if p.Limit != tt.wantLimit {
				t.Fatalf("limit: got %d, want %d", p.Limit, tt.wantLimit)
			}
			if p.Offset != tt.wantOffset {
				t.Fatalf("offset: got %d, want %d", p.Offset, tt.wantOffset)
			}
		})
	}
}

func TestGetOperationByHashRejectsNilPool(t *testing.T) {
	t.Parallel()

	_, err := GetOperationByHash(context.Background(), nil, make([]byte, 32))
	if err == nil {
		t.Fatal("expected nil-pool error")
	}
	if !strings.Contains(err.Error(), "pool is required") {
		t.Fatalf("expected pool error, got %v", err)
	}
}

func TestGetStatsRejectsNilPool(t *testing.T) {
	t.Parallel()

	_, err := GetStats(context.Background(), nil)
	if err == nil {
		t.Fatal("expected nil-pool error")
	}
	if !strings.Contains(err.Error(), "pool is required") {
		t.Fatalf("expected pool error, got %v", err)
	}
}
