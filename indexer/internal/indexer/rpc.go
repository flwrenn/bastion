package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const jsonRPCVersion = "2.0"

var errRPCResponseTooLarge = errors.New("rpc response exceeds size limit")

type rpcClient struct {
	url              string
	maxResponseBytes int64
	httpClient       *http.Client
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

func newRPCClient(url string, maxResponseBytes int64) *rpcClient {
	return &rpcClient{
		url:              url,
		maxResponseBytes: maxResponseBytes,
		httpClient: &http.Client{
			Timeout: 0,
		},
	}
}

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
		return fmt.Errorf("rpc %s returned status %d: %s", method, httpResp.StatusCode, string(body))
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("decode rpc response %s: %w", method, err)
	}

	if rpcResp.Error != nil {
		return fmt.Errorf("rpc %s error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(rpcResp.Result, out); err != nil {
		return fmt.Errorf("decode rpc result %s: %w", method, err)
	}

	return nil
}

func isRPCResponseTooLarge(err error) bool {
	return errors.Is(err, errRPCResponseTooLarge)
}

func (c *rpcClient) latestBlockNumber(ctx context.Context) (uint64, error) {
	var raw string
	if err := c.call(ctx, "eth_blockNumber", []any{}, &raw); err != nil {
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
	if err := c.call(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}

	return logs, nil
}

func (c *rpcClient) getTransactionByHash(ctx context.Context, txHash string) (rpcTransaction, error) {
	var tx *rpcTransaction
	if err := c.call(ctx, "eth_getTransactionByHash", []any{txHash}, &tx); err != nil {
		return rpcTransaction{}, err
	}
	if tx == nil {
		return rpcTransaction{}, fmt.Errorf("transaction %s not found", txHash)
	}

	return *tx, nil
}

func (c *rpcClient) getBlockByNumber(ctx context.Context, number uint64) (rpcBlock, error) {
	var block *rpcBlock
	if err := c.call(ctx, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", number), false}, &block); err != nil {
		return rpcBlock{}, err
	}
	if block == nil {
		return rpcBlock{}, fmt.Errorf("block %d not found", number)
	}

	return *block, nil
}
