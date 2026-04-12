package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool creates an isolated Postgres schema for the test, runs migrations
// inside it, and returns a pool with search_path pinned to that schema.
// The schema is dropped on test cleanup, so no shared data is touched.
// Skips the test when DATABASE_URL is not set.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())

	// Bootstrap: connect with default search_path to create the schema.
	bootstrap, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("bootstrap connection: %v", err)
	}
	if _, err := bootstrap.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %q", schema)); err != nil {
		bootstrap.Close()
		t.Fatalf("create test schema: %v", err)
	}
	bootstrap.Close()

	// Create a pool with search_path pinned to the isolated schema.
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create pool with test schema: %v", err)
	}

	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		// Drop the test schema using a fresh connection.
		cleanup, err := pgxpool.New(context.Background(), dsn)
		if err == nil {
			cleanup.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %q CASCADE", schema))
			cleanup.Close()
		}
	})

	return pool
}

func TestGetStatsIntegration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	// Distinct senders and paymasters.
	senderA := make([]byte, 20)
	senderA[19] = 0x0A
	senderB := make([]byte, 20)
	senderB[19] = 0x0B
	senderC := make([]byte, 20)
	senderC[19] = 0x0C

	noPaymaster := make([]byte, 20) // all zeros = self-funded
	paymaster1 := make([]byte, 20)
	paymaster1[19] = 0xFF

	// Insert 5 operations with mixed paymaster and success values:
	//
	//  #  | success | paymaster    | sender
	//  ---|---------|--------------|-------
	//  1  | true    | zero         | A
	//  2  | true    | zero         | A
	//  3  | true    | paymaster1   | B        <- sponsored
	//  4  | false   | zero         | C
	//  5  | false   | paymaster1   | B        <- sponsored
	//
	// Expected: total=5, success=3, sponsored=2, unique_senders=3
	ops := []UserOperation{
		testIntOp(senderA, noPaymaster, true, 900000, 0),
		testIntOp(senderA, noPaymaster, true, 900000, 1),
		testIntOp(senderB, paymaster1, true, 900001, 0),
		testIntOp(senderC, noPaymaster, false, 900001, 1),
		testIntOp(senderB, paymaster1, false, 900001, 2),
	}

	err := ReplaceOperationsAndSetCursor(ctx, pool, "test_cursor", 900000, 900001, 900001, ops)
	if err != nil {
		t.Fatalf("insert test operations: %v", err)
	}

	got, err := GetStats(ctx, pool)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if got.TotalOps != 5 {
		t.Errorf("TotalOps = %d, want 5", got.TotalOps)
	}
	if got.SuccessCount != 3 {
		t.Errorf("SuccessCount = %d, want 3", got.SuccessCount)
	}
	if got.SponsoredCount != 2 {
		t.Errorf("SponsoredCount = %d, want 2", got.SponsoredCount)
	}
	if got.UniqueSenders != 3 {
		t.Errorf("UniqueSenders = %d, want 3", got.UniqueSenders)
	}
}

// testIntOp builds a minimal UserOperation for integration testing.
func testIntOp(sender, paymaster []byte, success bool, block int64, logIdx int32) UserOperation {
	hash := make([]byte, 32)
	hash[0] = byte(block)
	hash[1] = byte(logIdx)

	txHash := make([]byte, 32)
	txHash[0] = byte(block >> 8)
	txHash[1] = byte(block)
	txHash[2] = byte(logIdx)

	return UserOperation{
		UserOpHash:     hash,
		Sender:         sender,
		Paymaster:      paymaster,
		Nonce:          "0",
		Success:        success,
		ActualGasCost:  "21000",
		ActualGasUsed:  "21000",
		TxHash:         txHash,
		BlockNumber:    block,
		BlockTimestamp: 1700000000 + block,
		LogIndex:       logIdx,
	}
}
