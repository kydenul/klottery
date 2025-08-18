package lottery

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

// Distributed Lock Implementation Strategy:
// - Lock Acquisition: Use Redis SET NX for optimal performance (single network call)
// - Lock Release: Use Lua script for safety (ensures only lock owner can release)
// This hybrid approach provides both high performance and security.

// Lua script for atomic lock release (only release needs Lua script for safety)
const (
	// releaseLockScript ensures only the lock owner can release the lock
	// This prevents the dangerous scenario where:
	// 1. Client A's lock expires
	// 2. Client B acquires the lock
	// 3. Client A tries to release lock and accidentally deletes Client B's lock
	releaseLockScript = `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`
)

// DistributedLockManager manages Redis distributed locks
type DistributedLockManager struct {
	redisClient   *redis.Client
	lockTimeout   time.Duration
	retryAttempts int
	retryInterval time.Duration
	lockCacheTTL  time.Duration // 锁缓存TTL

	performanceMonitor *PerformanceMonitor
}

// NewLockManager creates a new distributed lock manager
func NewLockManager(redisClient *redis.Client, lockTimeout time.Duration) *DistributedLockManager {
	return &DistributedLockManager{
		redisClient:   redisClient,
		lockTimeout:   lockTimeout,
		retryAttempts: DefaultRetryAttempts,
		retryInterval: DefaultRetryInterval,
		lockCacheTTL:  DefaultLockCacheTTL,

		performanceMonitor: NewPerformanceMonitor(),
	}
}

// NewLockManagerWithRetry creates a new distributed lock manager with custom retry settings
func NewLockManagerWithRetry(
	redisClient *redis.Client,
	lockTimeout time.Duration, retryAttempts int, retryInterval, lockCacheTTL time.Duration,
) *DistributedLockManager {
	return &DistributedLockManager{
		redisClient:   redisClient,
		lockTimeout:   lockTimeout,
		retryAttempts: retryAttempts,
		retryInterval: retryInterval,
		lockCacheTTL:  lockCacheTTL,

		performanceMonitor: NewPerformanceMonitor(),
	}
}

// NewBatchDistributedLockManager creates a new batch lock manager
func NewBatchLockManager(
	redisClient *redis.Client,
	lockTimeout time.Duration, retryAttempts int, retryInterval, lockCacheTTL time.Duration,
) *DistributedLockManager {
	return &DistributedLockManager{
		redisClient:   redisClient,
		lockTimeout:   lockTimeout,
		retryAttempts: retryAttempts,
		retryInterval: retryInterval,
		lockCacheTTL:  lockCacheTTL,

		performanceMonitor: NewPerformanceMonitor(),
	}
}

// AcquireLock attempts to acquire a distributed lock using SET NX for optimal performance
func (m *DistributedLockManager) AcquireLock(ctx context.Context, lockKey, lockValue string, expireTime time.Duration) (bool, error) {
	if lockKey == "" {
		return false, ErrInvalidParameters
	}
	if lockValue == "" {
		return false, ErrInvalidParameters
	}
	if expireTime <= 0 {
		expireTime = DefaultLockExpiration
	}

	// Add prefix to lock key
	fullLockKey := LockKeyPrefix + lockKey

	// Try to acquire lock with retry mechanism
	for attempt := 0; attempt <= m.retryAttempts; attempt++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		// Use Redis SET NX command for atomic lock acquisition (high performance)
		acquired, err := m.redisClient.SetNX(ctx, fullLockKey, lockValue, expireTime).Result()
		if err != nil {
			// If this is the last attempt, return the error
			if attempt == m.retryAttempts {
				return false, ErrRedisConnectionFailed
			}
			// Wait before retrying
			time.Sleep(m.retryInterval)
			continue
		}

		// Check if lock was acquired successfully
		if acquired {
			return true, nil
		}

		// If lock acquisition failed and this is not the last attempt, wait and retry
		if attempt < m.retryAttempts {
			time.Sleep(m.retryInterval)
		}
	}

	// All attempts failed
	return false, ErrLockAcquisitionFailed
}

func (m *DistributedLockManager) ReleaseLock(ctx context.Context, lockKey, lockValue string) (bool, error) {
	if lockKey == "" {
		return false, ErrInvalidParameters
	}
	if lockValue == "" {
		return false, ErrInvalidParameters
	}

	// Add prefix to lock key
	fullLockKey := LockKeyPrefix + lockKey

	// Try to release lock with retry mechanism
	for attempt := 0; attempt <= m.retryAttempts; attempt++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		// Execute Lua script for atomic lock release
		result, err := m.redisClient.Eval(ctx, releaseLockScript, []string{fullLockKey}, lockValue).Result()
		if err != nil {
			// If this is the last attempt, return the error
			if attempt == m.retryAttempts {
				return false, ErrRedisConnectionFailed
			}
			// Wait before retrying
			time.Sleep(m.retryInterval)
			continue
		}

		// Check if lock was released successfully
		if result.(int64) == 1 {
			return true, nil
		}

		// Lock was not found or value didn't match - no need to retry
		return false, nil
	}

	// This should not be reached, but included for completeness
	return false, ErrRedisConnectionFailed
}

