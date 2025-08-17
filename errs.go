package lottery

import "errors"

// Error codes and messages for the lottery system
var (
	// ErrActivityNotFound indicates the lottery activity does not exist
	ErrActivityNotFound = errors.New("LOTTERY_001: activity not found")

	// ErrActivityEnded indicates the lottery activity has ended
	ErrActivityEnded = errors.New("LOTTERY_002: activity has ended")

	// ErrUserLimitReached indicates the user has reached the lottery limit
	ErrUserLimitReached = errors.New("LOTTERY_003: user has reached lottery limit")

	// ErrInsufficientPrizes indicates insufficient prize inventory
	ErrInsufficientPrizes = errors.New("LOTTERY_004: insufficient prize inventory")

	// ErrLockAcquisitionFailed indicates failed to acquire distributed lock
	ErrLockAcquisitionFailed = errors.New("LOTTERY_005: failed to acquire distributed lock")

	// ErrRedisConnectionFailed indicates Redis connection failure
	ErrRedisConnectionFailed = errors.New("LOTTERY_006: Redis connection failed")

	// ErrInvalidParameters indicates parameter validation failure
	ErrInvalidParameters = errors.New("LOTTERY_007: parameter validation failed")

	// ErrInvalidRange indicates invalid lottery range parameters
	ErrInvalidRange = errors.New("invalid range: min must be less than or equal to max")

	// ErrInvalidCount indicates invalid count parameter
	ErrInvalidCount = errors.New("invalid count: must be greater than 0")

	// ErrInvalidProbability indicates invalid probability values in prize pool
	ErrInvalidProbability = errors.New("invalid probability: probabilities must sum to 1.0")

	// ErrEmptyPrizePool indicates empty prize pool
	ErrEmptyPrizePool = errors.New("empty prize pool")

	// ErrLockTimeout indicates lock acquisition timeout
	ErrLockTimeout = errors.New("lock acquisition timeout")

	// ErrInvalidPrizeID indicates invalid or empty prize ID
	ErrInvalidPrizeID = errors.New("invalid prize ID: cannot be empty")

	// ErrInvalidPrizeName indicates invalid or empty prize name
	ErrInvalidPrizeName = errors.New("invalid prize name: cannot be empty")

	// ErrNegativePrizeValue indicates negative prize value
	ErrNegativePrizeValue = errors.New("invalid prize value: cannot be negative")

	// ErrInvalidLockTimeout indicates invalid lock timeout configuration
	ErrInvalidLockTimeout = errors.New("invalid lock timeout: must be between 1s and 5m")

	// ErrInvalidRetryAttempts indicates invalid retry attempts configuration
	ErrInvalidRetryAttempts = errors.New("invalid retry attempts: must be between 0 and 10")

	// ErrInvalidRetryInterval indicates invalid retry interval configuration
	ErrInvalidRetryInterval = errors.New("invalid retry interval: cannot be negative")

	// ErrPartialDrawFailure indicates that some draws in a multi-draw operation failed
	ErrPartialDrawFailure = errors.New("partial draw failure: some draws completed successfully, some failed")

	// ErrDrawStateCorrupted indicates that the draw state is corrupted or invalid
	ErrDrawStateCorrupted = errors.New("draw state corrupted: invalid state data")

	// ErrRollbackFailed indicates that rollback operation failed
	ErrRollbackFailed = errors.New("rollback failed: unable to revert partial changes")

	// ErrDrawInterrupted indicates that the draw operation was interrupted
	ErrDrawInterrupted = errors.New("draw operation interrupted")
)
