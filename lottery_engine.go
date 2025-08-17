package lottery

import (
	"context"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// LotteryEngine provides thread-safe lottery functionality
type LotteryEngine struct {
	redisClient *redis.Client
	lockManager *DistributedLockManager
	config      *LotteryConfig
	logger      Logger
	mu          sync.RWMutex // 保护配置和lockManager的并发访问

	performanceMonitor *PerformanceMonitor
	lockCache          sync.Map      // 锁缓存，用于快速路径优化
	lockCacheTTL       time.Duration // 锁缓存TTL
}

// NewLotteryEngine creates a new lottery engine with the given Redis client
func NewLotteryEngine(redisClient *redis.Client) *LotteryEngine {
	config := &LotteryConfig{
		LockTimeout:   30 * time.Second,
		RetryAttempts: 3,
		RetryInterval: 100 * time.Millisecond,
	}

	return &LotteryEngine{
		redisClient: redisClient,
		lockManager: NewLockManagerWithRetry(
			redisClient,
			config.LockTimeout,
			config.RetryAttempts,
			config.RetryInterval,
		),
		config: config,
		logger: &DefaultLogger{},

		performanceMonitor: NewPerformanceMonitor(),
		lockCacheTTL:       1 * time.Second, // 1秒的锁缓存TTL
	}
}

// NewLotteryEngineWithConfig creates a new lottery engine with custom configuration
func NewLotteryEngineWithConfig(redisClient *redis.Client, config *LotteryConfig) *LotteryEngine {
	return &LotteryEngine{
		redisClient: redisClient,
		lockManager: NewLockManagerWithRetry(
			redisClient,
			config.LockTimeout,
			config.RetryAttempts,
			config.RetryInterval,
		),
		config: config,
		logger: &DefaultLogger{},

		performanceMonitor: NewPerformanceMonitor(),
		lockCacheTTL:       1 * time.Second, // 1秒的锁缓存TTL
	}
}

// NewLotteryEngineWithLogger creates a new lottery engine with custom logger
func NewLotteryEngineWithLogger(redisClient *redis.Client, logger Logger) *LotteryEngine {
	config := &LotteryConfig{
		LockTimeout:   30 * time.Second,
		RetryAttempts: 3,
		RetryInterval: 100 * time.Millisecond,
	}

	lockManager := NewLockManagerWithRetry(
		redisClient,
		config.LockTimeout,
		config.RetryAttempts,
		config.RetryInterval,
	)

	return &LotteryEngine{
		redisClient: redisClient,
		lockManager: lockManager,
		config:      config,
		logger:      logger,

		performanceMonitor: NewPerformanceMonitor(),
		lockCacheTTL:       1 * time.Second, // 1秒的锁缓存TTL
	}
}

// NewLotteryEngineWithConfigAndLogger creates a new lottery engine with custom configuration and logger
func NewLotteryEngineWithConfigAndLogger(
	redisClient *redis.Client, config *LotteryConfig, logger Logger,
) *LotteryEngine {
	lockManager := NewLockManagerWithRetry(
		redisClient,
		config.LockTimeout,
		config.RetryAttempts,
		config.RetryInterval,
	)

	return &LotteryEngine{
		redisClient: redisClient,
		lockManager: lockManager,
		config:      config,
		logger:      logger,

		performanceMonitor: NewPerformanceMonitor(),
		lockCacheTTL:       1 * time.Second, // 1秒的锁缓存TTL
	}
}

// GetConfig returns a copy of the current lottery engine configuration
func (e *LotteryEngine) GetConfig() *LotteryConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return &LotteryConfig{
		LockTimeout:   e.config.LockTimeout,
		RetryAttempts: e.config.RetryAttempts,
		RetryInterval: e.config.RetryInterval,
	}
}

// UpdateConfig updates the lottery engine configuration at runtime
func (e *LotteryEngine) UpdateConfig(newConfig *LotteryConfig) error {
	e.logger.Debug("UpdateConfig called")

	if newConfig == nil {
		e.logger.Error("UpdateConfig failed: nil configuration")
		return ErrInvalidParameters
	}

	// Validate the new configuration
	if err := newConfig.Validate(); err != nil {
		e.logger.Error("UpdateConfig validation failed: %v", err)
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Update the engine configuration
	e.config = &LotteryConfig{
		LockTimeout:   newConfig.LockTimeout,
		RetryAttempts: newConfig.RetryAttempts,
		RetryInterval: newConfig.RetryInterval,
	}

	// Update the lock manager with new configuration
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.config.LockTimeout,
		e.config.RetryAttempts,
		e.config.RetryInterval,
	)

	e.logger.Info("Configuration updated successfully: LockTimeout=%v, RetryAttempts=%d, RetryInterval=%v",
		e.config.LockTimeout, e.config.RetryAttempts, e.config.RetryInterval)
	return nil
}

// SetLockTimeout updates the lock timeout configuration at runtime
func (e *LotteryEngine) SetLockTimeout(timeout time.Duration) error {
	e.logger.Debug("SetLockTimeout called with timeout=%v", timeout)

	if timeout < MinLockTimeout || timeout > MaxLockTimeout {
		e.logger.Error("SetLockTimeout failed: invalid timeout %v (must be between %v and %v)", timeout, MinLockTimeout, MaxLockTimeout)
		return ErrInvalidLockTimeout
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.config.LockTimeout = timeout
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.config.LockTimeout,
		e.config.RetryAttempts,
		e.config.RetryInterval,
	)

	e.logger.Info("Lock timeout updated to %v", timeout)
	return nil
}

