package indexer

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeAddress(t *testing.T) {
	t.Parallel()

	value, err := normalizeAddress("0x0000000071727De22E5E9d8BAf0edAc6f37da032")
	if err != nil {
		t.Fatalf("normalizeAddress returned error: %v", err)
	}

	const expected = "0x0000000071727de22e5e9d8baf0edac6f37da032"
	if value != expected {
		t.Fatalf("expected %q, got %q", expected, value)
	}
}

func TestNormalizeAddressRejectsInvalidLength(t *testing.T) {
	t.Parallel()

	if _, err := normalizeAddress("0x1234"); err == nil {
		t.Fatal("expected error for invalid address length")
	}
}

func TestNormalizeAddressAcceptsUppercasePrefix(t *testing.T) {
	t.Parallel()

	value, err := normalizeAddress("0X0000000071727De22E5E9d8BAf0edAc6f37da032")
	if err != nil {
		t.Fatalf("normalizeAddress returned error: %v", err)
	}

	const expected = "0x0000000071727de22e5e9d8baf0edac6f37da032"
	if value != expected {
		t.Fatalf("expected %q, got %q", expected, value)
	}
}

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("RPC_URL", "https://rpc.example")
	clearOptionalIndexerEnv(t)

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv returned error: %v", err)
	}

	if cfg.RPCURL != "https://rpc.example" {
		t.Fatalf("expected RPC_URL to be preserved, got %q", cfg.RPCURL)
	}
	if cfg.EntryPoint != defaultEntryPoint {
		t.Fatalf("expected default entrypoint %q, got %q", defaultEntryPoint, cfg.EntryPoint)
	}
	if cfg.BatchSize != defaultBatchSize {
		t.Fatalf("expected default batch size %d, got %d", defaultBatchSize, cfg.BatchSize)
	}
	if cfg.Confirmations != defaultConfirmations {
		t.Fatalf("expected default confirmations %d, got %d", defaultConfirmations, cfg.Confirmations)
	}
	if cfg.ReorgWindow != defaultConfirmations {
		t.Fatalf("expected default reorg window %d, got %d", defaultConfirmations, cfg.ReorgWindow)
	}
	if cfg.PollInterval != defaultPollInterval {
		t.Fatalf("expected default poll interval %s, got %s", defaultPollInterval, cfg.PollInterval)
	}
	if cfg.RequestTimeout != defaultRequestTimeout {
		t.Fatalf("expected default request timeout %s, got %s", defaultRequestTimeout, cfg.RequestTimeout)
	}
	if cfg.RPCConcurrency != defaultRPCConcurrency {
		t.Fatalf("expected default rpc concurrency %d, got %d", defaultRPCConcurrency, cfg.RPCConcurrency)
	}
	if cfg.RPCResponseMaxBytes != defaultRPCResponseMax {
		t.Fatalf("expected default rpc response max %d, got %d", defaultRPCResponseMax, cfg.RPCResponseMaxBytes)
	}
	if !cfg.EnableTxEnrichment {
		t.Fatal("expected tx enrichment to be enabled by default")
	}
	if cfg.AllowCursorTrim {
		t.Fatal("expected cursor trim to be disabled by default")
	}
	if cfg.StateKey != stateKeyLastIndexedBlock {
		t.Fatalf("expected state key %q, got %q", stateKeyLastIndexedBlock, cfg.StateKey)
	}
}

