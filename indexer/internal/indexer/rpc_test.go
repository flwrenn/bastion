package indexer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

	client := newRPCClient(server.URL, limit, retryConfig{MaxAttempts: 1, RequestTimeout: 5 * time.Second})
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

func TestRPCCallTruncatesHTTPErrorBody(t *testing.T) {
	t.Parallel()

	// Serve a large non-2xx body well over the retained excerpt size.
	const bodySize = 16 * 1024
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		chunk := make([]byte, bodySize)
		for i := range chunk {
			chunk[i] = 'x'
		}
		_, _ = w.Write(chunk)
	}))
	defer server.Close()

	client := newRPCClient(server.URL, int64(bodySize*2), retryConfig{MaxAttempts: 1, RequestTimeout: 5 * time.Second})
	err := client.call(context.Background(), "eth_blockNumber", []any{}, nil)
	if err == nil {
		t.Fatal("expected HTTP status error")
	}

	var httpErr *httpStatusError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *httpStatusError, got %T: %v", err, err)
	}
	if got := len(httpErr.body); got > httpStatusErrorBodyLimit+64 {
		t.Fatalf("retained body too large: got %d bytes, want <= %d", got, httpStatusErrorBodyLimit+64)
	}
	if !strings.Contains(httpErr.body, "truncated") {
		t.Fatalf("expected truncation marker in body, got %q", httpErr.body)
	}
}

func TestBackoffDelayIsOverflowSafe(t *testing.T) {
	t.Parallel()

	client := &rpcClient{
		retry: retryConfig{
			BaseDelay: 500 * time.Millisecond,
			MaxDelay:  30 * time.Second,
		},
	}

	// Unreasonably large attempt counts must never produce a non-positive delay
	// or exceed MaxDelay. This guards against int64 overflow in BaseDelay<<attempt.
	for _, attempt := range []int{0, 1, 10, 30, 62, 1_000, 1 << 20} {
		delay := client.backoffDelay(attempt, nil)
		if delay <= 0 {
			t.Errorf("attempt=%d produced non-positive delay %s", attempt, delay)
		}
		if delay > client.retry.MaxDelay {
			t.Errorf("attempt=%d produced delay %s exceeding MaxDelay %s", attempt, delay, client.retry.MaxDelay)
		}
	}
}

func TestBackoffDelayHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	client := &rpcClient{
		retry: retryConfig{
			BaseDelay: 500 * time.Millisecond,
			MaxDelay:  30 * time.Second,
		},
	}

	err := &httpStatusError{statusCode: 429, retryAfter: 2 * time.Second}
	if got := client.backoffDelay(0, err); got != 2*time.Second {
		t.Errorf("expected Retry-After delay of 2s, got %s", got)
	}

	// Retry-After beyond MaxDelay is clamped.
	err = &httpStatusError{statusCode: 429, retryAfter: time.Hour}
	if got := client.backoffDelay(0, err); got != client.retry.MaxDelay {
		t.Errorf("expected clamped delay %s, got %s", client.retry.MaxDelay, got)
	}
}
