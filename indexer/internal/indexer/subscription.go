package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
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
	conn      *websocket.Conn
	nextMu    sync.Mutex
	closeOnce sync.Once
}

func newWebSocketHeadSubscription(ctx context.Context, wsURL string) (headSubscription, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}

	if err := sendSubscribeRequest(ctx, conn); err != nil {
		conn.CloseNow()
		return nil, err
	}

	sub := &websocketHeadSubscription{conn: conn}
	if err := waitForSubscriptionAck(ctx, sub); err != nil {
		_ = sub.Close()
		return nil, err
	}

	return sub, nil
}

func sendSubscribeRequest(ctx context.Context, conn *websocket.Conn) error {
	payload, err := json.Marshal(rpcRequest{
		JSONRPC: jsonRPCVersion,
		ID:      1,
		Method:  "eth_subscribe",
		Params:  []any{subscriptionTypeNewHeads},
	})
	if err != nil {
		return fmt.Errorf("marshal subscribe request: %w", err)
	}

	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
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
		closeErr = s.conn.Close(websocket.StatusNormalClosure, "")
	})

	return closeErr
}

func (s *websocketHeadSubscription) read(ctx context.Context) ([]byte, error) {
	s.nextMu.Lock()
	defer s.nextMu.Unlock()

	for {
		_, message, err := s.conn.Read(ctx)
		if err != nil {
			return nil, err
		}
		if len(message) > 0 {
			return message, nil
		}
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