func TestLoadConfigFromEnv_ParsesOptionals(t *testing.T) {
	t.Setenv("RPC_URL", "https://rpc.example")
	t.Setenv("ENTRYPOINT", "0X0000000071727De22E5E9d8BAf0edAc6f37da032")
	t.Setenv("INDEXER_START_BLOCK", "123")
	t.Setenv("INDEXER_BATCH_SIZE", "777")
	t.Setenv("INDEXER_CONFIRMATIONS", "9")
	t.Setenv("INDEXER_REORG_WINDOW", "4")
	t.Setenv("INDEXER_POLL_INTERVAL", "750ms")
	t.Setenv("INDEXER_REQUEST_TIMEOUT", "9s")
	t.Setenv("INDEXER_RPC_CONCURRENCY", "12")
	t.Setenv("INDEXER_RPC_RESPONSE_MAX_BYTES", "1048576")
	t.Setenv("INDEXER_ENABLE_TX_ENRICHMENT", "false")
	t.Setenv("INDEXER_ALLOW_CURSOR_TRIM", "true")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv returned error: %v", err)
	}

	if !cfg.HasStartBlock || cfg.StartBlock != 123 {
		t.Fatalf("expected start block 123, got has=%v block=%d", cfg.HasStartBlock, cfg.StartBlock)
	}
	if cfg.BatchSize != 777 {
		t.Fatalf("expected batch size 777, got %d", cfg.BatchSize)
	}
	if cfg.Confirmations != 9 {
		t.Fatalf("expected confirmations 9, got %d", cfg.Confirmations)
	}
	if cfg.ReorgWindow != 4 {
		t.Fatalf("expected reorg window 4, got %d", cfg.ReorgWindow)
	}
	if cfg.PollInterval != 750*time.Millisecond {
		t.Fatalf("expected poll interval 750ms, got %s", cfg.PollInterval)
	}
	if cfg.RequestTimeout != 9*time.Second {
		t.Fatalf("expected request timeout 9s, got %s", cfg.RequestTimeout)
	}
	if cfg.RPCConcurrency != 12 {
		t.Fatalf("expected rpc concurrency 12, got %d", cfg.RPCConcurrency)
	}
	if cfg.RPCResponseMaxBytes != 1048576 {
		t.Fatalf("expected rpc response max 1048576, got %d", cfg.RPCResponseMaxBytes)
	}
	if cfg.EnableTxEnrichment {
		t.Fatal("expected tx enrichment to be disabled")
	}
	if !cfg.AllowCursorTrim {
		t.Fatal("expected cursor trim to be enabled")
	}
}

func TestLoadConfigFromEnv_RequiresRPCURL(t *testing.T) {
	clearOptionalIndexerEnv(t)
	t.Setenv("RPC_URL", "")

	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected error when RPC_URL is missing")
	}
	if !strings.Contains(err.Error(), "RPC_URL") {
		t.Fatalf("expected RPC_URL error, got %v", err)
	}
}

func TestLoadConfigFromEnv_InvalidNumericValues(t *testing.T) {
	t.Setenv("RPC_URL", "https://rpc.example")

	t.Setenv("INDEXER_BATCH_SIZE", "0")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected batch size validation error")
	}

	t.Setenv("INDEXER_BATCH_SIZE", "500")
	t.Setenv("INDEXER_RPC_CONCURRENCY", "0")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected rpc concurrency validation error")
	}

	t.Setenv("INDEXER_RPC_CONCURRENCY", "abc")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected rpc concurrency parse error")
	}

	t.Setenv("INDEXER_RPC_CONCURRENCY", "8")
	t.Setenv("INDEXER_RPC_RESPONSE_MAX_BYTES", "0")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected rpc response max validation error")
	}

	t.Setenv("INDEXER_RPC_RESPONSE_MAX_BYTES", "abc")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected rpc response max parse error")
	}

	t.Setenv("INDEXER_RPC_RESPONSE_MAX_BYTES", "9223372036854775807")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected rpc response max overflow-prone validation error")
	}
}

func TestLoadConfigFromEnv_InvalidDurationsAndBools(t *testing.T) {
	t.Setenv("RPC_URL", "https://rpc.example")

	t.Setenv("INDEXER_POLL_INTERVAL", "0s")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected poll interval validation error")
	}

	t.Setenv("INDEXER_POLL_INTERVAL", "1s")
	t.Setenv("INDEXER_REQUEST_TIMEOUT", "0s")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected request timeout validation error")
	}

	t.Setenv("INDEXER_REQUEST_TIMEOUT", "1s")
	t.Setenv("INDEXER_ENABLE_TX_ENRICHMENT", "maybe")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected tx enrichment parse error")
	}

	t.Setenv("INDEXER_ENABLE_TX_ENRICHMENT", "true")
	t.Setenv("INDEXER_ALLOW_CURSOR_TRIM", "maybe")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected cursor trim parse error")
	}
}

func clearOptionalIndexerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENTRYPOINT", "")
	t.Setenv("INDEXER_START_BLOCK", "")
	t.Setenv("INDEXER_BATCH_SIZE", "")
	t.Setenv("INDEXER_CONFIRMATIONS", "")
	t.Setenv("INDEXER_REORG_WINDOW", "")
	t.Setenv("INDEXER_POLL_INTERVAL", "")
	t.Setenv("INDEXER_REQUEST_TIMEOUT", "")
	t.Setenv("INDEXER_RPC_CONCURRENCY", "")
	t.Setenv("INDEXER_RPC_RESPONSE_MAX_BYTES", "")
	t.Setenv("INDEXER_ENABLE_TX_ENRICHMENT", "")
	t.Setenv("INDEXER_ALLOW_CURSOR_TRIM", "")
}
