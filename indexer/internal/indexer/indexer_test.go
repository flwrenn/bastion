package indexer

import (
	"errors"
	"fmt"
	"testing"
)

func TestPlanScanRange_NoCursor_NoStartBlock(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{}}
	_, _, ok := svc.planScanRange(0, false, 100)
	if ok {
		t.Fatal("expected no scan range when start block is not configured")
	}
}

func TestPlanScanRange_NoCursor_WithStartBlock(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{HasStartBlock: true, StartBlock: 42}}
	from, to, ok := svc.planScanRange(0, false, 100)
	if !ok {
		t.Fatal("expected scan range to be available")
	}
	if from != 42 || to != 100 {
		t.Fatalf("expected range [42,100], got [%d,%d]", from, to)
	}
}

func TestPlanScanRange_WithCursorAndReorgWindow(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{ReorgWindow: 5}}
	from, to, ok := svc.planScanRange(20, true, 30)
	if !ok {
		t.Fatal("expected scan range to be available")
	}
	if from != 15 || to != 30 {
		t.Fatalf("expected range [15,30], got [%d,%d]", from, to)
	}
}

func TestPlanScanRange_CursorAheadOfSafeHeadReturnsNoRange(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{ReorgWindow: 3}}
	_, _, ok := svc.planScanRange(200, true, 50)
	if ok {
		t.Fatal("expected no scan range when cursor is ahead of safe head")
	}
}

func TestPlanScanRange_CursorAtSafeHeadReturnsNoRange(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{ReorgWindow: 3}}
	_, _, ok := svc.planScanRange(50, true, 50)
	if ok {
		t.Fatal("expected no scan range when cursor is at safe head")
	}
}

func TestPlanScanRange_StartBlockAboveSafeHead(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{HasStartBlock: true, StartBlock: 1000}}
	_, _, ok := svc.planScanRange(0, false, 100)
	if ok {
		t.Fatal("expected no scan range when start block > safe head")
	}
}

func TestRewindRangeToSafeHead(t *testing.T) {
	t.Parallel()

	from, to := rewindRangeToSafeHead(100, 20)
	if from != 80 || to != 100 {
		t.Fatalf("expected [80,100], got [%d,%d]", from, to)
	}

	from, to = rewindRangeToSafeHead(5, 20)
	if from != 0 || to != 5 {
		t.Fatalf("expected [0,5], got [%d,%d]", from, to)
	}
}

func TestValidateInitialBackfillConfig_RequiresStartBlockWithoutCursor(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{HasStartBlock: false}}
	err := svc.validateInitialBackfillConfig(false)
	if err == nil {
		t.Fatal("expected validation error when cursor and start block are missing")
	}
	if !errors.Is(err, errInitialBackfillStartBlockRequired) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestValidateInitialBackfillConfig_AllowsMissingStartBlockWithCursor(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{HasStartBlock: false}}
	err := svc.validateInitialBackfillConfig(true)
	if err != nil {
		t.Fatalf("expected no error when cursor exists, got %v", err)
	}
}

func TestValidateInitialBackfillConfig_AllowsStartBlockWithoutCursor(t *testing.T) {
	t.Parallel()

	svc := Service{cfg: Config{HasStartBlock: true, StartBlock: 123}}
	err := svc.validateInitialBackfillConfig(false)
	if err != nil {
		t.Fatalf("expected no error when start block is configured, got %v", err)
	}
}

func TestIsFatalIndexIterationError(t *testing.T) {
	t.Parallel()

	if !isFatalIndexIterationError(errInitialBackfillStartBlockRequired) {
		t.Fatal("expected sentinel to be fatal")
	}

	wrapped := fmt.Errorf("wrapped: %w", errInitialBackfillStartBlockRequired)
	if !isFatalIndexIterationError(wrapped) {
		t.Fatal("expected wrapped sentinel to be fatal")
	}

	if isFatalIndexIterationError(errors.New("other error")) {
		t.Fatal("expected non-sentinel error to be non-fatal")
	}
}

func TestWebsocketLogEndpoint(t *testing.T) {
	t.Parallel()

	if got := websocketLogEndpoint("wss://rpc.example/ws?key=secret"); got != "wss://rpc.example" {
		t.Fatalf("expected redacted endpoint, got %q", got)
	}

	if got := websocketLogEndpoint("ws://localhost:8546/path"); got != "ws://localhost:8546" {
		t.Fatalf("expected endpoint with host+port, got %q", got)
	}

	if got := websocketLogEndpoint("bad://"); got != "invalid" {
		t.Fatalf("expected invalid endpoint marker, got %q", got)
	}
}

func TestRedactWebSocketURLError(t *testing.T) {
	t.Parallel()

	wsURL := "wss://rpc.example/ws?key=secret"
	err := fmt.Errorf("dial failed for %s", wsURL)
	redacted := redactWebSocketURLError(err, wsURL)
	if redacted == nil {
		t.Fatal("expected redacted error")
	}
	if !errors.Is(redacted, err) {
		t.Fatal("expected redacted error to preserve original cause")
	}

	if redacted.Error() == err.Error() {
		t.Fatal("expected websocket url to be redacted")
	}
	if redacted.Error() != "dial failed for wss://rpc.example" {
		t.Fatalf("unexpected redacted error: %q", redacted.Error())
	}
}

func TestRedactWebSocketURLError_ReturnsOriginalWhenNoURL(t *testing.T) {
	t.Parallel()

	err := errors.New("dial failed")
	redacted := redactWebSocketURLError(err, "")
	if !errors.Is(redacted, err) {
		t.Fatal("expected original error when ws url is empty")
	}
}
