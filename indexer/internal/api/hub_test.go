package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/flwrenn/bastion/indexer/internal/db"
)

// waitForClients polls hub.clients until it reaches n or the deadline expires.
func waitForClients(t *testing.T, hub *Hub, n int, deadline time.Duration) {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		hub.mu.Lock()
		count := len(hub.clients)
		hub.mu.Unlock()
		if count >= n {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("timed out waiting for %d client(s), have %d", n, count)
		case <-ticker.C:
		}
	}
}

func TestBroadcastNoClients(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	// Must not panic with zero clients.
	hub.Broadcast([]db.UserOperation{testOp()})
}

func TestBroadcastEmptyOps(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	// Must not panic with empty slice.
	hub.Broadcast(nil)
	hub.Broadcast([]db.UserOperation{})
}

func TestBroadcastDelivered(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	client := &wsClient{send: make(chan []byte, clientSendBuffer)}
	hub.mu.Lock()
	hub.clients[client] = struct{}{}
	hub.mu.Unlock()

	hub.Broadcast([]db.UserOperation{testOp()})

	select {
	case msg := <-client.send:
		var resp operationResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Sender != "0x"+testAddr {
			t.Fatalf("sender = %q, want 0x%s", resp.Sender, testAddr)
		}
		if resp.Nonce != "42" {
			t.Fatalf("nonce = %q, want 42", resp.Nonce)
		}
	default:
		t.Fatal("expected message on client send channel")
	}
}

func TestBroadcastDropsSlowClient(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	slow := &wsClient{send: make(chan []byte)} // unbuffered = always full
	hub.mu.Lock()
	hub.clients[slow] = struct{}{}
	hub.mu.Unlock()

	hub.Broadcast([]db.UserOperation{testOp()})

	hub.mu.Lock()
	_, exists := hub.clients[slow]
	hub.mu.Unlock()

	if exists {
		t.Fatal("expected slow client to be removed")
	}
}

func TestShutdownClosesClients(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	c := &wsClient{send: make(chan []byte, clientSendBuffer)}
	hub.mu.Lock()
	hub.clients[c] = struct{}{}
	hub.mu.Unlock()

	hub.Shutdown()

	// send channel should be closed.
	_, ok := <-c.send
	if ok {
		t.Fatal("expected send channel to be closed")
	}

	hub.mu.Lock()
	count := len(hub.clients)
	hub.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 clients, got %d", count)
	}
}

func TestShutdownRejectsNewConnections(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	hub.Shutdown()

	srv := httptest.NewServer(hub)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	wsURL := "ws" + srv.URL[len("http"):]
	conn, _, err := websocket.Dial(ctx, wsURL+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Use a short deadline so the test fails fast if the server doesn't
	// close the connection, rather than silently passing on context timeout.
	readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer readCancel()

	_, _, err = conn.Read(readCtx)
	if err == nil {
		t.Fatal("expected connection to be closed after shutdown")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("server did not close connection promptly; timed out waiting")
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("expected websocket.CloseError, got %T: %v", err, err)
	}
	if closeErr.Code != websocket.StatusGoingAway {
		t.Fatalf("close code = %v, want StatusGoingAway", closeErr.Code)
	}
}

func TestServeWSEndToEnd(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Shutdown()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	waitForClients(t, hub, 1, 2*time.Second)

	hub.Broadcast([]db.UserOperation{testOp()})

	_, msg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var resp operationResponse
	if err := json.Unmarshal(msg, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.UserOpHash != "0x"+testHash {
		t.Fatalf("userOpHash = %q, want 0x%s", resp.UserOpHash, testHash)
	}
	if resp.BlockNumber != 100 {
		t.Fatalf("blockNumber = %d, want 100", resp.BlockNumber)
	}
}

func TestServeWSMultipleMessages(t *testing.T) {
	t.Parallel()

	hub := NewHub()
	defer hub.Shutdown()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", hub.ServeWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + srv.URL[len("http"):] + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	waitForClients(t, hub, 1, 2*time.Second)

	// Broadcast two operations in one batch.
	op1 := testOp()
	op2 := testOp()
	op2.Nonce = "99"
	hub.Broadcast([]db.UserOperation{op1, op2})

	// Should receive two separate messages.
	for _, wantNonce := range []string{"42", "99"} {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var resp operationResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Nonce != wantNonce {
			t.Fatalf("nonce = %q, want %q", resp.Nonce, wantNonce)
		}
	}
}

// hub implements http.Handler so httptest.NewServer can serve it directly
// for the shutdown rejection test.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.ServeWS(w, r)
}
