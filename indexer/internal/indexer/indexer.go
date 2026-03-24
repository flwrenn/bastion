package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flwrenn/bastion/indexer/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	cfg                          Config
	pool                         *pgxpool.Pool
	rpc                          *rpcClient
	entryPoint                   string
	blockTimestampCache          map[uint64]int64
	cacheMu                      sync.RWMutex
	newHeadSubscriptionFactory   func(context.Context, string) (headSubscription, error)
	subscriptionReconnectBackoff time.Duration
}

const blockTimestampCacheMaxEntries = 4096

var errInitialBackfillStartBlockRequired = errors.New("initial backfill start block is required")

func New(cfg Config, pool *pgxpool.Pool) (*Service, error) {
	if cfg.RPCURL == "" {
		return nil, fmt.Errorf("RPCURL is required")
	}
	if cfg.PollInterval <= 0 {
		return nil, fmt.Errorf("PollInterval must be greater than 0")
	}
	if cfg.BatchSize == 0 {
		return nil, fmt.Errorf("BatchSize must be greater than 0")
	}
	if cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("RequestTimeout must be greater than 0")
	}
	if cfg.RPCConcurrency <= 0 {
		return nil, fmt.Errorf("RPCConcurrency must be greater than 0")
	}
	if cfg.RPCResponseMaxBytes <= 0 {
		return nil, fmt.Errorf("RPCResponseMaxBytes must be greater than 0")
	}
	if cfg.RPCResponseMaxBytes >= math.MaxInt64 {
		return nil, fmt.Errorf("RPCResponseMaxBytes must be less than %d", math.MaxInt64)
	}
	if cfg.StateKey == "" {
		return nil, fmt.Errorf("StateKey is required")
	}
	if pool == nil {
		return nil, fmt.Errorf("pool is required")
	}

	normalizedEntryPoint, err := normalizeAddress(cfg.EntryPoint)
	if err != nil {
		return nil, fmt.Errorf("normalize entrypoint: %w", err)
	}

	return &Service{
		cfg:                          cfg,
		pool:                         pool,
		rpc:                          newRPCClient(cfg.RPCURL, cfg.RPCResponseMaxBytes),
		entryPoint:                   normalizedEntryPoint,
		blockTimestampCache:          make(map[uint64]int64),
		newHeadSubscriptionFactory:   newWebSocketHeadSubscription,
		subscriptionReconnectBackoff: defaultSubscriptionBackoff,
	}, nil
}

func (s *Service) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return nil
	}
	if s.cfg.PollInterval <= 0 {
		return fmt.Errorf("PollInterval must be greater than 0")
	}
	if s.pool == nil {
		return fmt.Errorf("pool is required")
	}
	if s.rpc == nil {
		return fmt.Errorf("rpc client is required")
	}

	if err := s.indexOnce(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("initial index iteration failed: %w", err)
	}

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	wakeCh := make(chan struct{}, 1)

	if s.cfg.WSURL != "" {
		slog.Info("starting head subscription", "ws_endpoint", websocketLogEndpoint(s.cfg.WSURL))
		go s.runHeadSubscriptionLoop(ctx, wakeCh)
	}

	runIteration := func() error {
		err := s.indexOnce(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil
		}
		if isFatalIndexIterationError(err) {
			return fmt.Errorf("index iteration failed: %w", err)
		}
		slog.Error("index iteration failed", "err", err)
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := runIteration(); err != nil {
				return err
			}
		case <-wakeCh:
			if err := runIteration(); err != nil {
				return err
			}
		}
	}
}

func (s *Service) runHeadSubscriptionLoop(ctx context.Context, wakeCh chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}

		subscription, err := s.newHeadSubscription(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if s.cfg.WSURL == "" {
				return
			}
			slog.Warn("head subscription connect failed; retrying", "err", redactWebSocketURLError(err, s.cfg.WSURL))
			if !sleepContext(ctx, s.subscriptionBackoff()) {
				return
			}
			continue
		}

		slog.Info("head subscription connected")

		err = s.consumeHeadSubscription(ctx, subscription, wakeCh)
		closeErr := subscription.Close()
		if closeErr != nil && ctx.Err() == nil {
			slog.Warn("head subscription close failed", "err", closeErr)
		}

		if ctx.Err() != nil {
			return
		}

		if err != nil {
			slog.Warn("head subscription disconnected; reconnecting", "err", redactWebSocketURLError(err, s.cfg.WSURL))
		}
		if !sleepContext(ctx, s.subscriptionBackoff()) {
			return
		}
	}
}

