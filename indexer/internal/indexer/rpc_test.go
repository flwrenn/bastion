package indexer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRPCCallRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	const limit = int64(1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"`)
		chunk := make([]byte, limit)
		for i := range chunk {
			chunk[i] = 'a'
		}
		_, _ = w.Write(chunk)
		_, _ = fmt.Fprint(w, `"}`)
	}))
	defer server.Close()

	client := newRPCClient(server.URL, limit)
	err := client.call(context.Background(), "eth_blockNumber", []any{}, nil)
	if err == nil {
		t.Fatal("expected oversized response error")
	}

	if !isRPCResponseTooLarge(err) {
		t.Fatalf("expected oversized-response sentinel error, got %v", err)
	}

	expected := fmt.Sprintf("exceeds %d bytes", limit)
	if got := err.Error(); got == "" || !strings.Contains(got, expected) {
		t.Fatalf("expected error containing %q, got %q", expected, got)
	}
}
