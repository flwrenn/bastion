package indexer

import "testing"

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
