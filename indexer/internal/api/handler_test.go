package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flwrenn/bastion/indexer/internal/db"
)

const floatEps = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < floatEps
}

func TestEncodeHex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"nil returns empty", nil, ""},
		{"empty returns 0x", []byte{}, "0x"},
		{"20-byte address", make([]byte, 20), "0x0000000000000000000000000000000000000000"},
		{"short bytes", []byte{0xca, 0xfe}, "0xcafe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := encodeHex(tt.in); got != tt.want {
				t.Fatalf("encodeHex(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecodeHexBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantLen int
		wantErr bool
	}{
		{"valid 20-byte address", "0x0000000000000000000000000000000000000001", 20, false},
		{"valid 32-byte hash", "0x" + "ab" + "00000000000000000000000000000000000000000000000000000000000000", 32, false},
		{"uppercase prefix", "0X00ff", 2, false},
		{"missing prefix", "abcd", 0, true},
		{"odd length", "0xabc", 0, true},
		{"invalid hex char", "0xzzzz", 0, true},
		{"empty after prefix", "0x", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := decodeHexBytes(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.in, err)
			}
			if len(b) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(b), tt.wantLen)
			}
		})
	}
}

func TestIntQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		key  string
		def  int
		want int
	}{
		{"missing key uses default", "/path", "limit", 20, 20},
		{"valid int", "/path?limit=50", "limit", 20, 50},
		{"non-numeric uses default", "/path?limit=abc", "limit", 20, 20},
		{"empty value uses default", "/path?limit=", "limit", 20, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, tt.url, nil)
			if got := intQuery(r, tt.key, tt.def); got != tt.want {
				t.Fatalf("intQuery = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCORSHeaders(t *testing.T) {
	t.Parallel()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CORS(inner)

	t.Run("GET request includes CORS headers", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Fatalf("Allow-Origin = %q, want *", got)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("OPTIONS preflight returns 204", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodOptions, "/api/operations", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
		}
		if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, OPTIONS" {
			t.Fatalf("Allow-Methods = %q, want %q", got, "GET, OPTIONS")
		}
	})
}

func TestListOperationsBadSender(t *testing.T) {
	t.Parallel()

	h := New(nil)
	mux := http.NewServeMux()
	h.Register(mux)

	r := httptest.NewRequest(http.MethodGet, "/api/operations?sender=not-hex", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid sender address" {
		t.Fatalf("error = %q, want %q", body["error"], "invalid sender address")
	}
}

