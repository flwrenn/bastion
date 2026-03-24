package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

const (
	subscriptionTypeNewHeads   = "newHeads"
	defaultSubscriptionBackoff = 2 * time.Second
)

type headSubscription interface {
	Next(ctx context.Context) error
	Close() error
}

type websocketHeadSubscription struct {
	ws        *websocket.Conn
	nextMu    sync.Mutex
	closeOnce sync.Once
}

func newWebSocketHeadSubscription(ctx context.Context, wsURL string) (headSubscription, error) {
	config, err := websocket.NewConfig(wsURL, originForWebSocketURL(wsURL))
	if err != nil {
		return nil, fmt.Errorf("create websocket config: %w", err)
	}

	ws, err := config.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	if err := sendSubscribeRequest(ctx, ws); err != nil {
		_ = ws.Close()
		return nil, err
	}

	sub := &websocketHeadSubscription{ws: ws}
	if err := waitForSubscriptionAck(ctx, sub); err != nil {
		_ = sub.Close()
		return nil, err
	}

	return sub, nil
}

func sendSubscribeRequest(ctx context.Context, ws *websocket.Conn) error {
	payload, err := json.Marshal(rpcRequest{
		JSONRPC: jsonRPCVersion,
		ID:      1,
		Method:  "eth_subscribe",
		Params:  []any{subscriptionTypeNewHeads},
	})
	if err != nil {
		return fmt.Errorf("marshal subscribe request: %w", err)
	}

	if err := writeWebSocketMessage(ctx, ws, payload); err != nil {
		return fmt.Errorf("send subscribe request: %w", err)
	}

	return nil
}

func waitForSubscriptionAck(ctx context.Context, sub *websocketHeadSubscription) error {
	for {
		message, err := sub.read(ctx)
		if err != nil {
			return fmt.Errorf("read subscription ack: %w", err)
		}

		var response rpcResponse
		if err := json.Unmarshal(message, &response); err == nil && response.ID == 1 {
			if response.Error != nil {
				return fmt.Errorf("subscription rejected %d: %s", response.Error.Code, response.Error.Message)
			}
			if len(response.Result) == 0 || string(response.Result) == "null" {
				return fmt.Errorf("subscription ack missing result")
			}
			return nil
		}

		if rpcErr := decodeRPCError(message); rpcErr != nil {
			return rpcErr
		}
	}
}

func (s *websocketHeadSubscription) Next(ctx context.Context) error {
	for {
		message, err := s.read(ctx)
		if err != nil {
			return err
		}

		var notification rpcSubscriptionNotification
		if err := json.Unmarshal(message, &notification); err != nil {
			continue
		}

		if notification.Method != "eth_subscription" {
			if rpcErr := decodeRPCError(message); rpcErr != nil {
				return rpcErr
			}
			continue
		}
		if notification.Params.Subscription == "" {
			continue
		}

		var head rpcSubscriptionHead
		if err := json.Unmarshal(notification.Params.Result, &head); err != nil {
			continue
		}
		if head.Number == "" {
			continue
		}

		return nil
	}
}

func (s *websocketHeadSubscription) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.ws.Close()
	})

	return closeErr
}

func (s *websocketHeadSubscription) read(ctx context.Context) ([]byte, error) {
	s.nextMu.Lock()
	defer s.nextMu.Unlock()

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		readDeadline := time.Now().Add(500 * time.Millisecond)
		if err := s.ws.SetReadDeadline(readDeadline); err != nil {
			return nil, fmt.Errorf("set read deadline: %w", err)
		}

		var message []byte
		err := websocket.Message.Receive(s.ws, &message)
		if err == nil {
			if len(message) == 0 {
				continue
			}
			return message, nil
		}

		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			continue
		}

		return nil, err
	}
}

func writeWebSocketMessage(ctx context.Context, ws *websocket.Conn, payload []byte) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		writeDeadline := time.Now().Add(500 * time.Millisecond)
		if err := ws.SetWriteDeadline(writeDeadline); err != nil {
			return fmt.Errorf("set write deadline: %w", err)
		}

		err := websocket.Message.Send(ws, payload)
		if err == nil {
			return nil
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			continue
		}

		return err
	}
}

type rpcSubscriptionNotification struct {
	Method string `json:"method"`
	Params struct {
		Subscription string          `json:"subscription"`
		Result       json.RawMessage `json:"result"`
	} `json:"params"`
}

type rpcSubscriptionHead struct {
	Number string `json:"number"`
}

func originForWebSocketURL(wsURL string) string {
	parsed, err := url.Parse(wsURL)
	if err == nil && parsed.Host != "" {
		scheme := "http"
		if strings.EqualFold(parsed.Scheme, "wss") {
			scheme = "https"
		}

		return scheme + "://" + parsed.Host
	}

	if strings.HasPrefix(strings.ToLower(wsURL), "wss://") {
		return "https://bastion.local"
	}

	return "http://bastion.local"
}

func decodeRPCError(message []byte) error {
	var response rpcResponse
	if err := json.Unmarshal(message, &response); err != nil {
		return nil
	}
	if response.Error == nil {
		return nil
	}

	return fmt.Errorf("subscription rpc error %d: %s", response.Error.Code, response.Error.Message)
}
