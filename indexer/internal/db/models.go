package db

// UserOperation represents a row in the user_operations table.
// Fields map to the EntryPoint v0.7 UserOperationEvent plus
// decoded inner-call fields (target, calldata) and transaction-
// level context (tx hash, block, timestamp, log index).
type UserOperation struct {
	ID             int64
	UserOpHash     []byte
	Sender         []byte
	Paymaster      []byte
	Target         []byte
	Calldata       []byte
	Nonce          string // DB NUMERIC — Go string for uint256 range
	Success        bool
	ActualGasCost  string // DB NUMERIC — Go string for uint256 range
	ActualGasUsed  string // DB NUMERIC — Go string for uint256 range
	TxHash         []byte
	BlockNumber    int64
	BlockTimestamp int64
	LogIndex       int32
}
