package indexer

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubHeadSubscription struct {
	nextFn  func(context.Context) error
	closed  bool
	closeFn func() error
}

func (s *stubHeadSubscription) Next(ctx context.Context) error {
	if s.nextFn == nil {
		<-ctx.Done()
		return ctx.Err()
	}

	return s.nextFn(ctx)
}

func (s *stubHeadSubscription) Close() error {
	s.closed = true
	if s.closeFn != nil {
		return s.closeFn()
	}

	return nil
}

func TestConsumeHeadSubscriptionSignalsWakeWithoutBlocking(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nextCalls := 0
	subscription := &stubHeadSubscription{
		nextFn: func(context.Context) error {
			nextCalls++
			if nextCalls >= 3 {
				return errors.New("done")
			}
			return nil
		},
	}

	service := &Service{}
	wakeCh := make(chan struct{}, 1)
	err := service.consumeHeadSubscription(ctx, subscription, wakeCh)
	if err == nil {
		t.Fatal("expected consumeHeadSubscription error")
	}

	if len(wakeCh) != 1 {
		t.Fatalf("expected wake signal to be buffered once, got %d", len(wakeCh))
	}
}

func TestRunHeadSubscriptionLoopReconnectsAndSignalsWake(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wakeCh := make(chan struct{}, 4)
	connectAttempts := 0
	firstSubscription := &stubHeadSubscription{
		nextFn: func(context.Context) error {
			return errors.New("connection lost")
		},
	}
	secondSubscription := &stubHeadSubscription{}
	secondSubscription.nextFn = func(context.Context) error {
		select {
		case wakeCh <- struct{}{}:
		default:
		}
		cancel()
		return context.Canceled
	}

	service := &Service{
		cfg: Config{WSURL: "wss://ws.example"},
		newHeadSubscriptionFactory: func(context.Context, string) (headSubscription, error) {
			connectAttempts++
			if connectAttempts == 1 {
				return firstSubscription, nil
			}
			return secondSubscription, nil
		},
		subscriptionReconnectBackoff: time.Millisecond,
	}

	done := make(chan struct{})
	go func() {
		service.runHeadSubscriptionLoop(ctx, wakeCh)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscription loop did not stop")
	}

	if connectAttempts < 2 {
		t.Fatalf("expected at least 2 connection attempts, got %d", connectAttempts)
	}
	if !firstSubscription.closed {
		t.Fatal("expected first subscription to be closed")
	}
	if !secondSubscription.closed {
		t.Fatal("expected second subscription to be closed")
	}
}

func TestRunHeadSubscriptionLoopTimesOutStuckConnect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wakeCh := make(chan struct{}, 1)
	connectAttempts := 0
	service := &Service{
		cfg: Config{
			WSURL:          "wss://ws.example",
			RequestTimeout: 10 * time.Millisecond,
		},
		newHeadSubscriptionFactory: func(ctx context.Context, _ string) (headSubscription, error) {
			connectAttempts++
			<-ctx.Done()
			if connectAttempts >= 2 {
				cancel()
			}
			return nil, ctx.Err()
		},
		subscriptionReconnectBackoff: time.Millisecond,
	}

	done := make(chan struct{})
	go func() {
		service.runHeadSubscriptionLoop(ctx, wakeCh)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected subscription loop to stop after timeout and cancellation")
	}

	if connectAttempts < 2 {
		t.Fatalf("expected reconnect attempts after timeout, got %d", connectAttempts)
	}
}

func TestRunHeadSubscriptionLoopSkipsWhenContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service := &Service{
		cfg: Config{WSURL: "wss://ws.example"},
		newHeadSubscriptionFactory: func(context.Context, string) (headSubscription, error) {
			t.Fatal("factory should not be called after context cancellation")
			return nil, nil
		},
	}

	wakeCh := make(chan struct{}, 1)
	service.runHeadSubscriptionLoop(ctx, wakeCh)
}

func TestSubscriptionConnectTimeoutDefaultsWhenRequestTimeoutInvalid(t *testing.T) {
	t.Parallel()

	service := &Service{cfg: Config{RequestTimeout: 0}}
	if got := service.subscriptionConnectTimeout(); got != defaultRequestTimeout {
		t.Fatalf("expected default request timeout %s, got %s", defaultRequestTimeout, got)
	}
}

func TestSubscriptionConnectTimeoutUsesRequestTimeout(t *testing.T) {
	t.Parallel()

	service := &Service{cfg: Config{RequestTimeout: 7 * time.Second}}
	if got := service.subscriptionConnectTimeout(); got != 7*time.Second {
		t.Fatalf("expected request timeout %s, got %s", 7*time.Second, got)
	}
}

func TestOriginForWebSocketURL_DerivesHostBasedOrigin(t *testing.T) {
	t.Parallel()

	if got := originForWebSocketURL("wss://rpc.example/ws?key=secret"); got != "https://rpc.example" {
		t.Fatalf("expected https origin, got %q", got)
	}

	if got := originForWebSocketURL("ws://localhost:8546/path"); got != "http://localhost:8546" {
		t.Fatalf("expected http origin with port, got %q", got)
	}
}
