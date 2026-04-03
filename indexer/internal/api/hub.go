package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/flwrenn/bastion/indexer/internal/db"
)

const (
	clientSendBuffer = 32
	writeTimeout     = 5 * time.Second
)

// Hub manages WebSocket clients and broadcasts new operations to them.
type Hub struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
	closed  bool
}

type wsClient struct {
	send chan []byte
	conn *websocket.Conn
}

// NewHub creates a Hub ready to accept WebSocket connections.
func NewHub() *Hub {
	return &Hub{clients: make(map[*wsClient]struct{})}
}

// Broadcast converts each operation to JSON and sends it to every connected
// client. Slow consumers whose buffer is full are dropped immediately.
func (h *Hub) Broadcast(ops []db.UserOperation) {
	messages := make([][]byte, 0, len(ops))
	for i := range ops {
		data, err := json.Marshal(toResponse(ops[i]))
		if err != nil {
			slog.Error("marshal broadcast message", "error", err)
			continue
		}
		messages = append(messages, data)
	}
	if len(messages) == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		for _, msg := range messages {
			select {
			case c.send <- msg:
			default:
				// Slow client — drop it and close the connection so any
				// blocked Write unblocks promptly.
				h.removeClientLocked(c)
				break
			}
		}
	}
}

// ServeWS upgrades an HTTP request to a WebSocket connection and streams
// broadcast messages until the client disconnects or the hub shuts down.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		slog.Error("websocket accept", "error", err)
		return
	}

	client := &wsClient{
		send: make(chan []byte, clientSendBuffer),
		conn: conn,
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		conn.Close(websocket.StatusGoingAway, "server shutting down")
		return
	}
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	slog.Info("websocket client connected", "remote", r.RemoteAddr)

	// CloseRead returns a context that is cancelled when the client sends a
	// close frame or the underlying connection breaks.
	ctx := conn.CloseRead(r.Context())

	defer func() {
		h.mu.Lock()
		h.removeClientLocked(client)
		h.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
		slog.Info("websocket client disconnected", "remote", r.RemoteAddr)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.send:
			if !ok {
				// Channel closed by Shutdown or Broadcast (slow client).
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// Shutdown closes every connected client and prevents new connections.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true
	for c := range h.clients {
		h.removeClientLocked(c)
	}
}

// removeClientLocked removes a client, closes its send channel, and forces
// the underlying WebSocket connection closed so any blocked write unblocks.
// Caller must hold h.mu.
func (h *Hub) removeClientLocked(c *wsClient) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	close(c.send)
	if c.conn != nil {
		c.conn.CloseNow()
	}
}