func TestListOperationsBadSenderLength(t *testing.T) {
	t.Parallel()

	h := New(nil)
	mux := http.NewServeMux()
	h.Register(mux)

	r := httptest.NewRequest(http.MethodGet, "/api/operations?sender=0xdead", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetOperationBadHash(t *testing.T) {
	t.Parallel()

	h := New(nil)
	mux := http.NewServeMux()
	h.Register(mux)

	r := httptest.NewRequest(http.MethodGet, "/api/operations/not-a-hash", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid userOpHash" {
		t.Fatalf("error = %q, want %q", body["error"], "invalid userOpHash")
	}
}

func TestGetOperationShortHash(t *testing.T) {
	t.Parallel()

	h := New(nil)
	mux := http.NewServeMux()
	h.Register(mux)

	r := httptest.NewRequest(http.MethodGet, "/api/operations/0xdeadbeef", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestClampedParamsFromQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
	}{
		{"over-max limit clamped to 100", "?limit=200", 100, 0},
		{"zero limit defaults to 20", "?limit=0", 20, 0},
		{"negative offset clamped to 0", "?offset=-5", 20, 0},
		{"over-max offset clamped to 10000", "?offset=20000", 20, 10000},
		{"combined out-of-range", "?limit=999&offset=-1", 100, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := db.ListParams{
				Limit:  intQuery(httptest.NewRequest(http.MethodGet, "/api/operations"+tt.query, nil), "limit", 20),
				Offset: intQuery(httptest.NewRequest(http.MethodGet, "/api/operations"+tt.query, nil), "offset", 0),
			}
			db.ClampListParams(&p)
			if p.Limit != tt.wantLimit {
				t.Fatalf("limit: got %d, want %d", p.Limit, tt.wantLimit)
			}
			if p.Offset != tt.wantOffset {
				t.Fatalf("offset: got %d, want %d", p.Offset, tt.wantOffset)
			}
		})
	}
}

func TestToResponse(t *testing.T) {
	t.Parallel()

	resp := toResponse(testOp())

	if resp.Sender != "0x"+testAddr {
		t.Fatalf("sender = %q, want 0x%s", resp.Sender, testAddr)
	}
	if resp.UserOpHash != "0x"+testHash {
		t.Fatalf("userOpHash = %q", resp.UserOpHash)
	}
	if resp.Nonce != "42" {
		t.Fatalf("nonce = %q, want 42", resp.Nonce)
	}
	if !resp.Success {
		t.Fatal("expected success = true")
	}
	if resp.BlockNumber != 100 {
		t.Fatalf("blockNumber = %d, want 100", resp.BlockNumber)
	}
}

const (
	testAddr = "0000000000000000000000000000000000000001"
	testHash = "0000000000000000000000000000000000000000000000000000000000000001"
)

func testOp() db.UserOperation {
	sender := make([]byte, 20)
	sender[19] = 1
	hash := make([]byte, 32)
	hash[31] = 1
	return db.UserOperation{
		ID:             1,
		UserOpHash:     hash,
		Sender:         sender,
		Paymaster:      make([]byte, 20),
		Nonce:          "42",
		Success:        true,
		ActualGasCost:  "1000",
		ActualGasUsed:  "500",
		TxHash:         make([]byte, 32),
		BlockNumber:    100,
		BlockTimestamp: 1700000000,
		LogIndex:       0,
	}
}

// --- stub Store for happy-path tests ---

// stubStore implements Store with canned return values.
type stubStore struct {
	listOps   []db.UserOperation
	listTotal int64
	listErr   error

	getOp  *db.UserOperation
	getErr error

	stats    db.Stats
	statsErr error
}

func (s *stubStore) ListOperations(_ context.Context, _ db.ListParams) ([]db.UserOperation, int64, error) {
	return s.listOps, s.listTotal, s.listErr
}

func (s *stubStore) GetOperationByHash(_ context.Context, _ []byte) (*db.UserOperation, error) {
	return s.getOp, s.getErr
}

func (s *stubStore) GetStats(_ context.Context) (db.Stats, error) {
	return s.stats, s.statsErr
}

// newTestMux creates a Handler backed by the given Store and returns
// the ServeMux with routes registered — ready for httptest.
func newTestMux(store Store) *http.ServeMux {
	mux := http.NewServeMux()
	New(store).Register(mux)
	return mux
}

// --- happy-path handler tests ---

func TestListOperationsHappyPath(t *testing.T) {
	t.Parallel()

	op := testOp()
	store := &stubStore{listOps: []db.UserOperation{op}, listTotal: 1}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet, "/api/operations", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body listResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Total != 1 {
		t.Fatalf("total = %d, want 1", body.Total)
	}
	if body.Limit != 20 {
		t.Fatalf("limit = %d, want 20", body.Limit)
	}
	if body.Offset != 0 {
		t.Fatalf("offset = %d, want 0", body.Offset)
	}
	if len(body.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(body.Data))
	}

	got := body.Data[0]
	if got.UserOpHash != "0x"+testHash {
		t.Fatalf("userOpHash = %q, want 0x%s", got.UserOpHash, testHash)
	}
	if got.Sender != "0x"+testAddr {
		t.Fatalf("sender = %q, want 0x%s", got.Sender, testAddr)
	}
	if got.Nonce != "42" {
		t.Fatalf("nonce = %q, want 42", got.Nonce)
	}
	if !got.Success {
		t.Fatal("expected success = true")
	}
	if got.ActualGasCost != "1000" {
		t.Fatalf("actualGasCost = %q, want 1000", got.ActualGasCost)
	}
	if got.ActualGasUsed != "500" {
		t.Fatalf("actualGasUsed = %q, want 500", got.ActualGasUsed)
	}
	if got.BlockNumber != 100 {
		t.Fatalf("blockNumber = %d, want 100", got.BlockNumber)
	}
	if got.BlockTimestamp != 1700000000 {
		t.Fatalf("blockTimestamp = %d, want 1700000000", got.BlockTimestamp)
	}
}

func TestListOperationsEmpty(t *testing.T) {
	t.Parallel()

	store := &stubStore{listOps: nil, listTotal: 0}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet, "/api/operations", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body listResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Total != 0 {
		t.Fatalf("total = %d, want 0", body.Total)
	}
	if body.Data == nil {
		t.Fatal("data should be empty slice, not nil")
	}
	if len(body.Data) != 0 {
		t.Fatalf("len(data) = %d, want 0", len(body.Data))
	}
}

func TestListOperationsCustomPagination(t *testing.T) {
	t.Parallel()

	store := &stubStore{listOps: []db.UserOperation{testOp()}, listTotal: 50}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet, "/api/operations?limit=10&offset=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body listResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Limit != 10 {
		t.Fatalf("limit = %d, want 10", body.Limit)
	}
	if body.Offset != 5 {
		t.Fatalf("offset = %d, want 5", body.Offset)
	}
	if body.Total != 50 {
		t.Fatalf("total = %d, want 50", body.Total)
	}
}

func TestListOperationsStoreError(t *testing.T) {
	t.Parallel()

	store := &stubStore{listErr: errors.New("db down")}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet, "/api/operations", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "internal error" {
		t.Fatalf("error = %q, want %q", body["error"], "internal error")
	}
}

