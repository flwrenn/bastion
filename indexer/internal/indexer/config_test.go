package indexer

import "testing"

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