// SetRetryAttempts updates the retry attempts configuration at runtime
func (e *LotteryEngine) SetRetryAttempts(attempts int) error {
	e.logger.Debug("SetRetryAttempts called with attempts=%d", attempts)

	if attempts < 0 || attempts > MaxRetryAttempts {
		e.logger.Error("SetRetryAttempts failed: invalid attempts %d (must be between 0 and %d)", attempts, MaxRetryAttempts)
		return ErrInvalidRetryAttempts
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.config.RetryAttempts = attempts
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.config.LockTimeout,
		e.config.RetryAttempts,
		e.config.RetryInterval,
	)

	e.logger.Info("Retry attempts updated to %d", attempts)
	return nil
}

// SetRetryInterval updates the retry interval configuration at runtime
func (e *LotteryEngine) SetRetryInterval(interval time.Duration) error {
	e.logger.Debug("SetRetryInterval called with interval=%v", interval)

	if interval < 0 {
		e.logger.Error("SetRetryInterval failed: invalid interval %v (cannot be negative)", interval)
		return ErrInvalidRetryInterval
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.config.RetryInterval = interval
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.config.LockTimeout,
		e.config.RetryAttempts,
		e.config.RetryInterval,
	)

	e.logger.Info("Retry interval updated to %v", interval)
	return nil
}

// SetLogger updates the logger at runtime
func (e *LotteryEngine) SetLogger(logger Logger) {
	if logger != nil {
		e.logger.Info("Logger updated")
		e.logger = logger
		e.logger.Info("New logger activated")
	}
}

// GetLogger returns the current logger
func (e *LotteryEngine) GetLogger() Logger { return e.logger }

// shouldAbortOnError determines whether a multi-draw operation should be aborted based on the error type
func (e *LotteryEngine) shouldAbortOnError(err error) bool {
	// Critical errors that should abort the operation
	switch err {
	case ErrRedisConnectionFailed:
		return true // Redis connection issues are critical
	case context.DeadlineExceeded:
		return true // Timeout errors should abort
	case context.Canceled:
		return true // Cancellation should abort
	default:
		// Check if it's a context error
		if err == context.DeadlineExceeded || err == context.Canceled {
			return true
		}
		// For other errors like lock acquisition failures, continue trying
		return false
	}
}

// RollbackMultiDraw attempts to rollback a partially completed multi-draw operation
// This is a placeholder for future implementation of rollback logic
func (e *LotteryEngine) RollbackMultiDraw(ctx context.Context, drawState *DrawState) error {
	e.logger.Info("RollbackMultiDraw called for lockKey=%s, completed=%d/%d",
		drawState.LockKey, drawState.CompletedCount, drawState.TotalCount)

	// Validate draw state
	if err := drawState.Validate(); err != nil {
		e.logger.Error("RollbackMultiDraw failed: invalid draw state: %v", err)
		return ErrDrawStateCorrupted
	}

	// FIXME:
	// For now, we just log the rollback attempt
	// In a real implementation, this might involve:
	// 1. Reversing any side effects of the completed draws
	// 2. Cleaning up any temporary state
	// 3. Notifying external systems of the rollback

	e.logger.Info("Rollback completed for lockKey=%s", drawState.LockKey)
	return nil
}

// SaveDrawState saves the current state of a multi-draw operation
// This is a placeholder for future implementation of state persistence
func (e *LotteryEngine) SaveDrawState(ctx context.Context, drawState *DrawState) error {
	e.logger.Debug("SaveDrawState called for lockKey=%s, progress=%.1f%%",
		drawState.LockKey, drawState.Progress())

	// Validate draw state
	if err := drawState.Validate(); err != nil {
		e.logger.Error("SaveDrawState failed: invalid draw state: %v", err)
		return ErrDrawStateCorrupted
	}

	// Update timestamp
	drawState.LastUpdateTime = time.Now().Unix()

	// FIXME:
	// For now, we just log the state save
	// In a real implementation, this might involve:
	// 1. Serializing the state to JSON
	// 2. Storing it in Redis with an expiration time
	// 3. Using a key based on lockKey and operation ID

	e.logger.Debug("Draw state saved for lockKey=%s", drawState.LockKey)
	return nil
}

// LoadDrawState loads a previously saved draw state
// This is a placeholder for future implementation of state recovery
func (e *LotteryEngine) LoadDrawState(ctx context.Context, lockKey string) (*DrawState, error) {
	e.logger.Debug("LoadDrawState called for lockKey=%s", lockKey)

	if lockKey == "" {
		return nil, ErrInvalidParameters
	}

	// FIXME:
	// For now, we return nil to indicate no saved state found
	// In a real implementation, this might involve:
	// 1. Loading serialized state from Redis
	// 2. Deserializing from JSON
	// 3. Validating the loaded state

	e.logger.Debug("No saved state found for lockKey=%s", lockKey)
	return nil, nil
}