func TestGetOperationHappyPath(t *testing.T) {
	t.Parallel()

	op := testOp()
	store := &stubStore{getOp: &op}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet,
		"/api/operations/0x"+testHash, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var got operationResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.UserOpHash != "0x"+testHash {
		t.Fatalf("userOpHash = %q, want 0x%s", got.UserOpHash, testHash)
	}
	if got.Sender != "0x"+testAddr {
		t.Fatalf("sender = %q, want 0x%s", got.Sender, testAddr)
	}
	if got.Nonce != "42" {
		t.Fatalf("nonce = %q, want 42", got.Nonce)
	}
	if !got.Success {
		t.Fatal("expected success = true")
	}
	if got.BlockNumber != 100 {
		t.Fatalf("blockNumber = %d, want 100", got.BlockNumber)
	}
}

func TestGetOperationNotFound(t *testing.T) {
	t.Parallel()

	store := &stubStore{getOp: nil}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet,
		"/api/operations/0x"+testHash, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "operation not found" {
		t.Fatalf("error = %q, want %q", body["error"], "operation not found")
	}
}

func TestGetOperationStoreError(t *testing.T) {
	t.Parallel()

	store := &stubStore{getErr: errors.New("db down")}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet,
		"/api/operations/0x"+testHash, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestGetStatsHappyPath(t *testing.T) {
	t.Parallel()

	store := &stubStore{
		stats: db.Stats{
			TotalOps:       100,
			SuccessCount:   75,
			SponsoredCount: 40,
			UniqueSenders:  10,
		},
	}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var got statsResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.TotalOps != 100 {
		t.Fatalf("totalOps = %d, want 100", got.TotalOps)
	}
	if got.SuccessCount != 75 {
		t.Fatalf("successCount = %d, want 75", got.SuccessCount)
	}
	if got.SponsoredCount != 40 {
		t.Fatalf("sponsoredCount = %d, want 40", got.SponsoredCount)
	}
	if got.UniqueSenders != 10 {
		t.Fatalf("uniqueSenders = %d, want 10", got.UniqueSenders)
	}
	if !approxEqual(got.SuccessRate, 0.75) {
		t.Fatalf("successRate = %f, want ~0.75", got.SuccessRate)
	}
	if !approxEqual(got.SponsoredRate, 0.4) {
		t.Fatalf("sponsoredRate = %f, want ~0.4", got.SponsoredRate)
	}
}

func TestGetStatsZeroOps(t *testing.T) {
	t.Parallel()

	store := &stubStore{stats: db.Stats{}}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got statsResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.TotalOps != 0 {
		t.Fatalf("totalOps = %d, want 0", got.TotalOps)
	}
	if !approxEqual(got.SuccessRate, 0) {
		t.Fatalf("successRate = %f, want 0 (avoid division by zero)", got.SuccessRate)
	}
	if got.SponsoredCount != 0 {
		t.Fatalf("sponsoredCount = %d, want 0", got.SponsoredCount)
	}
	if !approxEqual(got.SponsoredRate, 0) {
		t.Fatalf("sponsoredRate = %f, want 0 (avoid division by zero)", got.SponsoredRate)
	}
}

func TestGetStatsStoreError(t *testing.T) {
	t.Parallel()

	store := &stubStore{statsErr: errors.New("db down")}
	mux := newTestMux(store)

	r := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestToResponseIncludesEnrichmentFields(t *testing.T) {
	t.Parallel()

	op := db.UserOperation{
		UserOpHash:      make([]byte, 32),
		Sender:          make([]byte, 20),
		Paymaster:       make([]byte, 20),
		TxHash:          make([]byte, 32),
		Nonce:           "0",
		ActualGasCost:   "0",
		ActualGasUsed:   "0",
		AccountDeployed: true,
		RevertReason:    []byte("boom"),
	}

	got := toResponse(op)
	if !got.AccountDeployed {
		t.Fatal("AccountDeployed not propagated")
	}
	if got.RevertReason != "0x626f6f6d" {
		t.Fatalf("RevertReason = %q, want 0x626f6f6d", got.RevertReason)
	}
}

func TestToResponseOmitsEmptyEnrichmentFields(t *testing.T) {
	t.Parallel()

	op := db.UserOperation{
		UserOpHash:    make([]byte, 32),
		Sender:        make([]byte, 20),
		Paymaster:     make([]byte, 20),
		TxHash:        make([]byte, 32),
		Nonce:         "0",
		ActualGasCost: "0",
		ActualGasUsed: "0",
	}

	got := toResponse(op)
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), "accountDeployed") {
		t.Errorf("accountDeployed should be omitted when false; got %s", payload)
	}
	if strings.Contains(string(payload), "revertReason") {
		t.Errorf("revertReason should be omitted when empty; got %s", payload)
	}
}
