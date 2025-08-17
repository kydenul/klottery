package lottery

import "time"

const (
	// DefaultLockTimeout is the default timeout for acquiring distributed locks
	DefaultLockTimeout = 30 * time.Second

	// DefaultRetryAttempts is the default number of retry attempts
	DefaultRetryAttempts = 3

	// DefaultRetryInterval is the default interval between retry attempts
	DefaultRetryInterval = 100 * time.Millisecond

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

	// ProbabilityTolerance is the tolerance for probability sum validation
	ProbabilityTolerance = 0.0001
)