// ResumeMultiDrawInRange resumes a previously interrupted multi-draw range operation
func (e *LotteryEngine) ResumeMultiDrawInRange(ctx context.Context, lockKey string, min, max, count int) (*MultiDrawResult, error) {
	e.logger.Info("ResumeMultiDrawInRange called for lockKey=%s", lockKey)

	// Try to load previous state
	savedState, err := e.LoadDrawState(ctx, lockKey)
	if err != nil {
		e.logger.Error("Failed to load draw state: %v", err)
		return nil, err
	}

	if savedState == nil {
		// No saved state, start fresh
		e.logger.Info("No saved state found, starting fresh multi-draw operation")
		return e.DrawMultipleInRangeWithRecovery(ctx, lockKey, min, max, count)
	}

	// Validate that the saved state matches the current request
	if savedState.TotalCount != count {
		e.logger.Error("Saved state count mismatch: saved=%d, requested=%d", savedState.TotalCount, count)
		return nil, ErrDrawStateCorrupted
	}

	// Resume from where we left off
	remainingCount := count - savedState.CompletedCount
	if remainingCount <= 0 {
		// Already completed
		result := &MultiDrawResult{
			Results:        savedState.Results,
			TotalRequested: count,
			Completed:      savedState.CompletedCount,
			Failed:         0,
			PartialSuccess: false,
		}
		return result, nil
	}

	e.logger.Info("Resuming multi-draw operation: %d/%d completed, %d remaining",
		savedState.CompletedCount, count, remainingCount)

	// Continue with remaining draws
	remainingResult, err := e.DrawMultipleInRangeWithRecovery(ctx, lockKey, min, max, remainingCount)
	if err != nil && remainingResult == nil {
		return nil, err
	}

	// Combine results
	combinedResult := &MultiDrawResult{
		Results:        append(savedState.Results, remainingResult.Results...),
		TotalRequested: count,
		Completed:      savedState.CompletedCount + remainingResult.Completed,
		Failed:         remainingResult.Failed,
		PartialSuccess: remainingResult.PartialSuccess || (savedState.CompletedCount > 0 && remainingResult.Failed > 0),
		LastError:      remainingResult.LastError,
		ErrorDetails:   remainingResult.ErrorDetails,
	}

	return combinedResult, err
}

// ResumeMultiDrawFromPrizes resumes a previously interrupted multi-draw prize operation
func (e *LotteryEngine) ResumeMultiDrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int) (*MultiDrawResult, error) {
	e.logger.Info("ResumeMultiDrawFromPrizes called for lockKey=%s", lockKey)

	// Try to load previous state
	savedState, err := e.LoadDrawState(ctx, lockKey)
	if err != nil {
		e.logger.Error("Failed to load draw state: %v", err)
		return nil, err
	}

	if savedState == nil {
		// No saved state, start fresh
		e.logger.Info("No saved state found, starting fresh multi-draw operation")
		return e.DrawMultipleFromPrizesWithRecovery(ctx, lockKey, prizes, count)
	}

	// Validate that the saved state matches the current request
	if savedState.TotalCount != count {
		e.logger.Error("Saved state count mismatch: saved=%d, requested=%d", savedState.TotalCount, count)
		return nil, ErrDrawStateCorrupted
	}

	// Resume from where we left off
	remainingCount := count - savedState.CompletedCount
	if remainingCount <= 0 {
		// Already completed
		result := &MultiDrawResult{
			PrizeResults:   savedState.PrizeResults,
			TotalRequested: count,
			Completed:      savedState.CompletedCount,
			Failed:         0,
			PartialSuccess: false,
		}
		return result, nil
	}

	e.logger.Info("Resuming multi-draw operation: %d/%d completed, %d remaining",
		savedState.CompletedCount, count, remainingCount)

	// Continue with remaining draws
	remainingResult, err := e.DrawMultipleFromPrizesWithRecovery(ctx, lockKey, prizes, remainingCount)
	if err != nil && remainingResult == nil {
		return nil, err
	}

	// Combine results
	combinedResult := &MultiDrawResult{
		PrizeResults:   append(savedState.PrizeResults, remainingResult.PrizeResults...),
		TotalRequested: count,
		Completed:      savedState.CompletedCount + remainingResult.Completed,
		Failed:         remainingResult.Failed,
		PartialSuccess: remainingResult.PartialSuccess || (savedState.CompletedCount > 0 && remainingResult.Failed > 0),
		LastError:      remainingResult.LastError,
		ErrorDetails:   remainingResult.ErrorDetails,
	}

	return combinedResult, err
}

