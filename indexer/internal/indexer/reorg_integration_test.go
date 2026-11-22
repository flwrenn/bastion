package indexer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flwrenn/bastion/indexer/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIndexOnceReplacesReorgedBlocksIntegration(t *testing.T) {
	pool := reorgTestPool(t)
	ctx := context.Background()

	// Head 107 with 2 confirmations: the first pass indexes [100,105].
	chainA := stubChain{
		head: 107,
		logs: []rpcLog{
			userOperationLog("op-settled", "tx-a1", 101, 0, true),
			userOperationLog("op-moved", "tx-a3", 103, 0, true),
			accountDeployedLog("op-dropped", "tx-a4", 104, 0),
			revertReasonLog("op-dropped", "tx-a4", 104, 1),
			userOperationLog("op-dropped", "tx-a4", 104, 2, false),
		},
	}
	// Forks from chain A after block 102. Head 109: with the cursor at 105 and
	// a reorg window of 3, the second pass rescans [102,107]. op-moved lands
	// in a different transaction one block later, op-dropped never lands, and
	// op-new and op-late are new.
	chainB := stubChain{
		head: 109,
		logs: []rpcLog{
			userOperationLog("op-settled", "tx-a1", 101, 0, true),
			userOperationLog("op-moved", "tx-b4", 104, 0, true),
			accountDeployedLog("op-new", "tx-b5", 105, 0),
			userOperationLog("op-new", "tx-b5", 105, 1, true),
			revertReasonLog("op-late", "tx-b6", 106, 0),
			userOperationLog("op-late", "tx-b6", 106, 1, false),
		},
	}

	node := &stubNode{chain: chainA}
	server := httptest.NewServer(node)
	defer server.Close()

	service, err := New(Config{
		RPCURL:              server.URL,
		EntryPoint:          defaultEntryPoint,
		StartBlock:          100,
		HasStartBlock:       true,
		BatchSize:           defaultBatchSize,
		Confirmations:       2,
		ReorgWindow:         3,
		PollInterval:        time.Second,
		RequestTimeout:      5 * time.Second,
		RPCConcurrency:      2,
		RPCResponseMaxBytes: 1 << 20,
		RPCMaxRetries:       1,
		RPCRetryBaseDelay:   time.Millisecond,
		RPCRetryMaxDelay:    time.Millisecond,
		StateKey:            stateKeyLastIndexedBlock,
	}, pool)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := service.indexOnce(ctx); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	assertIndexed(t, pool, 105, map[string][]string{
		"user_operations": {
			"block=101 log=0 tx=tx-a1 op=op-settled",
			"block=103 log=0 tx=tx-a3 op=op-moved",
			"block=104 log=2 tx=tx-a4 op=op-dropped",
		},
		"account_deployments":    {"block=104 log=0 tx=tx-a4 op=op-dropped"},
		"user_operation_reverts": {"block=104 log=1 tx=tx-a4 op=op-dropped"},
	})
	if t.Failed() {
		t.Fatal("first pass did not store chain A; the reorg pass has nothing to replace")
	}

	node.reorgTo(chainB)

	if err := service.indexOnce(ctx); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	assertIndexed(t, pool, 107, map[string][]string{
		"user_operations": {
			"block=101 log=0 tx=tx-a1 op=op-settled",
			"block=104 log=0 tx=tx-b4 op=op-moved",
			"block=105 log=1 tx=tx-b5 op=op-new",
			"block=106 log=1 tx=tx-b6 op=op-late",
		},
		"account_deployments":    {"block=105 log=0 tx=tx-b5 op=op-new"},
		"user_operation_reverts": {"block=106 log=0 tx=tx-b6 op=op-late"},
	})
	if duplicated := duplicatedUserOps(t, pool); len(duplicated) > 0 {
		t.Errorf("user_operations holds more than one row for: %s", strings.Join(duplicated, ", "))
	}
}

// stubChain is the canonical chain a stubNode serves at one point in time.
type stubChain struct {
	head uint64
	logs []rpcLog
}

// stubNode answers the three JSON-RPC methods an indexing pass uses when
// transaction enrichment is off.
type stubNode struct {
	mu    sync.Mutex
	chain stubChain
}

func (n *stubNode) reorgTo(chain stubChain) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.chain = chain
}

