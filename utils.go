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