// DrawMultipleInRangeOptimized draws multiple random numbers with performance optimizations
func (e *LotteryEngine) DrawMultipleInRangeOptimized(
	ctx context.Context, lockKey string, min, max, count int, progressCallback ProgressCallback,
) (*MultiDrawResult, error) {
	e.logger.Debug(
		"DrawMultipleInRangeOptimized called with lockKey=%s, min=%d, max=%d, count=%d",
		lockKey, min, max, count)

	// Validate parameters
	if err := ValidateRange(min, max); err != nil {
		e.logger.Error("DrawMultipleInRangeOptimized validation failed: %v", err)
		return nil, err
	}
	if err := ValidateCount(count); err != nil {
		e.logger.Error("DrawMultipleInRangeOptimized count validation failed: %v", err)
		return nil, err
	}
	if lockKey == "" {
		e.logger.Error("DrawMultipleInRangeOptimized failed: empty lock key")
		return nil, ErrInvalidParameters
	}

	// Initialize result structure
	result := &MultiDrawResult{
		Results:        make([]int, 0, count),
		TotalRequested: count,
		Completed:      0,
		Failed:         0,
		PartialSuccess: false,
		ErrorDetails:   make([]DrawError, 0),
	}

	// Get lock manager with read lock protection
	e.mu.RLock()
	lockManager := e.lockManager
	e.mu.RUnlock()

	generator := NewSecureRandomGenerator()

	// Determine batch size for optimization (balance between performance and memory)
	batchSize := calculateOptimalBatchSize(count)

	// Process in batches
	for batchStart := 0; batchStart < count; batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > count {
			batchEnd = count
		}
		currentBatchSize := batchEnd - batchStart

		// Check context cancellation
		select {
		case <-ctx.Done():
			e.logger.Info("DrawMultipleInRangeOptimized cancelled after %d draws", result.Completed)
			result.PartialSuccess = true
			result.LastError = ctx.Err()
			return result, ErrDrawInterrupted
		default:
		}

		// For optimization, we'll use a simpler approach for now
		// Generate all random numbers for this batch first
		batchResults := make([]int, 0, currentBatchSize)
		batchSuccesses := 0

		for i := range currentBatchSize {
			drawIndex := batchStart + i

			// Generate a unique lock value for this draw
			lockValue := generateLockValue()

			// Acquire distributed lock for this draw
			acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, DefaultLockExpiration)
			if err != nil {
				drawError := DrawError{
					DrawIndex: drawIndex + 1,
					Error:     err,
					Timestamp: time.Now().Unix(),
				}
				result.ErrorDetails = append(result.ErrorDetails, drawError)
				result.Failed++
				result.LastError = err
				continue
			}

			if !acquired {
				drawError := DrawError{
					DrawIndex: drawIndex + 1,
					Error:     ErrLockAcquisitionFailed,
					Timestamp: time.Now().Unix(),
				}
				result.ErrorDetails = append(result.ErrorDetails, drawError)
				result.Failed++
				result.LastError = ErrLockAcquisitionFailed
				continue
			}

			// Generate random number
			drawResult, err := generator.GenerateInRange(min, max)

			// Release lock immediately after generation
			released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, lockValue)
			if releaseErr != nil {
				e.logger.Error("Failed to release lock at draw %d: %v", drawIndex+1, releaseErr)
			} else if !released {
				e.logger.Debug("Lock was already released or expired at draw %d", drawIndex+1)
			}

			if err != nil {
				drawError := DrawError{
					DrawIndex: drawIndex + 1,
					Error:     err,
					Timestamp: time.Now().Unix(),
				}
				result.ErrorDetails = append(result.ErrorDetails, drawError)
				result.Failed++
				result.LastError = err
				continue
			}

			batchResults = append(batchResults, drawResult)
			batchSuccesses++
			result.Completed++

			// Call progress callback if provided
			if progressCallback != nil {
				progressCallback(result.Completed, result.TotalRequested, drawResult)
			}
		}

		// Add successful results to final result
		result.Results = append(result.Results, batchResults...)

		e.logger.Debug(
			"Batch %d-%d completed: %d successes, %d failures",
			batchStart+1, batchEnd, batchSuccesses, currentBatchSize-batchSuccesses)
	}

	// Determine final result status
	if result.Failed > 0 && result.Completed > 0 {
		result.PartialSuccess = true
		e.logger.Info(
			"DrawMultipleInRangeOptimized completed with partial success: %d/%d successful",
			result.Completed, result.TotalRequested)
		return result, ErrPartialDrawFailure
	} else if result.Failed > 0 {
		e.logger.Error(
			"DrawMultipleInRangeOptimized failed completely: 0/%d successful", result.TotalRequested)
		return result, result.LastError
	}

	e.logger.Info(
		"DrawMultipleInRangeOptimized successful: lockKey=%s, count=%d", lockKey, result.Completed)
	return result, nil
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

// DrawInRange draws a random number within the specified range using distributed lock
func (e *LotteryEngine) DrawInRange(ctx context.Context, lockKey string, min, max int) (int, error) {
	e.logger.Debug("DrawInRange called with lockKey=%s, min=%d, max=%d", lockKey, min, max)

	// Validate parameters
	if err := ValidateRange(min, max); err != nil {
		e.logger.Error("DrawInRange validation failed: %v", err)
		return 0, err
	}
	if lockKey == "" {
		e.logger.Error("DrawInRange failed: empty lock key")
		return 0, ErrInvalidParameters
	}

	// Generate a unique lock value for this operation
	lockValue := generateLockValue()
	e.logger.Debug("Generated lock value: %s", lockValue)

	// Get lock manager with read lock protection
	e.mu.RLock()
	lockManager := e.lockManager
	e.mu.RUnlock()

	// Acquire distributed lock
	acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, DefaultLockExpiration)
	if err != nil {
		e.logger.Error("DrawInRange lock acquisition error for key %s: %v", lockKey, err)
		return 0, err
	}
	if !acquired {
		e.logger.Error("DrawInRange failed to acquire lock for key %s", lockKey)
		return 0, ErrLockAcquisitionFailed
	}

	e.logger.Debug("Successfully acquired lock for key %s", lockKey)

	// Ensure lock is released
	defer func() {
		released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, lockValue)
		if releaseErr != nil {
			e.logger.Error("Failed to release lock for key %s: %v", lockKey, releaseErr)
		} else if released {
			e.logger.Debug("Successfully released lock for key %s", lockKey)
		} else {
			e.logger.Debug("Lock for key %s was already released or expired", lockKey)
		}
	}()

	// Generate secure random number in range
	generator := NewSecureRandomGenerator()
	result, err := generator.GenerateInRange(min, max)
	if err != nil {
		e.logger.Error("DrawInRange random generation failed: %v", err)
		return 0, err
	}

	e.logger.Info("DrawInRange successful: lockKey=%s, result=%d", lockKey, result)
	return result, nil
}