func (n *stubNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	n.mu.Lock()
	chain := n.chain
	n.mu.Unlock()

	result, err := chain.answer(req.Method, req.Params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": jsonRPCVersion, "id": 1, "result": result})
}

func (c stubChain) answer(method string, params []json.RawMessage) (any, error) {
	if len(params) == 0 && method != "eth_blockNumber" {
		return nil, fmt.Errorf("%s: missing params", method)
	}

	switch method {
	case "eth_blockNumber":
		return fmt.Sprintf("0x%x", c.head), nil

	case "eth_getLogs":
		var filter struct {
			FromBlock string `json:"fromBlock"`
			ToBlock   string `json:"toBlock"`
		}
		if err := json.Unmarshal(params[0], &filter); err != nil {
			return nil, fmt.Errorf("eth_getLogs filter: %w", err)
		}
		from, err := parseHexUint64(filter.FromBlock)
		if err != nil {
			return nil, fmt.Errorf("eth_getLogs fromBlock: %w", err)
		}
		to, err := parseHexUint64(filter.ToBlock)
		if err != nil {
			return nil, fmt.Errorf("eth_getLogs toBlock: %w", err)
		}

		logs := []rpcLog{}
		for _, log := range c.logs {
			block, err := parseHexUint64(log.BlockNumber)
			if err != nil {
				return nil, fmt.Errorf("fixture log block number: %w", err)
			}
			if block >= from && block <= to {
				logs = append(logs, log)
			}
		}
		return logs, nil

	case "eth_getBlockByNumber":
		var number string
		if err := json.Unmarshal(params[0], &number); err != nil {
			return nil, fmt.Errorf("eth_getBlockByNumber number: %w", err)
		}
		block, err := parseHexUint64(number)
		if err != nil {
			return nil, fmt.Errorf("eth_getBlockByNumber number: %w", err)
		}
		// Both chains report the same timestamp at a given height. The
		// block-timestamp cache is keyed by height and is not invalidated on
		// reorg (#66), so this test does not assert block_timestamp.
		return rpcBlock{Number: number, Timestamp: fmt.Sprintf("0x%x", 1_700_000_000+12*block)}, nil

	default:
		return nil, fmt.Errorf("stub node does not serve %s", method)
	}
}

const reorgSenderTopic = "0x0000000000000000000000001111111111111111111111111111111111111111"

func userOperationLog(op, tx string, block, logIndex uint64, success bool) rpcLog {
	successWord := uint64(0)
	if success {
		successWord = 1
	}

	return rpcLog{
		Address:         defaultEntryPoint,
		Topics:          []string{userOperationEventTopic, labelHash(op), reorgSenderTopic, "0x" + hexWord(0)},
		Data:            "0x" + hexWord(0) + hexWord(successWord) + hexWord(21000) + hexWord(21000),
		TransactionHash: labelHash(tx),
		BlockNumber:     fmt.Sprintf("0x%x", block),
		LogIndex:        fmt.Sprintf("0x%x", logIndex),
	}
}

func accountDeployedLog(op, tx string, block, logIndex uint64) rpcLog {
	return rpcLog{
		Address:         defaultEntryPoint,
		Topics:          []string{accountDeployedTopic, labelHash(op), reorgSenderTopic},
		Data:            "0x" + hexWord(0xfa) + hexWord(0),
		TransactionHash: labelHash(tx),
		BlockNumber:     fmt.Sprintf("0x%x", block),
		LogIndex:        fmt.Sprintf("0x%x", logIndex),
	}
}

func revertReasonLog(op, tx string, block, logIndex uint64) rpcLog {
	return rpcLog{
		Address:         defaultEntryPoint,
		Topics:          []string{userOperationRevertReasonTopic, labelHash(op), reorgSenderTopic},
		Data:            "0x" + hexWord(0) + hexWord(64) + hex.EncodeToString(encodeBytes([]byte("boom"))),
		TransactionHash: labelHash(tx),
		BlockNumber:     fmt.Sprintf("0x%x", block),
		LogIndex:        fmt.Sprintf("0x%x", logIndex),
	}
}

// labelHash encodes a fixture name as a 32-byte hash, so stored rows can be
// printed back by name.
func labelHash(label string) string {
	hash := make([]byte, 32)
	copy(hash, label)
	return "0x" + hex.EncodeToString(hash)
}

func assertIndexed(t *testing.T, pool *pgxpool.Pool, wantCursor uint64, want map[string][]string) {
	t.Helper()

	for _, table := range []string{"user_operations", "account_deployments", "user_operation_reverts"} {
		got := storedRows(t, pool, table)
		if !slices.Equal(got, want[table]) {
			t.Errorf(
				"%s rows:\n  got:\n    %s\n  want:\n    %s",
				table,
				strings.Join(got, "\n    "),
				strings.Join(want[table], "\n    "),
			)
		}
	}

	cursor, ok, err := db.GetStateUint64(context.Background(), pool, stateKeyLastIndexedBlock)
	if err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if !ok || cursor != wantCursor {
		t.Errorf("cursor = %d (set: %v), want %d", cursor, ok, wantCursor)
	}
}

func storedRows(t *testing.T, pool *pgxpool.Pool, table string) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(), fmt.Sprintf(
		"SELECT block_number, log_index, tx_hash, user_op_hash FROM %s ORDER BY block_number, log_index, tx_hash",
		table,
	))
	if err != nil {
		t.Fatalf("query %s: %v", table, err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var (
			block      int64
			logIndex   int32
			txHash     []byte
			userOpHash []byte
		)
		if err := rows.Scan(&block, &logIndex, &txHash, &userOpHash); err != nil {
			t.Fatalf("scan %s row: %v", table, err)
		}
		lines = append(lines, fmt.Sprintf(
			"block=%d log=%d tx=%s op=%s",
			block, logIndex, hashLabel(txHash), hashLabel(userOpHash),
		))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s rows: %v", table, err)
	}

	return lines
}

func duplicatedUserOps(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT user_op_hash
		FROM user_operations
		GROUP BY user_op_hash
		HAVING count(*) > 1
		ORDER BY user_op_hash
	`)
	if err != nil {
		t.Fatalf("query duplicated user operations: %v", err)
	}
	defer rows.Close()

	var labels []string
	for rows.Next() {
		var userOpHash []byte
		if err := rows.Scan(&userOpHash); err != nil {
			t.Fatalf("scan duplicated user operation: %v", err)
		}
		labels = append(labels, hashLabel(userOpHash))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read duplicated user operations: %v", err)
	}

	return labels
}

func hashLabel(hash []byte) string {
	return strings.TrimRight(string(hash), "\x00")
}

// reorgTestPool mirrors testPool in internal/db: a migrated schema of its own
// that is dropped on cleanup. That helper is unexported test code, and a
// shared helper package would import db, which db's own tests could then not
// import. The schema prefix differs because `go test ./...` runs both
// packages against the same database at once.
func reorgTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	schema := fmt.Sprintf("test_indexer_%d", time.Now().UnixNano())

	bootstrap, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("bootstrap connection: %v", err)
	}
	_, err = bootstrap.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %q", schema))
	bootstrap.Close()
	if err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %q CASCADE", schema))
	})

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create pool with test schema: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return pool
}
