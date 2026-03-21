package indexer

import (
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"strings"
)

var (
	userOperationEventTopic = "0x49628fd1471006c1482da88028e9ce4dbb080b815c9b0344d39e5a8e6ec1419f"
	handleOpsSelector       = []byte{0x76, 0x5e, 0x82, 0x7f}
	executeSelector         = []byte{0xb6, 0x1d, 0x27, 0xf6}
)

type decodedEvent struct {
	UserOpHash    []byte
	Sender        []byte
	Paymaster     []byte
	Nonce         string
	Success       bool
	ActualGasCost string
	ActualGasUsed string
	TxHash        []byte
	TxHashHex     string
	BlockNumber   uint64
	LogIndex      int32
}

type operationCall struct {
	Sender   []byte
	Nonce    string
	Target   []byte
	Calldata []byte
}

type operationMeta struct {
	Target   []byte
	Calldata []byte
}

func decodeUserOperationEventLog(log rpcLog) (decodedEvent, error) {
	if len(log.Topics) != 4 {
		return decodedEvent{}, fmt.Errorf("expected 4 topics, got %d", len(log.Topics))
	}

	if !strings.EqualFold(log.Topics[0], userOperationEventTopic) {
		return decodedEvent{}, fmt.Errorf("unexpected topic0 %q", log.Topics[0])
	}

	userOpHash, err := decodeHexFixed(log.Topics[1], 32)
	if err != nil {
		return decodedEvent{}, fmt.Errorf("decode userOpHash topic: %w", err)
	}

	sender, err := decodeIndexedAddress(log.Topics[2])
	if err != nil {
		return decodedEvent{}, fmt.Errorf("decode sender topic: %w", err)
	}

	paymaster, err := decodeIndexedAddress(log.Topics[3])
	if err != nil {
		return decodedEvent{}, fmt.Errorf("decode paymaster topic: %w", err)
	}

	data, err := decodeHex(log.Data)
	if err != nil {
		return decodedEvent{}, fmt.Errorf("decode log data: %w", err)
	}
	if len(data) != 32*4 {
		return decodedEvent{}, fmt.Errorf("expected 128 bytes of log data, got %d", len(data))
	}

	nonce := uint256ToDecimal(data[0:32])
	success := !isZeroWord(data[32:64])
	actualGasCost := uint256ToDecimal(data[64:96])
	actualGasUsed := uint256ToDecimal(data[96:128])

	txHash, err := decodeHexFixed(log.TransactionHash, 32)
	if err != nil {
		return decodedEvent{}, fmt.Errorf("decode transaction hash: %w", err)
	}

	blockNumber, err := parseHexUint64(log.BlockNumber)
	if err != nil {
		return decodedEvent{}, fmt.Errorf("parse block number: %w", err)
	}

	logIndexValue, err := parseHexUint64(log.LogIndex)
	if err != nil {
		return decodedEvent{}, fmt.Errorf("parse log index: %w", err)
	}
	if logIndexValue > math.MaxInt32 {
		return decodedEvent{}, fmt.Errorf("log index %d overflows int32", logIndexValue)
	}

	return decodedEvent{
		UserOpHash:    userOpHash,
		Sender:        sender,
		Paymaster:     paymaster,
		Nonce:         nonce,
		Success:       success,
		ActualGasCost: actualGasCost,
		ActualGasUsed: actualGasUsed,
		TxHash:        txHash,
		TxHashHex:     "0x" + hex.EncodeToString(txHash),
		BlockNumber:   blockNumber,
		LogIndex:      int32(logIndexValue),
	}, nil
}

func decodeHandleOpsInput(input []byte) ([]operationCall, error) {
	if len(input) < 4+64 {
		return nil, fmt.Errorf("input too short: %d", len(input))
	}
	if !bytesEqual(input[:4], handleOpsSelector) {
		return nil, fmt.Errorf("unexpected selector 0x%s", hex.EncodeToString(input[:4]))
	}

	args := input[4:]

	opsOffsetWord, err := wordAt(args, 0)
	if err != nil {
		return nil, fmt.Errorf("read ops offset: %w", err)
	}
	opsOffset, err := wordToOffset(opsOffsetWord)
	if err != nil {
		return nil, fmt.Errorf("parse ops offset: %w", err)
	}
	if opsOffset+32 > len(args) {
		return nil, fmt.Errorf("ops offset %d out of bounds", opsOffset)
	}

	arrayStart := opsOffset
	arrayLenWord, err := wordAt(args, arrayStart)
	if err != nil {
		return nil, fmt.Errorf("read ops length: %w", err)
	}
	arrayLen, err := wordToOffset(arrayLenWord)
	if err != nil {
		return nil, fmt.Errorf("parse ops length: %w", err)
	}

	offsetsBase := arrayStart + 32
	requiredOffsetsEnd := offsetsBase + arrayLen*32
	if requiredOffsetsEnd > len(args) {
		return nil, fmt.Errorf("ops offsets exceed calldata length")
	}

	result := make([]operationCall, 0, arrayLen)
	for i := 0; i < arrayLen; i++ {
		offsetWord, err := wordAt(args, offsetsBase+i*32)
		if err != nil {
			return nil, fmt.Errorf("read op[%d] offset: %w", i, err)
		}
		relOffset, err := wordToOffset(offsetWord)
		if err != nil {
			return nil, fmt.Errorf("parse op[%d] offset: %w", i, err)
		}

		tupleStart := offsetsBase + relOffset
		tuple, err := parsePackedUserOperation(args, tupleStart)
		if err != nil {
			return nil, fmt.Errorf("decode op[%d]: %w", i, err)
		}

		target, calldata, ok := extractExecuteTargetAndCalldata(tuple.Calldata)
		if !ok {
			continue
		}

		result = append(result, operationCall{
			Sender:   tuple.Sender,
			Nonce:    tuple.Nonce,
			Target:   target,
			Calldata: calldata,
		})
	}

	return result, nil
}