// DrawMultipleInRange draws multiple random numbers within the specified range using distributed lock
func (e *LotteryEngine) DrawMultipleInRange(ctx context.Context, lockKey string, min, max, count int) ([]int, error) {
	result, err := e.DrawMultipleInRangeWithRecovery(ctx, lockKey, min, max, count)
	if err != nil {
		// If it's a partial failure, return the successful results with the error
		if result != nil && result.PartialSuccess {
			return result.Results, ErrPartialDrawFailure
		}
		return nil, err
	}
	return result.Results, nil
}

// DrawMultipleInRangeWithRecovery draws multiple random numbers with enhanced error handling and recovery
func (e *LotteryEngine) DrawMultipleInRangeWithRecovery(ctx context.Context, lockKey string, min, max, count int) (*MultiDrawResult, error) {
	e.logger.Debug("DrawMultipleInRangeWithRecovery called with lockKey=%s, min=%d, max=%d, count=%d", lockKey, min, max, count)

	// Validate parameters
	if err := ValidateRange(min, max); err != nil {
		e.logger.Error("DrawMultipleInRangeWithRecovery validation failed: %v", err)
		return nil, err
	}
	if err := ValidateCount(count); err != nil {
		e.logger.Error("DrawMultipleInRangeWithRecovery count validation failed: %v", err)
		return nil, err
	}
	if lockKey == "" {
		e.logger.Error("DrawMultipleInRangeWithRecovery failed: empty lock key")
		return nil, ErrInvalidParameters
	}

	// Initialize draw state
	drawState := &DrawState{
		LockKey:        lockKey,
		TotalCount:     count,
		CompletedCount: 0,
		Results:        make([]int, 0, count),
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}

	// Initialize result structure
	result := &MultiDrawResult{
		Results:        make([]int, 0, count),
		TotalRequested: count,
		Completed:      0,
		Failed:         0,
		PartialSuccess: false,
		ErrorDetails:   make([]DrawError, 0),
	}

	// Get lock manager with read lock protection
	e.mu.RLock()
	lockManager := e.lockManager
	e.mu.RUnlock()

	generator := NewSecureRandomGenerator()

	// Perform multiple draws with individual locks for each draw
	for i := 0; i < count; i++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			e.logger.Info("DrawMultipleInRangeWithRecovery cancelled after %d draws, returning partial results", result.Completed)
			result.PartialSuccess = true
			result.LastError = ctx.Err()
			return result, ErrDrawInterrupted
		default:
		}

		// Update draw state
		drawState.CompletedCount = i
		drawState.LastUpdateTime = time.Now().Unix()

		// Generate a unique lock value for this draw
		lockValue := generateLockValue()

		// Acquire distributed lock for this draw
		acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, DefaultLockExpiration)
		if err != nil {
			e.logger.Error("DrawMultipleInRangeWithRecovery lock acquisition error at draw %d: %v", i+1, err)

			// Record the error
			drawError := DrawError{
				DrawIndex: i + 1,
				Error:     err,
				Timestamp: time.Now().Unix(),
			}
			result.ErrorDetails = append(result.ErrorDetails, drawError)
			result.Failed++
			result.LastError = err

			// Decide whether to continue or abort based on error type
			if e.shouldAbortOnError(err) {
				e.logger.Error("Aborting DrawMultipleInRangeWithRecovery due to critical error: %v", err)
				result.PartialSuccess = result.Completed > 0
				return result, err
			}

			// Continue with next draw for non-critical errors
			e.logger.Info("Continuing DrawMultipleInRangeWithRecovery after non-critical error at draw %d", i+1)
			continue
		}

		if !acquired {
			err := ErrLockAcquisitionFailed
			e.logger.Error("DrawMultipleInRangeWithRecovery failed to acquire lock at draw %d", i+1)

			// Record the error
			drawError := DrawError{
				DrawIndex: i + 1,
				Error:     err,
				Timestamp: time.Now().Unix(),
			}
			result.ErrorDetails = append(result.ErrorDetails, drawError)
			result.Failed++
			result.LastError = err

			// Continue with next draw for lock acquisition failures
			continue
		}

		// Generate secure random number in range
		drawResult, err := generator.GenerateInRange(min, max)

		// Release lock immediately after generation
		released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, lockValue)
		if releaseErr != nil {
			e.logger.Error("Failed to release lock at draw %d: %v", i+1, releaseErr)
		} else if !released {
			e.logger.Debug("Lock was already released or expired at draw %d", i+1)
		}

		if err != nil {
			e.logger.Error("DrawMultipleInRangeWithRecovery generation error at draw %d: %v", i+1, err)

			// Record the error
			drawError := DrawError{
				DrawIndex: i + 1,
				Error:     err,
				Timestamp: time.Now().Unix(),
			}
			result.ErrorDetails = append(result.ErrorDetails, drawError)
			result.Failed++
			result.LastError = err

			// Continue with next draw for generation errors
			continue
		}

		// Successfully completed this draw
		result.Results = append(result.Results, drawResult)
		result.Completed++
		drawState.Results = append(drawState.Results, drawResult)
		drawState.CompletedCount = result.Completed

		e.logger.Debug("Draw %d completed successfully: result=%d", i+1, drawResult)
	}

	// Update final state
	drawState.CompletedCount = result.Completed
	drawState.LastUpdateTime = time.Now().Unix()

	// Determine if this is a partial success
	if result.Failed > 0 && result.Completed > 0 {
		result.PartialSuccess = true
		e.logger.Info("DrawMultipleInRangeWithRecovery completed with partial success: %d/%d successful", result.Completed, result.TotalRequested)
		return result, ErrPartialDrawFailure
	} else if result.Failed > 0 {
		e.logger.Error("DrawMultipleInRangeWithRecovery failed completely: 0/%d successful", result.TotalRequested)
		return result, result.LastError
	}

	e.logger.Info("DrawMultipleInRangeWithRecovery successful: lockKey=%s, count=%d", lockKey, result.Completed)
	return result, nil
}

