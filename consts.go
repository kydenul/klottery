package lottery

import "time"

const (
	// DefaultLockTimeout is the default timeout for acquiring distributed locks
	DefaultLockTimeout = 30 * time.Second

	// DefaultRetryAttempts is the default number of retry attempts
	DefaultRetryAttempts = 3

	// DefaultRetryInterval is the default interval between retry attempts
	DefaultRetryInterval = 100 * time.Millisecond

	// DefaultLockCacheTTL is the default TTL for cache of distributed locks
	DefaultLockCacheTTL = 1 * time.Second

	// LockKeyPrefix is the prefix for Redis lock keys
	LockKeyPrefix = "lottery:lock:"

	// CounterKeyPrefix is the prefix for Redis counter keys
	CounterKeyPrefix = "lottery:counter:"

	// DefaultLockExpiration is the default expiration time for locks
	DefaultLockExpiration = 30 * time.Second

	// MaxRetryAttempts is the maximum number of retry attempts allowed
	MaxRetryAttempts = 10

	// MinLockTimeout is the minimum lock timeout allowed
	MinLockTimeout = 1 * time.Second

	// MaxLockTimeout is the maximum lock timeout allowed
	MaxLockTimeout = 5 * time.Minute

	// MinLockCacheTTL is the minimum TTL for lock cache
	MinLockCacheTTL = 1 * time.Second

	// MaxLockCacheTTL is the maximum TTL for lock cache
	MaxLockCacheTTL = 5 * time.Minute

	// ProbabilityTolerance is the tolerance for probability sum validation
	ProbabilityTolerance = 0.0001
)

const (
	// DefaultCircuitBreakerName is the default name for Circuit Breaker
	DefaultCircuitBreakerName = "lottery-engine"

	// DefaultCircuitBreakerMaxRequests is the default max requests
	DefaultCircuitBreakerMaxRequests = 3

	// DefaultCircuitBreakerInterval is the default interval
	DefaultCircuitBreakerInterval = 60 * time.Second

	// DefaultCircuitBreakerTimeout is the default timeout
	DefaultCircuitBreakerTimeout = 30 * time.Second

	// DefaultCircuitBreakerFailureRatio is the default failure ratio
	DefaultCircuitBreakerFailureRatio = 0.6

	// DefaultCircuitBreakerMinRequests is the default min requests
	DefaultCircuitBreakerMinRequests = 3

	// DefaultCircuitBreakerOnStateChange is the default on state change
	DefaultCircuitBreakerOnStateChange = true
)

const (
	DefaultRedisAddr         = "localhost:6379"
	DefaultRedisPassword     = ""
	DefaultRedisDB           = 0
	DefaultRedisPoolSize     = 50
	DefaultRedisMinIdleConns = 10
	DefaultRedisMaxRetries   = 3
	DefaultRedisDialTimeout  = 5 * time.Second
	DefaultRedisReadTimeout  = 3 * time.Second
	DefaultRedisWriteTimeout = 3 * time.Second
	DefaultRedisPoolTimeout  = 4 * time.Second
	DefaultRedisClusterMode  = false
	DefaultRedisTLSEnabled   = false
)
