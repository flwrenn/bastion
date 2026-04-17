package api

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/flwrenn/bastion/indexer/internal/db"
)

// Handler serves the indexer REST API.
type Handler struct {
	store Store
}

// New creates an API handler backed by the given Store implementation.
func New(store Store) *Handler {
	return &Handler{store: store}
}

// Register mounts all API routes on the provided mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/operations/{hash}", h.GetOperation)
	mux.HandleFunc("GET /api/operations", h.ListOperations)
	mux.HandleFunc("GET /api/stats", h.GetStats)
}

// CORS wraps a handler with permissive CORS headers for frontend access.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- handlers ---

// ListOperations handles GET /api/operations.
func (h *Handler) ListOperations(w http.ResponseWriter, r *http.Request) {
	params := db.ListParams{
		Limit:  intQuery(r, "limit", 20),
		Offset: intQuery(r, "offset", 0),
	}
	db.ClampListParams(&params)

	if s := strings.TrimSpace(r.URL.Query().Get("sender")); s != "" {
		if len(s) != 42 { // "0x" + 40 hex chars = 20 bytes
			writeError(w, http.StatusBadRequest, "invalid sender address")
			return
		}
		b, err := decodeHexBytes(s)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid sender address")
			return
		}
		params.Sender = b
	}

	ops, total, err := h.store.ListOperations(r.Context(), params)
	if err != nil {
		slog.Error("list operations", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	data := make([]operationResponse, len(ops))
	for i := range ops {
		data[i] = toResponse(ops[i])
	}

	writeJSON(w, http.StatusOK, listResponse{
		Data:   data,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
	})
}

// GetOperation handles GET /api/operations/{hash}.
func (h *Handler) GetOperation(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.PathValue("hash"))
	if len(raw) != 66 { // "0x" + 64 hex chars = 32 bytes
		writeError(w, http.StatusBadRequest, "invalid userOpHash")
		return
	}
	hash, err := decodeHexBytes(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid userOpHash")
		return
	}

	op, err := h.store.GetOperationByHash(r.Context(), hash)
	if err != nil {
		slog.Error("get operation", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if op == nil {
		writeError(w, http.StatusNotFound, "operation not found")
		return
	}

	writeJSON(w, http.StatusOK, toResponse(*op))
}

// GetStats handles GET /api/stats.
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	s, err := h.store.GetStats(r.Context())
	if err != nil {
		slog.Error("get stats", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var successRate, sponsoredRate float64
	if s.TotalOps > 0 {
		successRate = float64(s.SuccessCount) / float64(s.TotalOps)
		sponsoredRate = float64(s.SponsoredCount) / float64(s.TotalOps)
	}

	writeJSON(w, http.StatusOK, statsResponse{
		TotalOps:              s.TotalOps,
		SuccessCount:          s.SuccessCount,
		SuccessRate:           successRate,
		SponsoredCount:        s.SponsoredCount,
		SponsoredRate:         sponsoredRate,
		UniqueSenders:         s.UniqueSenders,
		AccountsDeployedCount: s.AccountsDeployedCount,
	})
}

// --- response types ---

type operationResponse struct {
	UserOpHash      string `json:"userOpHash"`
	Sender          string `json:"sender"`
	Paymaster       string `json:"paymaster"`
	Target          string `json:"target,omitempty"`
	Calldata        string `json:"calldata,omitempty"`
	Nonce           string `json:"nonce"`
	Success         bool   `json:"success"`
	ActualGasCost   string `json:"actualGasCost"`
	ActualGasUsed   string `json:"actualGasUsed"`
	TxHash          string `json:"txHash"`
	BlockNumber     int64  `json:"blockNumber"`
	BlockTimestamp  int64  `json:"blockTimestamp"`
	LogIndex        int32  `json:"logIndex"`
	AccountDeployed bool   `json:"accountDeployed,omitempty"`
	RevertReason    string `json:"revertReason,omitempty"`
}

type listResponse struct {
	Data   []operationResponse `json:"data"`
	Total  int64               `json:"total"`
	Limit  int                 `json:"limit"`
	Offset int                 `json:"offset"`
}

type statsResponse struct {
	TotalOps              int64   `json:"totalOps"`
	SuccessCount          int64   `json:"successCount"`
	SuccessRate           float64 `json:"successRate"`
	SponsoredCount        int64   `json:"sponsoredCount"`
	SponsoredRate         float64 `json:"sponsoredRate"`
	UniqueSenders         int64   `json:"uniqueSenders"`
	AccountsDeployedCount int64   `json:"accountsDeployedCount"`
}

func toResponse(op db.UserOperation) operationResponse {
	r := operationResponse{
		UserOpHash:      encodeHex(op.UserOpHash),
		Sender:          encodeHex(op.Sender),
		Paymaster:       encodeHex(op.Paymaster),
		Target:          encodeHex(op.Target),
		Calldata:        encodeHex(op.Calldata),
		Nonce:           op.Nonce,
		Success:         op.Success,
		ActualGasCost:   op.ActualGasCost,
		ActualGasUsed:   op.ActualGasUsed,
		TxHash:          encodeHex(op.TxHash),
		BlockNumber:     op.BlockNumber,
		BlockTimestamp:  op.BlockTimestamp,
		LogIndex:        op.LogIndex,
		AccountDeployed: op.AccountDeployed,
	}
	// Only emit revertReason when the log carried non-empty data — empty
	// bytes from the chain are a legitimate "no reason supplied" and should
	// render as absent rather than "0x".
	if len(op.RevertReason) > 0 {
		r.RevertReason = encodeHex(op.RevertReason)
	}
	return r
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode json response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func encodeHex(b []byte) string {
	if b == nil {
		return ""
	}
	return "0x" + hex.EncodeToString(b)
}

func decodeHexBytes(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || (s[:2] != "0x" && s[:2] != "0X") {
		return nil, fmt.Errorf("missing 0x prefix")
	}
	h := s[2:]
	if len(h)%2 != 0 {
		return nil, fmt.Errorf("odd hex length")
	}
	return hex.DecodeString(h)
}

func intQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
