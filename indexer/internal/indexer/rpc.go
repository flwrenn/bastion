package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const jsonRPCVersion = "2.0"

var errRPCResponseTooLarge = errors.New("rpc response exceeds size limit")

// retryConfig controls exponential backoff for RPC calls.
type retryConfig struct {
	MaxAttempts    int
	BaseDelay      time.Duration
	MaxDelay       time.Duration
	RequestTimeout time.Duration
}

type rpcClient struct {
	url              string
	maxResponseBytes int64
	httpClient       *http.Client
	retry            retryConfig
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcLog struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
	LogIndex        string   `json:"logIndex"`
	Removed         bool     `json:"removed"`
}

type rpcTransaction struct {
	Hash  string `json:"hash"`
	Input string `json:"input"`
}

type rpcBlock struct {
	Number    string `json:"number"`
	Timestamp string `json:"timestamp"`
}

// --- Typed errors for retry decisions ---

// httpStatusError wraps an HTTP-level non-2xx response.
type httpStatusError struct {
	method     string
	statusCode int
	retryAfter time.Duration
	body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("rpc %s: HTTP %d: %s", e.method, e.statusCode, e.body)
}

// jsonRPCCallError wraps a JSON-RPC level error (error field in response).
type jsonRPCCallError struct {
	method  string
	code    int
	message string
}

func (e *jsonRPCCallError) Error() string {
	return fmt.Sprintf("rpc %s: error %d: %s", e.method, e.code, e.message)
}

// --- Client ---

func newRPCClient(url string, maxResponseBytes int64, retry retryConfig) *rpcClient {
	return &rpcClient{
		url:              url,
		maxResponseBytes: maxResponseBytes,
		httpClient:       &http.Client{Timeout: 0},
		retry:            retry,
	}
}

// call performs a single JSON-RPC request with no retry.
func (c *rpcClient) call(ctx context.Context, method string, params any, out any) error {
	reqBody := rpcRequest{
		JSONRPC: jsonRPCVersion,
		ID:      1,
		Method:  method,
		Params:  params,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal rpc request %s: %w", method, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create rpc request %s: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send rpc request %s: %w", method, err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, c.maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read rpc response %s: %w", method, err)
	}
	if int64(len(body)) > c.maxResponseBytes {
		return fmt.Errorf("%w: rpc %s response exceeds %d bytes", errRPCResponseTooLarge, method, c.maxResponseBytes)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return &httpStatusError{
			method:     method,
			statusCode: httpResp.StatusCode,
			retryAfter: parseRetryAfter(httpResp.Header.Get("Retry-After")),
			body:       string(body),
		}
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("decode rpc response %s: %w", method, err)
	}

	if rpcResp.Error != nil {
		return &jsonRPCCallError{
			method:  method,
			code:    rpcResp.Error.Code,
			message: rpcResp.Error.Message,
		}
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(rpcResp.Result, out); err != nil {
		return fmt.Errorf("decode rpc result %s: %w", method, err)
	}

	return nil
}

// callWithRetry wraps call with exponential backoff and rate limit awareness.
// Each attempt gets its own per-request timeout. The parent ctx is used only
// for cancellation (e.g. shutdown).
func (c *rpcClient) callWithRetry(ctx context.Context, method string, params any, out any) error {
	var lastErr error

	for attempt := range c.retry.MaxAttempts {
		reqCtx, cancel := context.WithTimeout(ctx, c.retry.RequestTimeout)
		err := c.call(reqCtx, method, params, out)
		cancel()

		if err == nil {
			return nil
		}
		lastErr = err

		// Parent context cancelled (shutdown) — stop immediately.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Response too large is structural, not transient.
		if isRPCResponseTooLarge(err) {
			return err
		}

		if !isRetryableError(err) {
			return err
		}

		// Last attempt — return the error without sleeping.
		if attempt == c.retry.MaxAttempts-1 {
			break
		}

		delay := c.backoffDelay(attempt, err)
		slog.Warn("rpc call failed, retrying",
			"method", method,
			"attempt", attempt+1,
			"max_attempts", c.retry.MaxAttempts,
			"delay", delay,
			"err", err,
		)

		if !sleepContext(ctx, delay) {
			return ctx.Err()
		}
	}

	return fmt.Errorf("rpc %s: failed after %d attempts: %w", method, c.retry.MaxAttempts, lastErr)
}

// isRetryableError returns true for transient errors worth retrying.
func isRetryableError(err error) bool {
	var httpErr *httpStatusError
	if errors.As(err, &httpErr) {
		return httpErr.statusCode == 429 || httpErr.statusCode >= 500
	}

	var jsonErr *jsonRPCCallError
	if errors.As(err, &jsonErr) {
		// Known rate limit error codes from major providers (Alchemy, QuickNode).
		return jsonErr.code == -32005 || jsonErr.code == -32097
	}

	// Network errors (connection refused, DNS failure, request timeout) are retryable.
	return true
}

// backoffDelay computes the delay before the next retry attempt.
// Respects Retry-After from HTTP 429 responses; otherwise uses exponential backoff.
func (c *rpcClient) backoffDelay(attempt int, err error) time.Duration {
	var httpErr *httpStatusError
	if errors.As(err, &httpErr) && httpErr.retryAfter > 0 {
		if httpErr.retryAfter > c.retry.MaxDelay {
			return c.retry.MaxDelay
		}
		return httpErr.retryAfter
	}

	delay := c.retry.BaseDelay * time.Duration(1<<attempt)
	if delay > c.retry.MaxDelay {
		delay = c.retry.MaxDelay
	}
	return delay
}

// parseRetryAfter extracts a duration from the Retry-After HTTP header.
// Supports both delay-seconds and HTTP-date formats.
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		if delay := time.Until(t); delay > 0 {
			return delay
		}
	}
	return 0
}

