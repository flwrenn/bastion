package indexer

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestRunReturnsErrorWhenInitialIterationFails(t *testing.T) {
	t.Parallel()

	svc := &Service{
		cfg: Config{
			PollInterval:   time.Millisecond,
			RequestTimeout: time.Millisecond,
		},
		rpc: newRPCClient("http://127.0.0.1:1"),
	}

	err := svc.Run(context.Background())
	if err == nil {
		t.Fatal("expected initial iteration error")
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
			name: "missing rpc url",
			cfg: Config{
				EntryPoint:         defaultEntryPoint,
				PollInterval:       time.Second,
				BatchSize:          1,
				RequestTimeout:     time.Second,
				RPCConcurrency:     1,
				EnableTxEnrichment: true,
				StateKey:           stateKeyLastIndexedBlock,
			},
			wantErr: "RPCURL is required",
		},
		{
			name: "non-positive poll interval",
			cfg: Config{
				RPCURL:             "http://127.0.0.1:8545",
				EntryPoint:         defaultEntryPoint,
				PollInterval:       0,
				BatchSize:          1,
				RequestTimeout:     time.Second,
				RPCConcurrency:     1,
				EnableTxEnrichment: true,
				StateKey:           stateKeyLastIndexedBlock,
			},
			wantErr: "PollInterval must be greater than 0",
		},
		{
			name: "zero batch size",
			cfg: Config{
				RPCURL:             "http://127.0.0.1:8545",
				EntryPoint:         defaultEntryPoint,
				PollInterval:       time.Second,
				BatchSize:          0,
				RequestTimeout:     time.Second,
				RPCConcurrency:     1,
				EnableTxEnrichment: true,
				StateKey:           stateKeyLastIndexedBlock,
			},
			wantErr: "BatchSize must be greater than 0",
		},
		{
			name: "non-positive request timeout",
			cfg: Config{
				RPCURL:             "http://127.0.0.1:8545",
				EntryPoint:         defaultEntryPoint,
				PollInterval:       time.Second,
				BatchSize:          1,
				RequestTimeout:     0,
				RPCConcurrency:     1,
				EnableTxEnrichment: true,
				StateKey:           stateKeyLastIndexedBlock,
			},
			wantErr: "RequestTimeout must be greater than 0",
		},
		{
			name: "non-positive rpc concurrency",
			cfg: Config{
				RPCURL:             "http://127.0.0.1:8545",
				EntryPoint:         defaultEntryPoint,
				PollInterval:       time.Second,
				BatchSize:          1,
				RequestTimeout:     time.Second,
				RPCConcurrency:     0,
				EnableTxEnrichment: true,
				StateKey:           stateKeyLastIndexedBlock,
			},
			wantErr: "RPCConcurrency must be greater than 0",
		},
		{
			name: "missing state key",
			cfg: Config{
				RPCURL:             "http://127.0.0.1:8545",
				EntryPoint:         defaultEntryPoint,
				PollInterval:       time.Second,
				BatchSize:          1,
				RequestTimeout:     time.Second,
				RPCConcurrency:     1,
				EnableTxEnrichment: true,
				StateKey:           "",
			},
			wantErr: "StateKey is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := New(tt.cfg, nil)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