// AcquireLockWithTimeout attempts to acquire a lock with a timeout
func (m *DistributedLockManager) AcquireLockWithTimeout(ctx context.Context, lockKey, lockValue string, expireTime, timeout time.Duration) (bool, error) {
	if lockKey == "" {
		return false, ErrInvalidParameters
	}
	if lockValue == "" {
		return false, ErrInvalidParameters
	}
	if expireTime <= 0 {
		expireTime = DefaultLockExpiration
	}
	if timeout <= 0 {
		timeout = m.lockTimeout
	}

	// Create a context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Add prefix to lock key
	fullLockKey := LockKeyPrefix + lockKey

	// Keep trying until timeout or success
	for {
		select {
		case <-timeoutCtx.Done():
			return false, ErrLockTimeout
		default:
		}

		// Use Redis SET NX command for lock acquisition
		acquired, err := m.redisClient.SetNX(timeoutCtx, fullLockKey, lockValue, expireTime).Result()
		if err != nil {
			// Check if it's a timeout error
			if timeoutCtx.Err() != nil {
				return false, ErrLockTimeout
			}
			// For other Redis errors, wait and retry
			time.Sleep(m.retryInterval)
			continue
		}

		// Check if lock was acquired successfully
		if acquired {
			return true, nil
		}

		// Lock is held by someone else, wait and retry
		time.Sleep(m.retryInterval)
	}
}

// TryAcquireLock attempts to acquire a lock without retries (single attempt)
func (m *DistributedLockManager) TryAcquireLock(ctx context.Context, lockKey, lockValue string, expireTime time.Duration) (bool, error) {
	if lockKey == "" {
		return false, ErrInvalidParameters
	}
	if lockValue == "" {
		return false, ErrInvalidParameters
	}
	if expireTime <= 0 {
		expireTime = DefaultLockExpiration
	}

	// Add prefix to lock key
	fullLockKey := LockKeyPrefix + lockKey

	// Use Redis SET NX command for single attempt lock acquisition
	acquired, err := m.redisClient.SetNX(ctx, fullLockKey, lockValue, expireTime).Result()
	if err != nil {
		return false, ErrRedisConnectionFailed
	}

	return acquired, nil
}

// AcquireMultipleLocks attempts to acquire multiple locks in a batch using Redis pipeline
func (m *DistributedLockManager) AcquireMultipleLocks(ctx context.Context, lockKeys, lockValues []string, expireTime time.Duration) ([]bool, error) {
	if len(lockKeys) != len(lockValues) {
		return nil, ErrInvalidParameters
	}
	if len(lockKeys) == 0 {
		return []bool{}, nil
	}
	if expireTime <= 0 {
		expireTime = DefaultLockExpiration
	}

	// Validate parameters
	for i, key := range lockKeys {
		if key == "" || lockValues[i] == "" {
			return nil, ErrInvalidParameters
		}
	}

	results := make([]bool, len(lockKeys))

	// Use Redis pipeline for batch operations
	pipe := m.redisClient.Pipeline()

	// Add all SET NX commands to pipeline
	cmds := make([]*redis.BoolCmd, len(lockKeys))
	for i, key := range lockKeys {
		fullLockKey := LockKeyPrefix + key
		cmds[i] = pipe.SetNX(ctx, fullLockKey, lockValues[i], expireTime)
	}

	// Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, ErrRedisConnectionFailed
	}

	// Collect results
	for i, cmd := range cmds {
		acquired, err := cmd.Result()
		if err != nil {
			results[i] = false
		} else {
			results[i] = acquired
		}
	}

	return results, nil
}

// ReleaseMultipleLocks releases multiple locks in a batch using Lua script
func (m *DistributedLockManager) ReleaseMultipleLocks(ctx context.Context, lockKeys, lockValues []string) ([]bool, error) {
	if len(lockKeys) != len(lockValues) {
		return nil, ErrInvalidParameters
	}
	if len(lockKeys) == 0 {
		return []bool{}, nil
	}

	// Validate parameters
	for i, key := range lockKeys {
		if key == "" || lockValues[i] == "" {
			return nil, ErrInvalidParameters
		}
	}

	results := make([]bool, len(lockKeys))

	// For batch release, we'll use individual releases for safety
	// In a production system, you might want to use a more complex Lua script
	for i, key := range lockKeys {
		released, err := m.ReleaseLock(ctx, key, lockValues[i])
		if err != nil {
			results[i] = false
		} else {
			results[i] = released
		}
	}

	return results, nil
}

// GetPerformanceMetrics 获取性能指标
func (m *DistributedLockManager) GetPerformanceMetrics() PerformanceMetrics {
	return m.performanceMonitor.GetMetrics()
}

// AcquireMultipleLocksWithMonitoring 带性能监控的批量获取多个锁
func (m *DistributedLockManager) AcquireMultipleLocksWithMonitoring(ctx context.Context, lockKeys, lockValues []string, expireTime time.Duration) ([]bool, error) {
	startTime := time.Now()

	// 调用基础的批量锁获取方法
	results, err := m.AcquireMultipleLocks(ctx, lockKeys, lockValues, expireTime)

	duration := time.Since(startTime)

	// 记录性能指标
	if err != nil {
		m.performanceMonitor.RecordRedisError()
	} else if len(lockKeys) > 0 {
		avgDuration := duration / time.Duration(len(lockKeys))
		for _, result := range results {
			m.performanceMonitor.RecordLockAcquisition(result, avgDuration)
		}
	}

	return results, err
}

// ReleaseMultipleLocksWithMonitoring 带性能监控的批量释放多个锁
func (m *DistributedLockManager) ReleaseMultipleLocksWithMonitoring(ctx context.Context, lockKeys, lockValues []string) ([]bool, error) {
	// 调用基础的批量锁释放方法
	results, err := m.ReleaseMultipleLocks(ctx, lockKeys, lockValues)

	// 记录性能指标
	if err != nil {
		m.performanceMonitor.RecordRedisError()
	} else {
		for range results {
			m.performanceMonitor.RecordLockRelease()
		}
	}

	return results, err
}

// SetPerformanceMonitor 设置性能监控器
func (m *DistributedLockManager) SetPerformanceMonitor(monitor *PerformanceMonitor) {
	m.performanceMonitor = monitor
}