type packedUserOperation struct {
	Sender   []byte
	Nonce    string
	Calldata []byte
}

func parsePackedUserOperation(args []byte, tupleStart int) (packedUserOperation, error) {
	const tupleHeadSize = 9 * 32
	if tupleStart < 0 || tupleStart+tupleHeadSize > len(args) {
		return packedUserOperation{}, fmt.Errorf("tuple head out of bounds")
	}

	senderWord, err := wordAt(args, tupleStart)
	if err != nil {
		return packedUserOperation{}, err
	}
	nonceWord, err := wordAt(args, tupleStart+32)
	if err != nil {
		return packedUserOperation{}, err
	}
	callDataOffsetWord, err := wordAt(args, tupleStart+3*32)
	if err != nil {
		return packedUserOperation{}, err
	}

	callDataOffset, err := wordToOffset(callDataOffsetWord)
	if err != nil {
		return packedUserOperation{}, fmt.Errorf("parse callData offset: %w", err)
	}
	callData, err := readDynamicBytes(args, tupleStart, callDataOffset)
	if err != nil {
		return packedUserOperation{}, fmt.Errorf("read callData: %w", err)
	}

	return packedUserOperation{
		Sender:   append([]byte(nil), senderWord[12:32]...),
		Nonce:    uint256ToDecimal(nonceWord),
		Calldata: callData,
	}, nil
}

func extractExecuteTargetAndCalldata(callData []byte) ([]byte, []byte, bool) {
	const (
		selectorSize   = 4
		minCallDataLen = selectorSize + 4*32
	)

	if len(callData) < minCallDataLen {
		return nil, nil, false
	}
	if !bytesEqual(callData[:selectorSize], executeSelector) {
		return nil, nil, false
	}

	valueWord := callData[selectorSize+32 : selectorSize+64]
	if !isZeroWord(valueWord) {
		return nil, nil, false
	}

	target := append([]byte(nil), callData[selectorSize+12:selectorSize+32]...)
	offsetWord := callData[selectorSize+2*32 : selectorSize+3*32]
	offset, err := wordToOffset(offsetWord)
	if err != nil {
		return nil, nil, false
	}

	bytesHead := selectorSize + offset
	if bytesHead+32 > len(callData) {
		return nil, nil, false
	}

	lenWord := callData[bytesHead : bytesHead+32]
	lenValue, err := wordToOffset(lenWord)
	if err != nil {
		return nil, nil, false
	}

	dataStart := bytesHead + 32
	dataEnd := dataStart + lenValue
	if dataEnd > len(callData) {
		return nil, nil, false
	}

	inner := append([]byte(nil), callData[dataStart:dataEnd]...)
	return target, inner, true
}

func toOperationMetaQueue(calls []operationCall) map[string][]operationMeta {
	queue := make(map[string][]operationMeta)
	for i := range calls {
		call := calls[i]
		key := operationKey(call.Sender, call.Nonce)
		queue[key] = append(queue[key], operationMeta{
			Target:   call.Target,
			Calldata: call.Calldata,
		})
	}
	return queue
}

func decodeIndexedAddress(topic string) ([]byte, error) {
	decoded, err := decodeHexFixed(topic, 32)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), decoded[12:32]...), nil
}

func readDynamicBytes(data []byte, base int, offset int) ([]byte, error) {
	if offset < 0 {
		return nil, fmt.Errorf("negative offset")
	}
	start := base + offset
	if start+32 > len(data) {
		return nil, fmt.Errorf("offset %d out of bounds", offset)
	}

	lenWord, err := wordAt(data, start)
	if err != nil {
		return nil, err
	}
	lenValue, err := wordToOffset(lenWord)
	if err != nil {
		return nil, fmt.Errorf("parse bytes length: %w", err)
	}

	bytesStart := start + 32
	bytesEnd := bytesStart + lenValue
	if bytesEnd > len(data) {
		return nil, fmt.Errorf("bytes end %d out of bounds", bytesEnd)
	}

	return append([]byte(nil), data[bytesStart:bytesEnd]...), nil
}

func wordAt(data []byte, offset int) ([]byte, error) {
	if offset < 0 || offset+32 > len(data) {
		return nil, fmt.Errorf("word offset %d out of bounds", offset)
	}
	return data[offset : offset+32], nil
}

func wordToOffset(word []byte) (int, error) {
	v := new(big.Int).SetBytes(word)
	if v.Sign() < 0 {
		return 0, fmt.Errorf("negative value")
	}
	if !v.IsUint64() {
		return 0, fmt.Errorf("value overflows uint64")
	}
	u := v.Uint64()
	if u > uint64(math.MaxInt) {
		return 0, fmt.Errorf("value overflows int")
	}
	return int(u), nil
}

func uint256ToDecimal(word []byte) string {
	v := new(big.Int).SetBytes(word)
	return v.String()
}

func operationKey(sender []byte, nonce string) string {
	return hex.EncodeToString(sender) + ":" + nonce
}

func isZeroWord(word []byte) bool {
	for _, b := range word {
		if b != 0 {
			return false
		}
	}
	return true
}

func bytesEqual(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