// DrawFromPrizes draws a prize from the given prize pool using distributed lock
func (e *LotteryEngine) DrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize) (*Prize, error) {
	e.logger.Debug("DrawFromPrizes called with lockKey=%s, prizes count=%d", lockKey, len(prizes))

	// Validate parameters
	if lockKey == "" {
		e.logger.Error("DrawFromPrizes failed: empty lock key")
		return nil, ErrInvalidParameters
	}
	if len(prizes) == 0 {
		e.logger.Error("DrawFromPrizes failed: empty prize pool")
		return nil, ErrEmptyPrizePool
	}

	// Validate individual prizes first
	for _, prize := range prizes {
		if err := prize.Validate(); err != nil {
			e.logger.Error("DrawFromPrizes prize validation failed: %v", err)
			return nil, err
		}
	}

	// Generate a unique lock value for this operation
	lockValue := generateLockValue()
	e.logger.Debug("Generated lock value: %s", lockValue)

	// Get lock manager with read lock protection
	e.mu.RLock()
	lockManager := e.lockManager
	e.mu.RUnlock()

	// Acquire distributed lock
	acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, DefaultLockExpiration)
	if err != nil {
		e.logger.Error("DrawFromPrizes lock acquisition error for key %s: %v", lockKey, err)
		return nil, err
	}
	if !acquired {
		e.logger.Error("DrawFromPrizes failed to acquire lock for key %s", lockKey)
		return nil, ErrLockAcquisitionFailed
	}

	e.logger.Debug("Successfully acquired lock for key %s", lockKey)

	// Ensure lock is released
	defer func() {
		released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, lockValue)
		if releaseErr != nil {
			e.logger.Error("Failed to release lock for key %s: %v", lockKey, releaseErr)
		} else if released {
			e.logger.Debug("Successfully released lock for key %s", lockKey)
		} else {
			e.logger.Debug("Lock for key %s was already released or expired", lockKey)
		}
	}()

	// Create prize selector and select prize
	selector := NewDefaultPrizeSelector()
	selectedPrize, err := selector.SelectPrize(prizes)
	if err != nil {
		e.logger.Error("DrawFromPrizes prize selection failed: %v", err)
		return nil, err
	}

	e.logger.Info("DrawFromPrizes successful: lockKey=%s, prize=%s (ID: %s)", lockKey, selectedPrize.Name, selectedPrize.ID)
	return selectedPrize, nil
}

// DrawMultipleFromPrizes draws multiple prizes from the given prize pool using distributed lock
func (e *LotteryEngine) DrawMultipleFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int) ([]*Prize, error) {
	result, err := e.DrawMultipleFromPrizesWithRecovery(ctx, lockKey, prizes, count)
	if err != nil {
		// If it's a partial failure, return the successful results with the error
		if result != nil && result.PartialSuccess {
			return result.PrizeResults, ErrPartialDrawFailure
		}
		return nil, err
	}
	return result.PrizeResults, nil
}

