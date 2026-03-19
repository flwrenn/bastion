package db

// UserOperation represents a row in the user_operations table.
// Fields map to the EntryPoint v0.7 UserOperationEvent plus
// transaction-level context (tx hash, block, timestamp, log index).
type UserOperation struct {
	ID             int64
	UserOpHash     []byte
	Sender         []byte
	Paymaster      []byte
	Nonce          string // numeric — stored as string to handle uint256
	Success        bool
	ActualGasCost  string // numeric
	ActualGasUsed  string // numeric
	TxHash         []byte
	BlockNumber    int64
	BlockTimestamp int64
	LogIndex       int32
}
