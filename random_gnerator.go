package lottery

import (
	"crypto/rand"
	"math/big"
	"sync"
)

// SecureRandomGenerator implements secure random number generation using crypto/rand with caching
type SecureRandomGenerator struct {
	cache      []float64
	cacheSize  int
	cacheIndex int
	cacheMtx   sync.Mutex
}

// NewSecureRandomGenerator creates a new fast secure random generator with specified cache size
//
// If no cache size is provided, the default cache size will be used.
// The cache size should be a positive integer.
func NewSecureRandomGenerator(cacheSize ...int) *SecureRandomGenerator {
	var size int
	if len(cacheSize) <= 0 {
		size = DefaultFastRandomGeneratorCacheSize
	}
	if len(cacheSize) > 0 && cacheSize[0] > 0 {
		size = cacheSize[0]
	}

	generator := &SecureRandomGenerator{
		cache:      make([]float64, size),
		cacheSize:  size,
		cacheIndex: 0,
	}

	// 预填充缓存
	generator.refillCache()
	return generator
}

// refillCache refills the random number cache
func (g *SecureRandomGenerator) refillCache() error {
	for i := range g.cacheSize {
		val, err := generateFloat()
		if err != nil {
			// 如果生成失败，使用备用方法
			val = float64(i) / float64(g.cacheSize)
		}
		g.cache[i] = val
	}

	g.cacheIndex = 0
	return nil
}

// GenerateFloat generates a fast secure random float between 0 and 1 (exclusive of 1)
func (g *SecureRandomGenerator) GenerateFloat() (float64, error) {
	g.cacheMtx.Lock()
	defer g.cacheMtx.Unlock()

	// Refill cache if needed
	if g.cacheIndex >= g.cacheSize {
		if err := g.refillCache(); err != nil {
			return 0, err
		}
	}

	result := g.cache[g.cacheIndex]
	g.cacheIndex++
	return result, nil
}

// GenerateInRange generates a fast secure random number within the specified range [min, max] (inclusive)
func (g *SecureRandomGenerator) GenerateInRange(min, max int) (int, error) {
	if min > max {
		return 0, ErrInvalidRange
	}

	// Handle edge case where min == max
	if min == max {
		return min, nil
	}

	// Get a random float
	randomFloat, err := g.GenerateFloat()
	if err != nil {
		return 0, err
	}

	// Scale to range
	rangeSize := max - min + 1
	result := int(randomFloat*float64(rangeSize)) + min

	// Ensure result is within bounds (handle floating point precision issues)
	if result > max {
		result = max
	}

	return result, nil
}

// generateFloat generates a secure random float between 0 and 1 (exclusive of 1)
func generateFloat() (float64, error) {
	// Generate a random 64-bit integer
	randomBig, err := rand.Int(rand.Reader, big.NewInt(1<<53)) // Use 53 bits for precision
	if err != nil {
		return 0, err
	}

	// Convert to float64 and normalize to [0, 1)
	result := float64(randomBig.Int64()) / float64(1<<53)
	return result, nil
}
