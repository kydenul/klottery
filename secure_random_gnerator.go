package lottery

import (
	"crypto/rand"
	"math/big"
)

// SecureRandomGenerator implements secure random number generation using crypto/rand
type SecureRandomGenerator struct{}

// NewSecureRandomGenerator creates a new secure random generator
func NewSecureRandomGenerator() *SecureRandomGenerator {
	return &SecureRandomGenerator{}
}

// GenerateInRange generates a secure random number within the specified range [min, max] (inclusive)
func (g *SecureRandomGenerator) GenerateInRange(min, max int) (int, error) {
	if min > max {
		return 0, ErrInvalidRange
	}

	// Handle edge case where min == max
	if min == max {
		return min, nil
	}

	// Calculate the range size
	rangeSize := max - min + 1

	// Generate a random number in the range [0, rangeSize)
	randomBig, err := rand.Int(rand.Reader, big.NewInt(int64(rangeSize)))
	if err != nil {
		return 0, err
	}

	// Convert to int and add min to get the final result
	result := int(randomBig.Int64()) + min
	return result, nil
}

// GenerateFloat generates a secure random float between 0 and 1 (exclusive of 1)
func (g *SecureRandomGenerator) GenerateFloat() (float64, error) {
	// Generate a random 64-bit integer
	randomBig, err := rand.Int(rand.Reader, big.NewInt(1<<53)) // Use 53 bits for precision
	if err != nil {
		return 0, err
	}

	// Convert to float64 and normalize to [0, 1)
	result := float64(randomBig.Int64()) / float64(1<<53)
	return result, nil
}

// GenerateSecureInRange is a standalone function for generating secure random numbers in range
func GenerateSecureInRange(min, max int) (int, error) {
	return NewSecureRandomGenerator().GenerateInRange(min, max)
}

// GenerateSecureFloat is a standalone function for generating secure random floats
func GenerateSecureFloat() (float64, error) {
	return NewSecureRandomGenerator().GenerateFloat()
}
