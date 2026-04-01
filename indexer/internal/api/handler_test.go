package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flwrenn/bastion/indexer/internal/db"
)

func TestEncodeHex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"nil returns empty", nil, ""},
		{"empty returns empty", []byte{}, ""},
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

func TestListOperationsClampedResponse(t *testing.T) {
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
