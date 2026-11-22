package db

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	rangeCursorKey = "test_cursor"

	pgCheckViolation = "23514"
)

var eventTables = []string{"user_operations", "account_deployments", "user_operation_reverts"}

func TestReplaceEventsAndSetCursorReplacesRangeIntegration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	below := rangeOp("op-below", "tx-below", 99, 0)
	stale := rangeOp("op-stale", "tx-stale", 100, 1)
	staleFailed := rangeOp("op-stale-failed", "tx-stale-failed", 102, 1)
	above := rangeOp("op-above", "tx-above", 103, 0)

	if err := ReplaceEventsAndSetCursor(
		ctx, pool, rangeCursorKey, 99, 103, 103,
		[]UserOperation{below, stale, staleFailed, above},
		[]AccountDeployment{rangeDeployment(below), rangeDeployment(stale), rangeDeployment(above)},
		[]UserOperationRevert{rangeRevert(below), rangeRevert(staleFailed), rangeRevert(above)},
	); err != nil {
		t.Fatalf("seed range: %v", err)
	}

	fresh := rangeOp("op-fresh", "tx-fresh", 101, 1)
	freshFailed := rangeOp("op-fresh-failed", "tx-fresh-failed", 102, 1)

	// A batched rescan replaces the rewind window one batch at a time, so a
	// batch can move the cursor back to its own upper bound while later
	// blocks still hold rows.
	if err := ReplaceEventsAndSetCursor(
		ctx, pool, rangeCursorKey, 100, 102, 102,
		[]UserOperation{fresh, freshFailed},
		[]AccountDeployment{rangeDeployment(fresh)},
		[]UserOperationRevert{rangeRevert(freshFailed)},
	); err != nil {
		t.Fatalf("replace range: %v", err)
	}

	assertStoredEvents(t, pool, map[string][]string{
		"user_operations": {
			"block=99 log=0 tx=tx-below op=op-below",
			"block=101 log=1 tx=tx-fresh op=op-fresh",
			"block=102 log=1 tx=tx-fresh-failed op=op-fresh-failed",
			"block=103 log=0 tx=tx-above op=op-above",
		},
		"account_deployments": {
			"block=99 log=1000 tx=tx-below op=op-below",
			"block=101 log=1001 tx=tx-fresh op=op-fresh",
			"block=103 log=1000 tx=tx-above op=op-above",
		},
		"user_operation_reverts": {
			"block=99 log=2000 tx=tx-below op=op-below",
			"block=102 log=2001 tx=tx-fresh-failed op=op-fresh-failed",
			"block=103 log=2000 tx=tx-above op=op-above",
		},
	})
	assertCursor(t, pool, 102)
}

// A reorg usually re-includes the same UserOperation in a different
// transaction. The stale deployment and revert rows share its user_op_hash, so
// they have to be gone before the new ones can satisfy UNIQUE (user_op_hash).
func TestReplaceEventsAndSetCursorReincludedOperationIntegration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	original := rangeOp("op-reincluded", "tx-original", 100, 0)
	if err := ReplaceEventsAndSetCursor(
		ctx, pool, rangeCursorKey, 100, 101, 101,
		[]UserOperation{original},
		[]AccountDeployment{rangeDeployment(original)},
		[]UserOperationRevert{rangeRevert(original)},
	); err != nil {
		t.Fatalf("seed range: %v", err)
	}

	reincluded := rangeOp("op-reincluded", "tx-reorged", 101, 3)
	if err := ReplaceEventsAndSetCursor(
		ctx, pool, rangeCursorKey, 100, 101, 101,
		[]UserOperation{reincluded},
		[]AccountDeployment{rangeDeployment(reincluded)},
		[]UserOperationRevert{rangeRevert(reincluded)},
	); err != nil {
		t.Fatalf("replace range: %v", err)
	}

	assertStoredEvents(t, pool, map[string][]string{
		"user_operations":        {"block=101 log=3 tx=tx-reorged op=op-reincluded"},
		"account_deployments":    {"block=101 log=1003 tx=tx-reorged op=op-reincluded"},
		"user_operation_reverts": {"block=101 log=2003 tx=tx-reorged op=op-reincluded"},
	})
	assertCursor(t, pool, 101)
}

