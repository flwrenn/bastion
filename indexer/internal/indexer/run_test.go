package indexer

import (
	"context"
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
