package indexer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunReturnsNilWhenContextCancelledBeforeInitialIteration(t *testing.T) {
	t.Parallel()

	svc := &Service{cfg: Config{PollInterval: time.Millisecond}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := svc.Run(ctx); err != nil {
		t.Fatalf("expected nil error on canceled context, got %v", err)
	}
}

func TestRunReturnsErrorWhenPoolIsNil(t *testing.T) {
	t.Parallel()

	svc := &Service{
		cfg: Config{PollInterval: time.Second},
		rpc: newRPCClient("http://127.0.0.1:1", 1024),
	}

	err := svc.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when pool is nil")
	}
	if !strings.Contains(err.Error(), "pool is required") {
		t.Fatalf("expected pool error, got %v", err)
	}
}

func TestRunReturnsErrorWhenRPCClientIsNil(t *testing.T) {
	t.Parallel()

	svc := &Service{
		cfg:  Config{PollInterval: time.Second},
		pool: &pgxpool.Pool{},
	}

	err := svc.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when rpc client is nil")
	}
	if !strings.Contains(err.Error(), "rpc client is required") {
		t.Fatalf("expected rpc client error, got %v", err)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "nil pool",
			cfg: Config{
				RPCURL:              "http://127.0.0.1:8545",
				EntryPoint:          defaultEntryPoint,
				PollInterval:        time.Second,
				BatchSize:           1,
				RequestTimeout:      time.Second,
				RPCConcurrency:      1,
				RPCResponseMaxBytes: 1024,
				EnableTxEnrichment:  true,
				StateKey:            stateKeyLastIndexedBlock,
			},
			wantErr: "pool is required",
		},
		{
			name: "missing rpc url",
			cfg: Config{
				EntryPoint:          defaultEntryPoint,
				PollInterval:        time.Second,
				BatchSize:           1,
				RequestTimeout:      time.Second,
				RPCConcurrency:      1,
				RPCResponseMaxBytes: 1024,
				EnableTxEnrichment:  true,
				StateKey:            stateKeyLastIndexedBlock,
			},
			wantErr: "RPCURL is required",
		},
		{
			name: "non-positive poll interval",
			cfg: Config{
				RPCURL:              "http://127.0.0.1:8545",
				EntryPoint:          defaultEntryPoint,
				PollInterval:        0,
				BatchSize:           1,
				RequestTimeout:      time.Second,
				RPCConcurrency:      1,
				RPCResponseMaxBytes: 1024,
				EnableTxEnrichment:  true,
				StateKey:            stateKeyLastIndexedBlock,
			},
			wantErr: "PollInterval must be greater than 0",
		},
		{
			name: "zero batch size",
			cfg: Config{
				RPCURL:              "http://127.0.0.1:8545",
				EntryPoint:          defaultEntryPoint,
				PollInterval:        time.Second,
				BatchSize:           0,
				RequestTimeout:      time.Second,
				RPCConcurrency:      1,
				RPCResponseMaxBytes: 1024,
				EnableTxEnrichment:  true,
				StateKey:            stateKeyLastIndexedBlock,
			},
			wantErr: "BatchSize must be greater than 0",
		},
		{
			name: "non-positive request timeout",
			cfg: Config{
				RPCURL:              "http://127.0.0.1:8545",
				EntryPoint:          defaultEntryPoint,
				PollInterval:        time.Second,
				BatchSize:           1,
				RequestTimeout:      0,
				RPCConcurrency:      1,
				RPCResponseMaxBytes: 1024,
				EnableTxEnrichment:  true,
				StateKey:            stateKeyLastIndexedBlock,
			},
			wantErr: "RequestTimeout must be greater than 0",
		},
		{
			name: "non-positive rpc concurrency",
			cfg: Config{
				RPCURL:              "http://127.0.0.1:8545",
				EntryPoint:          defaultEntryPoint,
				PollInterval:        time.Second,
				BatchSize:           1,
				RequestTimeout:      time.Second,
				RPCConcurrency:      0,
				RPCResponseMaxBytes: 1024,
				EnableTxEnrichment:  true,
				StateKey:            stateKeyLastIndexedBlock,
			},
			wantErr: "RPCConcurrency must be greater than 0",
		},
		{
			name: "non-positive rpc response size",
			cfg: Config{
				RPCURL:              "http://127.0.0.1:8545",
				EntryPoint:          defaultEntryPoint,
				PollInterval:        time.Second,
				BatchSize:           1,
				RequestTimeout:      time.Second,
				RPCConcurrency:      1,
				RPCResponseMaxBytes: 0,
				EnableTxEnrichment:  true,
				StateKey:            stateKeyLastIndexedBlock,
			},
			wantErr: "RPCResponseMaxBytes must be greater than 0",
		},
		{
			name: "missing state key",
			cfg: Config{
				RPCURL:              "http://127.0.0.1:8545",
				EntryPoint:          defaultEntryPoint,
				PollInterval:        time.Second,
				BatchSize:           1,
				RequestTimeout:      time.Second,
				RPCConcurrency:      1,
				RPCResponseMaxBytes: 1024,
				EnableTxEnrichment:  true,
				StateKey:            "",
			},
			wantErr: "StateKey is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool := &pgxpool.Pool{}
			if tt.name == "nil pool" {
				pool = nil
			}

			_, err := New(tt.cfg, pool)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