func websocketLogEndpoint(wsURL string) string {
	parsed, err := url.Parse(wsURL)
	if err != nil || parsed.Host == "" {
		return "invalid"
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		return parsed.Host
	}

	return scheme + "://" + parsed.Host
}

func redactWebSocketURLError(err error, wsURL string) string {
	if err == nil {
		return ""
	}

	errText := err.Error()
	if wsURL == "" {
		return errText
	}

	endpoint := websocketLogEndpoint(wsURL)
	return strings.ReplaceAll(errText, wsURL, endpoint)
}

func (s *Service) subscriptionBackoff() time.Duration {
	if s.subscriptionReconnectBackoff <= 0 {
		return defaultSubscriptionBackoff
	}

	return s.subscriptionReconnectBackoff
}

func (s *Service) newHeadSubscription(ctx context.Context) (headSubscription, error) {
	factory := s.newHeadSubscriptionFactory
	if factory == nil {
		factory = newWebSocketHeadSubscription
	}

	return factory(ctx, s.cfg.WSURL)
}

func (s *Service) consumeHeadSubscription(ctx context.Context, subscription headSubscription, wakeCh chan<- struct{}) error {
	for {
		if err := subscription.Next(ctx); err != nil {
			return err
		}

		select {
		case wakeCh <- struct{}{}:
		default:
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isFatalIndexIterationError(err error) bool {
	return errors.Is(err, errInitialBackfillStartBlockRequired)
}

func (s *Service) indexOnce(ctx context.Context) error {
	cursor, hasCursor, err := db.GetStateUint64(ctx, s.pool, s.cfg.StateKey)
	if err != nil {
		return fmt.Errorf("load cursor: %w", err)
	}
	if err := s.validateInitialBackfillConfig(hasCursor); err != nil {
		return err
	}

	safeHead, hasSafeHead, err := s.safeHead(ctx)
	if err != nil {
		return fmt.Errorf("fetch safe head: %w", err)
	}
	if !hasSafeHead {
		slog.Debug("indexer idle", "reason", "no_safe_head_yet")
		return nil
	}

	trimmedCursor := false
	if hasCursor && cursor > safeHead {
		delta := cursor - safeHead
		if !s.cfg.AllowCursorTrim {
			slog.Warn(
				"cursor ahead of safe head; skipping iteration",
				"cursor",
				cursor,
				"safe_head",
				safeHead,
				"delta",
				delta,
			)
			return nil
		}

		slog.Warn(
			"cursor ahead of safe head; trimming future rows",
			"cursor",
			cursor,
			"safe_head",
			safeHead,
			"delta",
			delta,
		)
		if err := db.TrimOperationsAboveBlockAndSetCursor(ctx, s.pool, s.cfg.StateKey, safeHead); err != nil {
			return fmt.Errorf("reconcile cursor to safe head: %w", err)
		}
		cursor = safeHead
		trimmedCursor = true
	}

	if trimmedCursor {
		from, to := rewindRangeToSafeHead(safeHead, s.cfg.ReorgWindow)
		slog.Info("resyncing rewind window after cursor trim", "from", from, "to", to)
		if err := s.indexRange(ctx, from, to); err != nil {
			return err
		}
		return nil
	}

	from, to, ok := s.planScanRange(cursor, hasCursor, safeHead)
	if !ok {
		slog.Debug("indexer idle", "safe_head", safeHead, "cursor", cursor, "has_cursor", hasCursor)
		return nil
	}

	initialBackfill := !hasCursor
	if initialBackfill {
		slog.Info("starting historical backfill", "from", from, "to", to)
	}

	totalBlocks := to - from + 1
	processedBlocks := uint64(0)
	batchIndex := uint64(0)

	for batchFrom := from; batchFrom <= to; {
		batchTo := batchFrom + s.cfg.BatchSize - 1
		if batchTo > to || batchTo < batchFrom {
			batchTo = to
		}
		batchIndex++

		if err := s.indexRange(ctx, batchFrom, batchTo); err != nil {
			return err
		}

		if initialBackfill {
			processedBlocks += batchTo - batchFrom + 1
			remainingBlocks := totalBlocks - processedBlocks
			if remainingBlocks == 0 {
				slog.Info(
					"historical backfill complete",
					"batches",
					batchIndex,
					"processed_blocks",
					processedBlocks,
					"total_blocks",
					totalBlocks,
				)
			} else {
				slog.Debug(
					"historical backfill progress",
					"batch",
					batchIndex,
					"processed_blocks",
					processedBlocks,
					"total_blocks",
					totalBlocks,
					"remaining_blocks",
					remainingBlocks,
				)
			}
		}

		if batchTo == to {
			break
		}
		batchFrom = batchTo + 1
	}

	return nil
}

func (s *Service) safeHead(ctx context.Context) (uint64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()

	latest, err := s.rpc.latestBlockNumber(requestCtx)
	if err != nil {
		return 0, false, err
	}

	if latest < s.cfg.Confirmations {
		return 0, false, nil
	}

	return latest - s.cfg.Confirmations, true, nil
}

func rewindRangeToSafeHead(safeHead uint64, reorgWindow uint64) (uint64, uint64) {
	if safeHead > reorgWindow {
		return safeHead - reorgWindow, safeHead
	}
	return 0, safeHead
}

func (s *Service) planScanRange(cursor uint64, hasCursor bool, safeHead uint64) (uint64, uint64, bool) {
	var from uint64
	if hasCursor {
		if cursor > safeHead {
			cursor = safeHead
		}
		if cursor >= safeHead {
			return 0, 0, false
		}
		if cursor > s.cfg.ReorgWindow {
			from = cursor - s.cfg.ReorgWindow
		} else {
			from = 0
		}
	} else if s.cfg.HasStartBlock {
		from = s.cfg.StartBlock
	} else {
		return 0, 0, false
	}

	if from > safeHead {
		return 0, 0, false
	}

	return from, safeHead, true
}

func (s *Service) validateInitialBackfillConfig(hasCursor bool) error {
	if hasCursor {
		return nil
	}
	if s.cfg.HasStartBlock {
		return nil
	}

	return fmt.Errorf(
		"%w: INDEXER_START_BLOCK is required on first run when no cursor exists",
		errInitialBackfillStartBlockRequired,
	)
}

func (s *Service) indexRange(ctx context.Context, fromBlock uint64, toBlock uint64) error {
	if fromBlock > toBlock {
		return fmt.Errorf("invalid block range: from %d > to %d", fromBlock, toBlock)
	}

	if err := s.indexRangeAttempt(ctx, fromBlock, toBlock); err != nil {
		if isRPCResponseTooLarge(err) && fromBlock < toBlock {
			mid := fromBlock + (toBlock-fromBlock)/2
			if err := s.indexRange(ctx, fromBlock, mid); err != nil {
				return err
			}
			return s.indexRange(ctx, mid+1, toBlock)
		}
		return err
	}

	return nil
}

func (s *Service) indexRangeAttempt(ctx context.Context, fromBlock uint64, toBlock uint64) error {
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	logs, err := s.rpc.getLogs(requestCtx, s.entryPoint, userOperationEventTopic, fromBlock, toBlock)
	cancel()
	if err != nil {
		return fmt.Errorf("fetch logs [%d,%d]: %w", fromBlock, toBlock, err)
	}

	activeLogs := make([]rpcLog, 0, len(logs))
	for i := range logs {
		if logs[i].Removed {
			continue
		}
		activeLogs = append(activeLogs, logs[i])
	}

	txMetaByHash := make(map[string]map[string][]operationMeta)
	blockTimestamps := make(map[uint64]int64)
	if len(activeLogs) > 0 {
		if s.cfg.EnableTxEnrichment {
			txMetaByHash, err = s.loadTransactionOperationMeta(ctx, activeLogs)
			if err != nil {
				return fmt.Errorf("load transaction metadata: %w", err)
			}
		}

		blockTimestamps, err = s.loadBlockTimestamps(ctx, activeLogs)
		if err != nil {
			return fmt.Errorf("load block timestamps: %w", err)
		}
	}

	operations := make([]db.UserOperation, 0, len(activeLogs))
	for i := range activeLogs {
		log := activeLogs[i]

		event, err := decodeUserOperationEventLog(log)
		if err != nil {
			return fmt.Errorf(
				"decode user operation event tx %s block %s log_index %s: %w",
				log.TransactionHash,
				log.BlockNumber,
				log.LogIndex,
				err,
			)
		}

		blockTimestamp, ok := blockTimestamps[event.BlockNumber]
		if !ok {
			return fmt.Errorf("missing timestamp for block %d", event.BlockNumber)
		}

		meta := popOperationMeta(txMetaByHash, event.TxHashHex, event.Sender, event.Nonce)

		blockNumberInt64, err := toInt64(event.BlockNumber)
		if err != nil {
			return fmt.Errorf("convert block number %d: %w", event.BlockNumber, err)
		}

		operations = append(operations, db.UserOperation{
			UserOpHash:     event.UserOpHash,
			Sender:         event.Sender,
			Paymaster:      event.Paymaster,
			Target:         meta.Target,
			Calldata:       meta.Calldata,
			Nonce:          event.Nonce,
			Success:        event.Success,
			ActualGasCost:  event.ActualGasCost,
			ActualGasUsed:  event.ActualGasUsed,
			TxHash:         event.TxHash,
			BlockNumber:    blockNumberInt64,
			BlockTimestamp: blockTimestamp,
			LogIndex:       event.LogIndex,
		})
	}

	if err := db.ReplaceOperationsAndSetCursor(
		ctx,
		s.pool,
		s.cfg.StateKey,
		fromBlock,
		toBlock,
		toBlock,
		operations,
	); err != nil {
		return fmt.Errorf("persist range [%d,%d]: %w", fromBlock, toBlock, err)
	}

	slog.Info(
		"indexed block range",
		"from",
		fromBlock,
		"to",
		toBlock,
		"events",
		len(operations),
	)

	return nil
}

func (s *Service) loadTransactionOperationMeta(ctx context.Context, logs []rpcLog) (map[string]map[string][]operationMeta, error) {
	result := make(map[string]map[string][]operationMeta)

	txHashes := make(map[string]string)
	for i := range logs {
		log := logs[i]
		normalizedTxHash := strings.ToLower(log.TransactionHash)
		if _, exists := txHashes[normalizedTxHash]; exists {
			continue
		}
		txHashes[normalizedTxHash] = log.TransactionHash
	}

	type txJob struct {
		normalizedHash string
		rawHash        string
	}

	workerCount := s.cfg.RPCConcurrency
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(txHashes) {
		workerCount = len(txHashes)
	}
	if workerCount == 0 {
		return result, nil
	}

	jobs := make(chan txJob)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr != nil {
			return
		}
		firstErr = err
		cancel()
	}

	worker := func() {
		defer wg.Done()
		for job := range jobs {
			if workerCtx.Err() != nil {
				return
			}

			requestCtx, requestCancel := context.WithTimeout(workerCtx, s.cfg.RequestTimeout)
			tx, err := s.rpc.getTransactionByHash(requestCtx, job.rawHash)
			requestCancel()
			if err != nil {
				setErr(fmt.Errorf("load tx %s: %w", job.rawHash, err))
				return
			}

			input, err := decodeHex(tx.Input)
			if err != nil {
				setErr(fmt.Errorf("decode tx input %s: %w", tx.Hash, err))
				return
			}

			calls, err := decodeHandleOpsInput(input)
			if err != nil {
				slog.Debug("transaction input not handleOps or undecodable", "tx_hash", tx.Hash, "err", err)
				mu.Lock()
				result[job.normalizedHash] = map[string][]operationMeta{}
				mu.Unlock()
				continue
			}

			mu.Lock()
			result[job.normalizedHash] = toOperationMetaQueue(calls)
			mu.Unlock()
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

enqueue:
	for normalizedHash, rawHash := range txHashes {
		select {
		case <-workerCtx.Done():
			break enqueue
		case jobs <- txJob{normalizedHash: normalizedHash, rawHash: rawHash}:
		}
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return result, nil
}

func (s *Service) loadBlockTimestamps(ctx context.Context, logs []rpcLog) (map[uint64]int64, error) {
	result := make(map[uint64]int64)
	uniqueBlocks := make(map[uint64]struct{})
	for i := range logs {
		log := logs[i]
		blockNumber, err := parseHexUint64(log.BlockNumber)
		if err != nil {
			return nil, fmt.Errorf("parse block number %q: %w", log.BlockNumber, err)
		}
		uniqueBlocks[blockNumber] = struct{}{}
	}

	missingBlocks := make([]uint64, 0, len(uniqueBlocks))
	for blockNumber := range uniqueBlocks {
		if timestamp, ok := s.getCachedBlockTimestamp(blockNumber); ok {
			result[blockNumber] = timestamp
			continue
		}
		missingBlocks = append(missingBlocks, blockNumber)
	}

	if len(missingBlocks) == 0 {
		return result, nil
	}

	type blockJob struct {
		blockNumber uint64
	}

	workerCount := s.cfg.RPCConcurrency
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(missingBlocks) {
		workerCount = len(missingBlocks)
	}

	jobs := make(chan blockJob)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	fetched := make(map[uint64]int64, len(missingBlocks))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	setErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr != nil {
			return
		}
		firstErr = err
		cancel()
	}

	worker := func() {
		defer wg.Done()
		for job := range jobs {
			if workerCtx.Err() != nil {
				return
			}

			requestCtx, requestCancel := context.WithTimeout(workerCtx, s.cfg.RequestTimeout)
			block, err := s.rpc.getBlockByNumber(requestCtx, job.blockNumber)
			requestCancel()
			if err != nil {
				setErr(fmt.Errorf("load block %d: %w", job.blockNumber, err))
				return
			}

			timestampUint, err := parseHexUint64(block.Timestamp)
			if err != nil {
				setErr(fmt.Errorf("parse block %d timestamp %q: %w", job.blockNumber, block.Timestamp, err))
				return
			}
			timestampInt64, err := toInt64(timestampUint)
			if err != nil {
				setErr(fmt.Errorf("convert block %d timestamp %d: %w", job.blockNumber, timestampUint, err))
				return
			}

			mu.Lock()
			fetched[job.blockNumber] = timestampInt64
			mu.Unlock()
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

enqueue:
	for i := range missingBlocks {
		select {
		case <-workerCtx.Done():
			break enqueue
		case jobs <- blockJob{blockNumber: missingBlocks[i]}:
		}
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	s.storeBlockTimestamps(fetched)
	for blockNumber, timestamp := range fetched {
		result[blockNumber] = timestamp
	}

	return result, nil
}

func (s *Service) getCachedBlockTimestamp(blockNumber uint64) (int64, bool) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	timestamp, ok := s.blockTimestampCache[blockNumber]
	return timestamp, ok
}

func (s *Service) storeBlockTimestamps(values map[uint64]int64) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	for blockNumber, timestamp := range values {
		s.blockTimestampCache[blockNumber] = timestamp
	}

	if len(s.blockTimestampCache) <= blockTimestampCacheMaxEntries {
		return
	}

	keys := make([]uint64, 0, len(s.blockTimestampCache))
	for blockNumber := range s.blockTimestampCache {
		keys = append(keys, blockNumber)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	toRemove := len(s.blockTimestampCache) - blockTimestampCacheMaxEntries
	for i := 0; i < toRemove; i++ {
		delete(s.blockTimestampCache, keys[i])
	}
}

func popOperationMeta(
	txMetaByHash map[string]map[string][]operationMeta,
	txHash string,
	sender []byte,
	nonce string,
) operationMeta {
	txMeta, ok := txMetaByHash[txHash]
	if !ok {
		return operationMeta{}
	}

	key := operationKey(sender, nonce)
	queue := txMeta[key]
	if len(queue) == 0 {
		return operationMeta{}
	}

	meta := queue[0]
	if len(queue) == 1 {
		delete(txMeta, key)
	} else {
		txMeta[key] = queue[1:]
	}

	return meta
}