func TestReplaceEventsAndSetCursorRollsBackOnFailureIntegration(t *testing.T) {
	for _, failingTable := range eventTables {
		t.Run(failingTable, func(t *testing.T) {
			pool := testPool(t)
			ctx := context.Background()

			seeded := rangeOp("op-seeded", "tx-seeded", 100, 0)
			if err := ReplaceEventsAndSetCursor(
				ctx, pool, rangeCursorKey, 100, 102, 102,
				[]UserOperation{seeded},
				[]AccountDeployment{rangeDeployment(seeded)},
				[]UserOperationRevert{rangeRevert(seeded)},
			); err != nil {
				t.Fatalf("seed range: %v", err)
			}

			// The batch is valid up to its last row in failingTable, so the
			// range delete and every earlier insert have already run inside
			// the transaction when the CHECK on user_op_hash length fires.
			first := rangeOp("op-first", "tx-first", 101, 0)
			second := rangeOp("op-second", "tx-second", 102, 0)
			operations := []UserOperation{first, second}
			deployments := []AccountDeployment{rangeDeployment(first), rangeDeployment(second)}
			reverts := []UserOperationRevert{rangeRevert(first), rangeRevert(second)}

			truncated := second.UserOpHash[:31]
			switch failingTable {
			case "user_operations":
				operations[1].UserOpHash = truncated
			case "account_deployments":
				deployments[1].UserOpHash = truncated
			case "user_operation_reverts":
				reverts[1].UserOpHash = truncated
			}

			err := ReplaceEventsAndSetCursor(
				ctx, pool, rangeCursorKey, 100, 104, 104,
				operations, deployments, reverts,
			)

			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("replace range error = %v, want a Postgres error", err)
			}
			if pgErr.Code != pgCheckViolation || pgErr.TableName != failingTable {
				t.Fatalf(
					"replace range failed with code %s on table %q, want %s on %q: %v",
					pgErr.Code, pgErr.TableName, pgCheckViolation, failingTable, err,
				)
			}

			assertStoredEvents(t, pool, map[string][]string{
				"user_operations":        {"block=100 log=0 tx=tx-seeded op=op-seeded"},
				"account_deployments":    {"block=100 log=1000 tx=tx-seeded op=op-seeded"},
				"user_operation_reverts": {"block=100 log=2000 tx=tx-seeded op=op-seeded"},
			})
			assertCursor(t, pool, 102)
		})
	}
}

func rangeOp(opLabel, txLabel string, block int64, logIdx int32) UserOperation {
	sender := make([]byte, 20)
	sender[19] = 0x01

	return UserOperation{
		UserOpHash:     bytes32(opLabel),
		Sender:         sender,
		Paymaster:      make([]byte, 20),
		Nonce:          "0",
		Success:        true,
		ActualGasCost:  "21000",
		ActualGasUsed:  "21000",
		TxHash:         bytes32(txLabel),
		BlockNumber:    block,
		BlockTimestamp: 1700000000 + block,
		LogIndex:       logIdx,
	}
}

// rangeDeployment and rangeRevert place their log at a fixed offset from the
// operation's log index so the three events of one operation never collide on
// (tx_hash, log_index) within a table.
func rangeDeployment(op UserOperation) AccountDeployment {
	return AccountDeployment{
		UserOpHash:     op.UserOpHash,
		Sender:         op.Sender,
		Factory:        make([]byte, 20),
		Paymaster:      op.Paymaster,
		TxHash:         op.TxHash,
		BlockNumber:    op.BlockNumber,
		BlockTimestamp: op.BlockTimestamp,
		LogIndex:       1000 + op.LogIndex,
	}
}

func rangeRevert(op UserOperation) UserOperationRevert {
	return UserOperationRevert{
		UserOpHash:     op.UserOpHash,
		Sender:         op.Sender,
		Nonce:          op.Nonce,
		RevertReason:   []byte("boom"),
		TxHash:         op.TxHash,
		BlockNumber:    op.BlockNumber,
		BlockTimestamp: op.BlockTimestamp,
		LogIndex:       2000 + op.LogIndex,
	}
}

func assertStoredEvents(t *testing.T, pool *pgxpool.Pool, want map[string][]string) {
	t.Helper()

	for _, table := range eventTables {
		got := storedEvents(t, pool, table)
		if !slices.Equal(got, want[table]) {
			t.Errorf(
				"%s rows:\n  got:\n    %s\n  want:\n    %s",
				table,
				strings.Join(got, "\n    "),
				strings.Join(want[table], "\n    "),
			)
		}
	}
}

// storedEvents returns one line per row of an event table in block order.
// Hashes print as the label bytes32 was given, so a mismatch reads as fixture
// names instead of hex.
func storedEvents(t *testing.T, pool *pgxpool.Pool, table string) []string {
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
			logIdx     int32
			txHash     []byte
			userOpHash []byte
		)
		if err := rows.Scan(&block, &logIdx, &txHash, &userOpHash); err != nil {
			t.Fatalf("scan %s row: %v", table, err)
		}
		lines = append(lines, fmt.Sprintf(
			"block=%d log=%d tx=%s op=%s",
			block, logIdx, hashLabel(txHash), hashLabel(userOpHash),
		))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s rows: %v", table, err)
	}

	return lines
}

func hashLabel(hash []byte) string {
	return strings.TrimRight(string(hash), "\x00")
}

func assertCursor(t *testing.T, pool *pgxpool.Pool, want uint64) {
	t.Helper()

	got, ok, err := GetStateUint64(context.Background(), pool, rangeCursorKey)
	if err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if !ok {
		t.Fatalf("cursor missing, want %d", want)
	}
	if got != want {
		t.Errorf("cursor = %d, want %d", got, want)
	}
}
