package indexer

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEntryPoint        = "0x0000000071727de22e5e9d8baf0edac6f37da032"
	defaultBatchSize         = uint64(500)
	defaultConfirmations     = uint64(3)
	defaultPollInterval      = 4 * time.Second
	defaultRequestTimeout    = 15 * time.Second
	defaultRPCConcurrency    = 8
	defaultRPCResponseMax    = 8 * 1024 * 1024
	defaultRPCMaxRetries     = 5
	defaultRPCRetryBaseDelay = 500 * time.Millisecond
	defaultRPCRetryMaxDelay  = 30 * time.Second

	stateKeyLastIndexedBlock = "user_operations.last_indexed_block"
)

type Config struct {
	RPCURL              string
	WSURL               string
	EntryPoint          string
	StartBlock          uint64
	HasStartBlock       bool
	BatchSize           uint64
	Confirmations       uint64
	ReorgWindow         uint64
	PollInterval        time.Duration
	RequestTimeout      time.Duration
	RPCConcurrency      int
	RPCResponseMaxBytes int64
	RPCMaxRetries       int
	RPCRetryBaseDelay   time.Duration
	RPCRetryMaxDelay    time.Duration
	EnableTxEnrichment  bool
	AllowCursorTrim     bool
	StateKey            string
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		RPCURL:              strings.TrimSpace(os.Getenv("RPC_URL")),
		EntryPoint:          defaultEntryPoint,
		BatchSize:           defaultBatchSize,
		Confirmations:       defaultConfirmations,
		PollInterval:        defaultPollInterval,
		RequestTimeout:      defaultRequestTimeout,
		RPCConcurrency:      defaultRPCConcurrency,
		RPCResponseMaxBytes: defaultRPCResponseMax,
		RPCMaxRetries:       defaultRPCMaxRetries,
		RPCRetryBaseDelay:   defaultRPCRetryBaseDelay,
		RPCRetryMaxDelay:    defaultRPCRetryMaxDelay,
		EnableTxEnrichment:  true,
		StateKey:            stateKeyLastIndexedBlock,
	}

	if cfg.RPCURL == "" {
		return Config{}, fmt.Errorf("RPC_URL is not set")
	}

	if value := strings.TrimSpace(os.Getenv("WS_RPC_URL")); value != "" {
		normalizedWSURL, err := normalizeWebSocketURL(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse WS_RPC_URL: %w", err)
		}
		cfg.WSURL = normalizedWSURL
	}

	if value := strings.TrimSpace(os.Getenv("ENTRYPOINT")); value != "" {
		normalized, err := normalizeAddress(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse ENTRYPOINT: %w", err)
		}
		cfg.EntryPoint = normalized
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_START_BLOCK")); value != "" {
		start, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_START_BLOCK: %w", err)
		}
		cfg.StartBlock = start
		cfg.HasStartBlock = true
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_BATCH_SIZE")); value != "" {
		batchSize, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_BATCH_SIZE: %w", err)
		}
		if batchSize == 0 {
			return Config{}, fmt.Errorf("INDEXER_BATCH_SIZE must be greater than 0")
		}
		cfg.BatchSize = batchSize
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_CONFIRMATIONS")); value != "" {
		confirmations, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_CONFIRMATIONS: %w", err)
		}
		cfg.Confirmations = confirmations
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_REORG_WINDOW")); value != "" {
		reorgWindow, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_REORG_WINDOW: %w", err)
		}
		cfg.ReorgWindow = reorgWindow
	} else {
		cfg.ReorgWindow = cfg.Confirmations
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_POLL_INTERVAL")); value != "" {
		pollInterval, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_POLL_INTERVAL: %w", err)
		}
		if pollInterval <= 0 {
			return Config{}, fmt.Errorf("INDEXER_POLL_INTERVAL must be greater than 0")
		}
		cfg.PollInterval = pollInterval
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_REQUEST_TIMEOUT")); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_REQUEST_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("INDEXER_REQUEST_TIMEOUT must be greater than 0")
		}
		cfg.RequestTimeout = timeout
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_RPC_CONCURRENCY")); value != "" {
		concurrency, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_RPC_CONCURRENCY: %w", err)
		}
		if concurrency <= 0 {
			return Config{}, fmt.Errorf("INDEXER_RPC_CONCURRENCY must be greater than 0")
		}
		cfg.RPCConcurrency = concurrency
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_RPC_RESPONSE_MAX_BYTES")); value != "" {
		limit, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_RPC_RESPONSE_MAX_BYTES: %w", err)
		}
		if limit <= 0 {
			return Config{}, fmt.Errorf("INDEXER_RPC_RESPONSE_MAX_BYTES must be greater than 0")
		}
		if limit >= math.MaxInt64 {
			return Config{}, fmt.Errorf("INDEXER_RPC_RESPONSE_MAX_BYTES must be less than %d", math.MaxInt64)
		}
		cfg.RPCResponseMaxBytes = limit
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_RPC_MAX_RETRIES")); value != "" {
		maxRetries, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_RPC_MAX_RETRIES: %w", err)
		}
		if maxRetries <= 0 {
			return Config{}, fmt.Errorf("INDEXER_RPC_MAX_RETRIES must be greater than 0")
		}
		cfg.RPCMaxRetries = maxRetries
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_RPC_RETRY_BASE_DELAY")); value != "" {
		baseDelay, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_RPC_RETRY_BASE_DELAY: %w", err)
		}
		if baseDelay <= 0 {
			return Config{}, fmt.Errorf("INDEXER_RPC_RETRY_BASE_DELAY must be greater than 0")
		}
		cfg.RPCRetryBaseDelay = baseDelay
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_RPC_RETRY_MAX_DELAY")); value != "" {
		maxDelay, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_RPC_RETRY_MAX_DELAY: %w", err)
		}
		if maxDelay <= 0 {
			return Config{}, fmt.Errorf("INDEXER_RPC_RETRY_MAX_DELAY must be greater than 0")
		}
		cfg.RPCRetryMaxDelay = maxDelay
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_ENABLE_TX_ENRICHMENT")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_ENABLE_TX_ENRICHMENT: %w", err)
		}
		cfg.EnableTxEnrichment = enabled
	}

	if value := strings.TrimSpace(os.Getenv("INDEXER_ALLOW_CURSOR_TRIM")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse INDEXER_ALLOW_CURSOR_TRIM: %w", err)
		}
		cfg.AllowCursorTrim = enabled
	}

	return cfg, nil
}

func normalizeWebSocketURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", fmt.Errorf("scheme must be ws or wss")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("host is required")
	}

	return parsed.String(), nil
}
