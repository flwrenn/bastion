package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flwrenn/bastion/indexer/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	cfg                 Config
	pool                *pgxpool.Pool
	rpc                 *rpcClient
	entryPoint          string
	blockTimestampCache map[uint64]int64
	cacheMu             sync.RWMutex
}

const blockTimestampCacheMaxEntries = 4096

func New(cfg Config, pool *pgxpool.Pool) (*Service, error) {
	normalizedEntryPoint, err := normalizeAddress(cfg.EntryPoint)
	if err != nil {
		return nil, fmt.Errorf("normalize entrypoint: %w", err)
	}

	return &Service{
		cfg:                 cfg,
		pool:                pool,
		rpc:                 newRPCClient(cfg.RPCURL),
		entryPoint:          normalizedEntryPoint,
		blockTimestampCache: make(map[uint64]int64),
	}, nil
}

func (s *Service) Run(ctx context.Context) error {
	if err := s.indexOnce(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("initial index iteration failed: %w", err)
	}

	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.indexOnce(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				slog.Error("index iteration failed", "err", err)
			}
		}
	}
}

func (s *Service) indexOnce(ctx context.Context) error {
	safeHead, err := s.safeHead(ctx)
	if err != nil {
		return fmt.Errorf("fetch safe head: %w", err)
	}

	cursor, hasCursor, err := db.GetStateUint64(ctx, s.pool, s.cfg.StateKey)
	if err != nil {
		return fmt.Errorf("load cursor: %w", err)
	}
	if hasCursor && cursor > safeHead {
		slog.Warn(
			"cursor ahead of safe head; trimming future rows",
			"cursor",
			cursor,
			"safe_head",
			safeHead,
		)
		if err := db.TrimOperationsAboveBlockAndSetCursor(ctx, s.pool, s.cfg.StateKey, safeHead); err != nil {
			return fmt.Errorf("reconcile cursor to safe head: %w", err)
		}
		cursor = safeHead
	}

	from, to, ok := s.planScanRange(cursor, hasCursor, safeHead)
	if !ok {
		slog.Debug("indexer idle", "safe_head", safeHead, "cursor", cursor, "has_cursor", hasCursor)
		return nil
	}

	for batchFrom := from; batchFrom <= to; {
		batchTo := batchFrom + s.cfg.BatchSize - 1
		if batchTo > to || batchTo < batchFrom {
			batchTo = to
		}

		if err := s.indexRange(ctx, batchFrom, batchTo); err != nil {
			return err
		}

		if batchTo == to {
			break
		}
		batchFrom = batchTo + 1
	}

	return nil
}

func (s *Service) safeHead(ctx context.Context) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()

	latest, err := s.rpc.latestBlockNumber(requestCtx)
	if err != nil {
		return 0, err
	}

	if latest < s.cfg.Confirmations {
		return 0, nil
	}

	return latest - s.cfg.Confirmations, nil
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
		from = safeHead
	}

	if from > safeHead {
		return 0, 0, false
	}

	return from, safeHead, true
}

func (s *Service) indexRange(ctx context.Context, fromBlock uint64, toBlock uint64) error {
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
			slog.Warn("skip malformed user operation event", "err", err, "tx_hash", log.TransactionHash)
			continue
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
