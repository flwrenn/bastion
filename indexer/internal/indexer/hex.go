package indexer

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func normalizeAddress(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "0x") {
		return "", fmt.Errorf("address must start with 0x")
	}

	hexPart := trimmed[2:]
	if len(hexPart) != 40 {
		return "", fmt.Errorf("address must be 20 bytes, got %d hex chars", len(hexPart))
	}

	if _, err := hex.DecodeString(hexPart); err != nil {
		return "", fmt.Errorf("invalid address hex: %w", err)
	}

	return "0x" + strings.ToLower(hexPart), nil
}

func decodeHex(input string) ([]byte, error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "0x") {
		return nil, fmt.Errorf("hex value must start with 0x")
	}

	hexPart := trimmed[2:]
	if len(hexPart)%2 != 0 {
		return nil, fmt.Errorf("hex value has odd length")
	}

	decoded, err := hex.DecodeString(hexPart)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}

	return decoded, nil
}

func decodeHexFixed(input string, length int) ([]byte, error) {
	decoded, err := decodeHex(input)
	if err != nil {
		return nil, err
	}

	if len(decoded) != length {
		return nil, fmt.Errorf("expected %d bytes, got %d", length, len(decoded))
	}

	return decoded, nil
}

func parseHexUint64(input string) (uint64, error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "0x") {
		return 0, fmt.Errorf("hex uint64 must start with 0x")
	}

	value, err := strconv.ParseUint(trimmed[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parse hex uint64: %w", err)
	}

	return value, nil
}

func toInt64(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("value %d overflows int64", value)
	}

	return int64(value), nil
}
