package lottery

import (
	"crypto/rand"
	"fmt"
	"time"
)

// ValidateRange validates lottery range parameters
func ValidateRange(min, max int) error {
	if min > max {
		return ErrInvalidRange
	}
	return nil
}

// ValidateCount validates count parameter for multiple draws
func ValidateCount(count int) error {
	if count <= 0 {
		return ErrInvalidCount
	}
	return nil
}

// generateLockValue generates a unique lock value using crypto/rand
func generateLockValue() string {
	// Generate 16 random bytes
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		// Fallback to timestamp-based value if crypto/rand fails
		return fmt.Sprintf("lock_%d", time.Now().UnixNano())
	}

	// Convert to hex string
	const hexChars = "0123456789abcdef"
	result := make([]byte, 32)
	for i, b := range bytes {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0x0f]
	}

	return string(result)
}

// calculateOptimalBatchSize determines the optimal batch size based on the total count
func calculateOptimalBatchSize(totalCount int) int {
	// Use a heuristic to determine batch size
	// For small counts, use smaller batches to reduce memory usage
	// For large counts, use larger batches to improve performance

	if totalCount <= 10 {
		return 1 // No batching for very small counts
	} else if totalCount <= 100 {
		return 10 // Small batches for moderate counts
	} else if totalCount <= 1000 {
		return 50 // Medium batches for large counts
	} else {
		return 100 // Large batches for very large counts
	}
}