// DrawMultipleFromPrizesWithRecovery draws multiple prizes with enhanced error handling and recovery
func (e *LotteryEngine) DrawMultipleFromPrizesWithRecovery(ctx context.Context, lockKey string, prizes []Prize, count int) (*MultiDrawResult, error) {
	e.logger.Debug("DrawMultipleFromPrizesWithRecovery called with lockKey=%s, prizes count=%d, count=%d", lockKey, len(prizes), count)

	// Validate parameters
	if lockKey == "" {
		e.logger.Error("DrawMultipleFromPrizesWithRecovery failed: empty lock key")
		return nil, ErrInvalidParameters
	}
	if len(prizes) == 0 {
		e.logger.Error("DrawMultipleFromPrizesWithRecovery failed: empty prize pool")
		return nil, ErrEmptyPrizePool
	}
	if err := ValidateCount(count); err != nil {
		e.logger.Error("DrawMultipleFromPrizesWithRecovery count validation failed: %v", err)
		return nil, err
	}

	// Validate individual prizes first
	for _, prize := range prizes {
		if err := prize.Validate(); err != nil {
			e.logger.Error("DrawMultipleFromPrizesWithRecovery prize validation failed: %v", err)
			return nil, err
		}
	}

	// Initialize draw state
	drawState := &DrawState{
		LockKey:        lockKey,
		TotalCount:     count,
		CompletedCount: 0,
		PrizeResults:   make([]*Prize, 0, count),
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}

	// Initialize result structure
	result := &MultiDrawResult{
		PrizeResults:   make([]*Prize, 0, count),
		TotalRequested: count,
		Completed:      0,
		Failed:         0,
		PartialSuccess: false,
		ErrorDetails:   make([]DrawError, 0),
	}

	// Get lock manager with read lock protection
	e.mu.RLock()
	lockManager := e.lockManager
	e.mu.RUnlock()

	selector := NewDefaultPrizeSelector()

	// Perform multiple draws with individual locks for each draw
	for i := 0; i < count; i++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			e.logger.Info("DrawMultipleFromPrizesWithRecovery cancelled after %d draws, returning partial results", result.Completed)
			result.PartialSuccess = true
			result.LastError = ctx.Err()
			return result, ErrDrawInterrupted
		default:
		}

		// Update draw state
		drawState.CompletedCount = i
		drawState.LastUpdateTime = time.Now().Unix()

		// Generate a unique lock value for this draw
		lockValue := generateLockValue()

		// Acquire distributed lock for this draw
		acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, DefaultLockExpiration)
		if err != nil {
			e.logger.Error("DrawMultipleFromPrizesWithRecovery lock acquisition error at draw %d: %v", i+1, err)

			// Record the error
			drawError := DrawError{
				DrawIndex: i + 1,
				Error:     err,
				Timestamp: time.Now().Unix(),
			}
			result.ErrorDetails = append(result.ErrorDetails, drawError)
			result.Failed++
			result.LastError = err

			// Decide whether to continue or abort based on error type
			if e.shouldAbortOnError(err) {
				e.logger.Error("Aborting DrawMultipleFromPrizesWithRecovery due to critical error: %v", err)
				result.PartialSuccess = result.Completed > 0
				return result, err
			}

			// Continue with next draw for non-critical errors
			e.logger.Info("Continuing DrawMultipleFromPrizesWithRecovery after non-critical error at draw %d", i+1)
			continue
		}

		if !acquired {
			err := ErrLockAcquisitionFailed
			e.logger.Error("DrawMultipleFromPrizesWithRecovery failed to acquire lock at draw %d", i+1)

			// Record the error
			drawError := DrawError{
				DrawIndex: i + 1,
				Error:     err,
				Timestamp: time.Now().Unix(),
			}
			result.ErrorDetails = append(result.ErrorDetails, drawError)
			result.Failed++
			result.LastError = err

			// Continue with next draw for lock acquisition failures
			continue
		}

		// Select prize from pool
		selectedPrize, err := selector.SelectPrize(prizes)

		// Release lock immediately after selection
		released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, lockValue)
		if releaseErr != nil {
			e.logger.Error("Failed to release lock at draw %d: %v", i+1, releaseErr)
		} else if !released {
			e.logger.Debug("Lock was already released or expired at draw %d", i+1)
		}

		if err != nil {
			e.logger.Error("DrawMultipleFromPrizesWithRecovery prize selection error at draw %d: %v", i+1, err)

			// Record the error
			drawError := DrawError{
				DrawIndex: i + 1,
				Error:     err,
				Timestamp: time.Now().Unix(),
			}
			result.ErrorDetails = append(result.ErrorDetails, drawError)
			result.Failed++
			result.LastError = err

			// Continue with next draw for selection errors
			continue
		}

		// Successfully completed this draw
		result.PrizeResults = append(result.PrizeResults, selectedPrize)
		result.Completed++
		drawState.PrizeResults = append(drawState.PrizeResults, selectedPrize)
		drawState.CompletedCount = result.Completed

		e.logger.Debug("Draw %d completed successfully: prize=%s (ID: %s)", i+1, selectedPrize.Name, selectedPrize.ID)
	}

	// Update final state
	drawState.CompletedCount = result.Completed
	drawState.LastUpdateTime = time.Now().Unix()

	// Determine if this is a partial success
	if result.Failed > 0 && result.Completed > 0 {
		result.PartialSuccess = true
		e.logger.Info("DrawMultipleFromPrizesWithRecovery completed with partial success: %d/%d successful", result.Completed, result.TotalRequested)
		return result, ErrPartialDrawFailure
	} else if result.Failed > 0 {
		e.logger.Error("DrawMultipleFromPrizesWithRecovery failed completely: 0/%d successful", result.TotalRequested)
		return result, result.LastError
	}

	e.logger.Info("DrawMultipleFromPrizesWithRecovery successful: lockKey=%s, count=%d", lockKey, result.Completed)
	return result, nil
}

