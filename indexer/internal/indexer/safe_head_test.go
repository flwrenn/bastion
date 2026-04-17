package indexer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSafeHeadReturnsNoSafeHeadWhenConfirmationsNotMet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2"}`))
	}))
	defer server.Close()

	service := &Service{
		cfg: Config{
			Confirmations:  5,
			RequestTimeout: time.Second,
		},
		rpc: newRPCClient(server.URL, 1024, retryConfig{MaxAttempts: 1, RequestTimeout: 5 * time.Second}),
	}

	_, ok, err := service.safeHead(context.Background())
	if err != nil {
		t.Fatalf("safeHead returned error: %v", err)
	}
	if ok {
		t.Fatal("expected no safe head when latest block has fewer confirmations")
	}
}
