package numconv

import (
	"fmt"
	"math"
)

func Uint64ToInt64(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("value %d overflows int64", value)
	}

	return int64(value), nil
}