func isRPCResponseTooLarge(err error) bool {
	return errors.Is(err, errRPCResponseTooLarge)
}

// --- Convenience wrappers (all use callWithRetry) ---

func (c *rpcClient) latestBlockNumber(ctx context.Context) (uint64, error) {
	var raw string
	if err := c.callWithRetry(ctx, "eth_blockNumber", []any{}, &raw); err != nil {
		return 0, err
	}

	value, err := parseHexUint64(raw)
	if err != nil {
		return 0, fmt.Errorf("parse block number: %w", err)
	}

	return value, nil
}

func (c *rpcClient) getLogs(ctx context.Context, address string, topic0 string, fromBlock uint64, toBlock uint64) ([]rpcLog, error) {
	filter := map[string]any{
		"address":   address,
		"fromBlock": fmt.Sprintf("0x%x", fromBlock),
		"toBlock":   fmt.Sprintf("0x%x", toBlock),
		"topics":    []any{topic0},
	}

	var logs []rpcLog
	if err := c.callWithRetry(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}

	return logs, nil
}

func (c *rpcClient) getTransactionByHash(ctx context.Context, txHash string) (rpcTransaction, error) {
	var tx *rpcTransaction
	if err := c.callWithRetry(ctx, "eth_getTransactionByHash", []any{txHash}, &tx); err != nil {
		return rpcTransaction{}, err
	}
	if tx == nil {
		return rpcTransaction{}, fmt.Errorf("transaction %s not found", txHash)
	}

	return *tx, nil
}

func (c *rpcClient) getBlockByNumber(ctx context.Context, number uint64) (rpcBlock, error) {
	var block *rpcBlock
	if err := c.callWithRetry(ctx, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", number), false}, &block); err != nil {
		return rpcBlock{}, err
	}
	if block == nil {
		return rpcBlock{}, fmt.Errorf("block %d not found", number)
	}

	return *block, nil
}