// GetPerformanceMetrics 获取性能指标
func (e *LotteryEngine) GetPerformanceMetrics() PerformanceMetrics {
	return e.performanceMonitor.GetMetrics()
}

// ResetPerformanceMetrics 重置性能指标
func (e *LotteryEngine) ResetPerformanceMetrics() {
	e.performanceMonitor.ResetMetrics()
}

// EnablePerformanceMonitoring 启用性能监控
func (e *LotteryEngine) EnablePerformanceMonitoring() {
	e.performanceMonitor.Enable()
}

// DisablePerformanceMonitoring 禁用性能监控
func (e *LotteryEngine) DisablePerformanceMonitoring() {
	e.performanceMonitor.Disable()
}

// DrawInRangeWithMonitoring 带性能监控的范围抽奖
func (e *LotteryEngine) DrawInRangeWithMonitoring(ctx context.Context, lockKey string, min, max int) (int, error) {
	startTime := time.Now()

	// 调用原始的抽奖方法
	result, err := e.DrawInRange(ctx, lockKey, min, max)

	// 记录性能指标
	duration := time.Since(startTime)
	e.performanceMonitor.RecordDraw(err == nil, duration)

	if err == ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordLockAcquisition(false, duration)
	} else if err == nil {
		e.performanceMonitor.RecordLockAcquisition(true, duration)
		e.performanceMonitor.RecordLockRelease()
	}

	if err != nil && err != ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordRedisError()
	}

	return result, err
}

// DrawFromPrizesWithMonitoring 带性能监控的奖品池抽奖
func (e *LotteryEngine) DrawFromPrizesWithMonitoring(ctx context.Context, lockKey string, prizes []Prize) (*Prize, error) {
	startTime := time.Now()

	// 调用原始的抽奖方法
	result, err := e.DrawFromPrizes(ctx, lockKey, prizes)

	// 记录性能指标
	duration := time.Since(startTime)
	e.performanceMonitor.RecordDraw(err == nil, duration)

	if err == ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordLockAcquisition(false, duration)
	} else if err == nil {
		e.performanceMonitor.RecordLockAcquisition(true, duration)
		e.performanceMonitor.RecordLockRelease()
	}

	if err != nil && err != ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordRedisError()
	}

	return result, err
}

// DrawInRangeOptimized 优化的范围抽奖（带锁缓存）
func (e *LotteryEngine) DrawInRangeOptimized(ctx context.Context, lockKey string, min, max int) (int, error) {
	startTime := time.Now()

	// 检查锁缓存（快速路径）
	if cachedTime, exists := e.lockCache.Load(lockKey); exists {
		if time.Since(cachedTime.(time.Time)) < e.lockCacheTTL {
			// 锁仍在缓存中，直接返回失败
			duration := time.Since(startTime)
			e.performanceMonitor.RecordDraw(false, duration)
			e.performanceMonitor.RecordLockAcquisition(false, duration)
			return 0, ErrLockAcquisitionFailed
		}
	}

	// 调用原始的抽奖方法
	result, err := e.DrawInRange(ctx, lockKey, min, max)

	// 记录性能指标
	duration := time.Since(startTime)
	e.performanceMonitor.RecordDraw(err == nil, duration)

	if err == ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordLockAcquisition(false, duration)
		// 更新锁缓存
		e.lockCache.Store(lockKey, time.Now())
	} else if err == nil {
		e.performanceMonitor.RecordLockAcquisition(true, duration)
		e.performanceMonitor.RecordLockRelease()
		// 清除锁缓存
		e.lockCache.Delete(lockKey)
	}

	if err != nil && err != ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordRedisError()
	}

	return result, err
}

// DrawFromPrizesOptimized 优化的奖品池抽奖（带锁缓存）
func (e *LotteryEngine) DrawFromPrizesOptimized(ctx context.Context, lockKey string, prizes []Prize) (*Prize, error) {
	startTime := time.Now()

	// 检查锁缓存（快速路径）
	if cachedTime, exists := e.lockCache.Load(lockKey); exists {
		if time.Since(cachedTime.(time.Time)) < e.lockCacheTTL {
			// 锁仍在缓存中，直接返回失败
			duration := time.Since(startTime)
			e.performanceMonitor.RecordDraw(false, duration)
			e.performanceMonitor.RecordLockAcquisition(false, duration)
			return nil, ErrLockAcquisitionFailed
		}
	}

	// 调用原始的抽奖方法
	result, err := e.DrawFromPrizes(ctx, lockKey, prizes)

	// 记录性能指标
	duration := time.Since(startTime)
	e.performanceMonitor.RecordDraw(err == nil, duration)

	if err == ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordLockAcquisition(false, duration)
		// 更新锁缓存
		e.lockCache.Store(lockKey, time.Now())
	} else if err == nil {
		e.performanceMonitor.RecordLockAcquisition(true, duration)
		e.performanceMonitor.RecordLockRelease()
		// 清除锁缓存
		e.lockCache.Delete(lockKey)
	}

	if err != nil && err != ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordRedisError()
	}

	return result, err
}
