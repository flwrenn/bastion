package indexer

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestDecodeUserOperationEventLog(t *testing.T) {
	t.Parallel()

	log := rpcLog{
		Topics: []string{
			userOperationEventTopic,
			"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		Data:            "0x" + hexWord(5) + hexWord(1) + hexWord(123) + hexWord(456),
		TransactionHash: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		BlockNumber:     "0x10",
		LogIndex:        "0x2",
	}

	decoded, err := decodeUserOperationEventLog(log)
	if err != nil {
		t.Fatalf("decodeUserOperationEventLog returned error: %v", err)
	}

	if decoded.Nonce != "5" {
		t.Fatalf("expected nonce 5, got %s", decoded.Nonce)
	}
	if !decoded.Success {
		t.Fatal("expected success true")
	}
	if decoded.ActualGasCost != "123" {
		t.Fatalf("expected gas cost 123, got %s", decoded.ActualGasCost)
	}
	if decoded.ActualGasUsed != "456" {
		t.Fatalf("expected gas used 456, got %s", decoded.ActualGasUsed)
	}
	if decoded.BlockNumber != 16 {
		t.Fatalf("expected block number 16, got %d", decoded.BlockNumber)
	}
	if decoded.LogIndex != 2 {
		t.Fatalf("expected log index 2, got %d", decoded.LogIndex)
	}
	if hex.EncodeToString(decoded.Sender) != "1111111111111111111111111111111111111111" {
		t.Fatalf("unexpected sender: %x", decoded.Sender)
	}
	if hex.EncodeToString(decoded.Paymaster) != "2222222222222222222222222222222222222222" {
		t.Fatalf("unexpected paymaster: %x", decoded.Paymaster)
	}
}

func TestDecodeHandleOpsInputWithExecute(t *testing.T) {
	t.Parallel()

	innerData := []byte{0xde, 0xad, 0xbe, 0xef}
	target := mustAddressBytes("00000000000000000000000000000000000000aa")
	sender := mustAddressBytes("0000000000000000000000000000000000000001")

	executeCallData := buildExecuteCallData(target, innerData)
	handleOpsInput := buildHandleOpsInput(sender, 1, executeCallData)

	calls, err := decodeHandleOpsInput(handleOpsInput)
	if err != nil {
		t.Fatalf("decodeHandleOpsInput returned error: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	call := calls[0]
	if hex.EncodeToString(call.Sender) != "0000000000000000000000000000000000000001" {
		t.Fatalf("unexpected sender: %x", call.Sender)
	}
	if call.Nonce != "1" {
		t.Fatalf("expected nonce 1, got %s", call.Nonce)
	}
	if !bytes.Equal(call.Target, target) {
		t.Fatalf("unexpected target: %x", call.Target)
	}
	if !bytes.Equal(call.Calldata, innerData) {
		t.Fatalf("unexpected calldata: %x", call.Calldata)
	}
}

func TestDecodeHandleOpsInputSkipsExecuteWithNonZeroValue(t *testing.T) {
	t.Parallel()

	innerData := []byte{0xde, 0xad, 0xbe, 0xef}
	target := mustAddressBytes("00000000000000000000000000000000000000aa")
	sender := mustAddressBytes("0000000000000000000000000000000000000001")

	executeCallData := buildExecuteCallDataWithValue(target, 1, innerData)
	handleOpsInput := buildHandleOpsInput(sender, 1, executeCallData)

	calls, err := decodeHandleOpsInput(handleOpsInput)
	if err != nil {
		t.Fatalf("decodeHandleOpsInput returned error: %v", err)
	}

	if len(calls) != 0 {
		t.Fatalf("expected 0 calls, got %d", len(calls))
	}
}

func TestDecodeHandleOpsInputRejectsOversizedArrayLen(t *testing.T) {
	t.Parallel()

	selector := []byte{0x76, 0x5e, 0x82, 0x7f}
	args := make([]byte, 64)
	copy(args[0:32], uintWord(32))
	copy(args[32:64], uintWord(2))

	input := append(selector, args...)
	_, err := decodeHandleOpsInput(input)
	if err == nil {
		t.Fatal("expected error for oversized ops array length")
	}
}

func TestDecodeHandleOpsInputRejectsTupleOffsetOutOfBounds(t *testing.T) {
	t.Parallel()

	selector := []byte{0x76, 0x5e, 0x82, 0x7f}
	args := make([]byte, 32*4)
	copy(args[0:32], uintWord(64))
	copy(args[64:96], uintWord(1))
	copy(args[96:128], uintWord(1000))

	input := append(selector, args...)
	_, err := decodeHandleOpsInput(input)
	if err == nil {
		t.Fatal("expected error for tuple offset out of bounds")
	}
}

func TestDecodeAccountDeployedLog(t *testing.T) {
	t.Parallel()

	factory := mustAddressBytes("00000000000000000000000000000000000000ff")
	paymaster := mustAddressBytes("00000000000000000000000000000000000000aa")

	log := rpcLog{
		Topics: []string{
			accountDeployedTopic,
			"0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"0x0000000000000000000000001111111111111111111111111111111111111111",
		},
		Data:            "0x" + hex.EncodeToString(addressWord(factory)) + hex.EncodeToString(addressWord(paymaster)),
		TransactionHash: "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		BlockNumber:     "0x42",
		LogIndex:        "0x3",
	}

	decoded, err := decodeAccountDeployedLog(log)
	if err != nil {
		t.Fatalf("decodeAccountDeployedLog returned error: %v", err)
	}

	if hex.EncodeToString(decoded.UserOpHash) != "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("unexpected userOpHash: %x", decoded.UserOpHash)
	}
	if hex.EncodeToString(decoded.Sender) != "1111111111111111111111111111111111111111" {
		t.Fatalf("unexpected sender: %x", decoded.Sender)
	}
	if !bytes.Equal(decoded.Factory, factory) {
		t.Fatalf("unexpected factory: %x", decoded.Factory)
	}
	if !bytes.Equal(decoded.Paymaster, paymaster) {
		t.Fatalf("unexpected paymaster: %x", decoded.Paymaster)
	}
	if decoded.BlockNumber != 0x42 {
		t.Fatalf("expected block number 0x42, got %d", decoded.BlockNumber)
	}
	if decoded.LogIndex != 3 {
		t.Fatalf("expected log index 3, got %d", decoded.LogIndex)
	}
}

func TestDecodeAccountDeployedLogRejectsWrongTopicCount(t *testing.T) {
	t.Parallel()

	log := rpcLog{
		Topics: []string{accountDeployedTopic, "0x00"},
	}
	if _, err := decodeAccountDeployedLog(log); err == nil {
		t.Fatal("expected topic-count error")
	}
}

func TestDecodeUserOperationRevertReasonLog(t *testing.T) {
	t.Parallel()

	// revert reason = UTF-8 "boom"
	reason := []byte("boom")
	reasonEncoded := encodeBytes(reason) // length word + padded bytes

	// data layout: nonce (word) + offset (word) + dynamic bytes
	data := make([]byte, 0, 64+len(reasonEncoded))
	data = append(data, uintWord(7)...)  // nonce = 7
	data = append(data, uintWord(64)...) // offset = 64 (skip nonce + offset words)
	data = append(data, reasonEncoded...)

	log := rpcLog{
		Topics: []string{
			userOperationRevertReasonTopic,
			"0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		Data:            "0x" + hex.EncodeToString(data),
		TransactionHash: "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		BlockNumber:     "0x100",
		LogIndex:        "0x5",
	}

	decoded, err := decodeUserOperationRevertReasonLog(log)
	if err != nil {
		t.Fatalf("decodeUserOperationRevertReasonLog returned error: %v", err)
	}

	if decoded.Nonce != "7" {
		t.Fatalf("expected nonce 7, got %s", decoded.Nonce)
	}
	if !bytes.Equal(decoded.RevertReason, reason) {
		t.Fatalf("unexpected revert reason: %x", decoded.RevertReason)
	}
	if decoded.BlockNumber != 0x100 {
		t.Fatalf("expected block number 0x100, got %d", decoded.BlockNumber)
	}
	if decoded.LogIndex != 5 {
		t.Fatalf("expected log index 5, got %d", decoded.LogIndex)
	}
}

func TestDecodeUserOperationRevertReasonLogAcceptsEmptyBytes(t *testing.T) {
	t.Parallel()

	// Empty revertReason: length word = 0, no padding bytes.
	data := make([]byte, 0, 64+32)
	data = append(data, uintWord(0)...)  // nonce
	data = append(data, uintWord(64)...) // offset
	data = append(data, uintWord(0)...)  // bytes length = 0

	log := rpcLog{
		Topics: []string{
			userOperationRevertReasonTopic,
			"0x0000000000000000000000000000000000000000000000000000000000000001",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		Data:            "0x" + hex.EncodeToString(data),
		TransactionHash: "0x0000000000000000000000000000000000000000000000000000000000000001",
		BlockNumber:     "0x1",
		LogIndex:        "0x0",
	}

	decoded, err := decodeUserOperationRevertReasonLog(log)
	if err != nil {
		t.Fatalf("unexpected error for empty revert reason: %v", err)
	}
	if len(decoded.RevertReason) != 0 {
		t.Fatalf("expected empty revert reason, got %x", decoded.RevertReason)
	}
}

func hexWord(value uint64) string {
	return fmt.Sprintf("%064x", value)
}

func mustAddressBytes(hexAddress string) []byte {
	b, err := hex.DecodeString(hexAddress)
	if err != nil {
		panic(err)
	}
	if len(b) != 20 {
		panic("address must be 20 bytes")
	}
	return b
}

func buildExecuteCallData(target []byte, inner []byte) []byte {
	return buildExecuteCallDataWithValue(target, 0, inner)
}

func buildExecuteCallDataWithValue(target []byte, value uint64, inner []byte) []byte {
	selector := []byte{0xb6, 0x1d, 0x27, 0xf6}

	head := make([]byte, 0, 96)
	head = append(head, addressWord(target)...)
	head = append(head, uintWord(value)...)
	head = append(head, uintWord(96)...)

	tail := encodeBytes(inner)

	out := make([]byte, 0, len(selector)+len(head)+len(tail))
	out = append(out, selector...)
	out = append(out, head...)
	out = append(out, tail...)
	return out
}

func buildHandleOpsInput(sender []byte, nonce uint64, callData []byte) []byte {
	selector := []byte{0x76, 0x5e, 0x82, 0x7f}

	initCode := encodeBytes(nil)
	encodedCallData := encodeBytes(callData)
	paymasterAndData := encodeBytes(nil)
	signature := encodeBytes(nil)

	tupleHeadSize := 9 * 32
	initCodeOffset := tupleHeadSize
	callDataOffset := initCodeOffset + len(initCode)
	paymasterOffset := callDataOffset + len(encodedCallData)
	signatureOffset := paymasterOffset + len(paymasterAndData)

	tupleHead := make([]byte, 0, tupleHeadSize)
	tupleHead = append(tupleHead, addressWord(sender)...)
	tupleHead = append(tupleHead, uintWord(nonce)...)
	tupleHead = append(tupleHead, uintWord(uint64(initCodeOffset))...)
	tupleHead = append(tupleHead, uintWord(uint64(callDataOffset))...)
	tupleHead = append(tupleHead, uintWord(0)...)
	tupleHead = append(tupleHead, uintWord(0)...)
	tupleHead = append(tupleHead, uintWord(0)...)
	tupleHead = append(tupleHead, uintWord(uint64(paymasterOffset))...)
	tupleHead = append(tupleHead, uintWord(uint64(signatureOffset))...)

	tuple := make([]byte, 0, len(tupleHead)+len(initCode)+len(encodedCallData)+len(paymasterAndData)+len(signature))
	tuple = append(tuple, tupleHead...)
	tuple = append(tuple, initCode...)
	tuple = append(tuple, encodedCallData...)
	tuple = append(tuple, paymasterAndData...)
	tuple = append(tuple, signature...)

	opsData := make([]byte, 0, 64+len(tuple))
	opsData = append(opsData, uintWord(1)...)  // array length
	opsData = append(opsData, uintWord(32)...) // first tuple offset (relative to offsets base)
	opsData = append(opsData, tuple...)

	argsHead := make([]byte, 0, 64)
	argsHead = append(argsHead, uintWord(64)...) // ops starts after two head words
	argsHead = append(argsHead, addressWord(mustAddressBytes("0000000000000000000000000000000000000002"))...)

	out := make([]byte, 0, len(selector)+len(argsHead)+len(opsData))
	out = append(out, selector...)
	out = append(out, argsHead...)
	out = append(out, opsData...)

	return out
}

func addressWord(address []byte) []byte {
	if len(address) != 20 {
		panic("address must be 20 bytes")
	}
	word := make([]byte, 32)
	copy(word[12:], address)
	return word
}

func uintWord(value uint64) []byte {
	word := make([]byte, 32)
	for i := 0; i < 8; i++ {
		word[31-i] = byte(value >> (8 * i))
	}
	return word
}

func encodeBytes(value []byte) []byte {
	lengthWord := uintWord(uint64(len(value)))
	padded := make([]byte, len(value))
	copy(padded, value)
	for len(padded)%32 != 0 {
		padded = append(padded, 0)
	}
	return append(lengthWord, padded...)
}
