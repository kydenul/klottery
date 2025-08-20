package lottery

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sony/gobreaker"
)

// DrawError represents an error that occurred during a specific draw
type DrawError struct {
	DrawIndex int    `json:"draw_index"`    // Index of the draw that failed (1-based)
	Error     error  `json:"-"`             // The error that occurred (not serialized)
	ErrorMsg  string `json:"error_message"` // Error message for serialization
	Timestamp int64  `json:"timestamp"`     // Unix timestamp when the error occurred
}

// MarshalJSON implements custom JSON marshaling for DrawError
func (de DrawError) MarshalJSON() ([]byte, error) {
	// Create a temporary struct for marshaling
	temp := struct {
		DrawIndex int    `json:"draw_index"`
		ErrorMsg  string `json:"error_message"`
		Timestamp int64  `json:"timestamp"`
	}{
		DrawIndex: de.DrawIndex,
		ErrorMsg:  de.ErrorMsg,
		Timestamp: de.Timestamp,
	}

	// If ErrorMsg is empty but Error is not nil, use Error.Error()
	if temp.ErrorMsg == "" && de.Error != nil {
		temp.ErrorMsg = de.Error.Error()
	}

	return json.Marshal(temp)
}

// UnmarshalJSON implements custom JSON unmarshaling for DrawError
func (de *DrawError) UnmarshalJSON(data []byte) error {
	// Create a temporary struct for unmarshaling
	temp := struct {
		DrawIndex int    `json:"draw_index"`
		ErrorMsg  string `json:"error_message"`
		Timestamp int64  `json:"timestamp"`
	}{}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	de.DrawIndex = temp.DrawIndex
	de.ErrorMsg = temp.ErrorMsg
	de.Timestamp = temp.Timestamp
	// Note: Error field is not restored from JSON, only ErrorMsg is available

	return nil
}

// LotteryEngine provides thread-safe lottery functionality
type LotteryEngine struct {
	redisClient   *redis.Client
	lockManager   *DistributedLockManager
	configManager *ConfigManager
	logger        Logger
	mu            sync.RWMutex // 保护配置和 lockManager 的并发访问

	circuitBreaker     *gobreaker.CircuitBreaker // 熔断器
	performanceMonitor *PerformanceMonitor
	lockCache          sync.Map // 锁缓存，用于快速路径优化

	// Reused resources to reduce allocations on hot paths
	random        *SecureRandomGenerator
	prizeSelector *DefaultPrizeSelector
}

// NewLotteryEngine creates a new lottery engine with the given Redis client
func NewLotteryEngine(redisClient *redis.Client) *LotteryEngine {
	cm := NewDefaultConfigManager()
	logger := &DefaultLogger{}

	var breaker *gobreaker.CircuitBreaker
	// 启用熔断器
	if cm.config.CircuitBreaker.Enabled {
		breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        cm.config.CircuitBreaker.Name,
			MaxRequests: cm.config.CircuitBreaker.MaxRequests,
			Interval:    cm.config.CircuitBreaker.Interval,
			Timeout:     cm.config.CircuitBreaker.Timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				// 当请求数达到最小要求且失败率超过阈值时触发熔断
				return counts.Requests >= cm.config.CircuitBreaker.MinRequests &&
					float64(counts.TotalFailures)/float64(counts.Requests) >= cm.config.CircuitBreaker.FailureRatio
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if cm.config.CircuitBreaker.OnStateChange && logger != nil {
					logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
				}
			},
		})
	}

	return &LotteryEngine{
		redisClient: redisClient,
		lockManager: NewLockManagerWithRetry(
			redisClient,
			cm.config.Engine.LockTimeout,
			cm.config.Engine.RetryAttempts,
			cm.config.Engine.RetryInterval,
			cm.config.Engine.LockCacheTTL,
		),
		configManager: cm,
		logger:        logger,

		circuitBreaker:     breaker,
		performanceMonitor: NewPerformanceMonitor(),
		random:             NewSecureRandomGenerator(),
		prizeSelector:      NewDefaultPrizeSelector(),
	}
}

// NewLotteryEngineWithConfig creates a new lottery engine with custom configuration
func NewLotteryEngineWithConfig(redisClient *redis.Client, cm *ConfigManager) *LotteryEngine {
	logger := NewSilentLogger()

	var breaker *gobreaker.CircuitBreaker
	// 启用熔断器
	if cm.config.CircuitBreaker.Enabled {
		breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        cm.config.CircuitBreaker.Name,
			MaxRequests: cm.config.CircuitBreaker.MaxRequests,
			Interval:    cm.config.CircuitBreaker.Interval,
			Timeout:     cm.config.CircuitBreaker.Timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				// 当请求数达到最小要求且失败率超过阈值时触发熔断
				return counts.Requests >= cm.config.CircuitBreaker.MinRequests &&
					float64(counts.TotalFailures)/float64(counts.Requests) >= cm.config.CircuitBreaker.FailureRatio
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if cm.config.CircuitBreaker.OnStateChange && logger != nil {
					logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
				}
			},
		})
	}

	return &LotteryEngine{
		redisClient: redisClient,
		lockManager: NewLockManagerWithRetry(
			redisClient,
			cm.config.Engine.LockTimeout,
			cm.config.Engine.RetryAttempts,
			cm.config.Engine.RetryInterval,
			cm.config.Engine.LockCacheTTL,
		),
		configManager: cm,
		logger:        logger,

		circuitBreaker:     breaker,
		performanceMonitor: NewPerformanceMonitor(),
		random:             NewSecureRandomGenerator(),
		prizeSelector:      NewDefaultPrizeSelector(),
	}
}

// NewLotteryEngineWithLogger creates a new lottery engine with custom logger
func NewLotteryEngineWithLogger(redisClient *redis.Client, logger Logger) *LotteryEngine {
	cm := NewDefaultConfigManager()

	var breaker *gobreaker.CircuitBreaker
	// 启用熔断器
	if cm.config.CircuitBreaker.Enabled {
		breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        cm.config.CircuitBreaker.Name,
			MaxRequests: cm.config.CircuitBreaker.MaxRequests,
			Interval:    cm.config.CircuitBreaker.Interval,
			Timeout:     cm.config.CircuitBreaker.Timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				// 当请求数达到最小要求且失败率超过阈值时触发熔断
				return counts.Requests >= cm.config.CircuitBreaker.MinRequests &&
					float64(counts.TotalFailures)/float64(counts.Requests) >= cm.config.CircuitBreaker.FailureRatio
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if cm.config.CircuitBreaker.OnStateChange && logger != nil {
					logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
				}
			},
		})
	}

	lockManager := NewLockManagerWithRetry(
		redisClient,
		cm.config.Engine.LockTimeout,
		cm.config.Engine.RetryAttempts,
		cm.config.Engine.RetryInterval,
		cm.config.Engine.LockCacheTTL,
	)

	return &LotteryEngine{
		redisClient:   redisClient,
		lockManager:   lockManager,
		configManager: cm,
		logger:        logger,

		circuitBreaker:     breaker,
		performanceMonitor: NewPerformanceMonitor(),
		random:             NewSecureRandomGenerator(),
		prizeSelector:      NewDefaultPrizeSelector(),
	}
}

// NewLotteryEngineWithConfigAndLogger creates a new lottery engine with custom configuration and logger
func NewLotteryEngineWithConfigAndLogger(
	redisClient *redis.Client, cm *ConfigManager, logger Logger,
) *LotteryEngine {
	lockManager := NewLockManagerWithRetry(
		redisClient,
		cm.config.Engine.LockTimeout,
		cm.config.Engine.RetryAttempts,
		cm.config.Engine.RetryInterval,
		cm.config.Engine.LockCacheTTL,
	)

	var breaker *gobreaker.CircuitBreaker
	// 启用熔断器
	if cm.config.CircuitBreaker.Enabled {
		breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        cm.config.CircuitBreaker.Name,
			MaxRequests: cm.config.CircuitBreaker.MaxRequests,
			Interval:    cm.config.CircuitBreaker.Interval,
			Timeout:     cm.config.CircuitBreaker.Timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				// 当请求数达到最小要求且失败率超过阈值时触发熔断
				return counts.Requests >= cm.config.CircuitBreaker.MinRequests &&
					float64(counts.TotalFailures)/float64(counts.Requests) >= cm.config.CircuitBreaker.FailureRatio
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if cm.config.CircuitBreaker.OnStateChange && logger != nil {
					logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
				}
			},
		})
	}

	return &LotteryEngine{
		redisClient:   redisClient,
		lockManager:   lockManager,
		configManager: cm,
		logger:        logger,

		circuitBreaker:     breaker,
		performanceMonitor: NewPerformanceMonitor(),
		random:             NewSecureRandomGenerator(),
		prizeSelector:      NewDefaultPrizeSelector(),
	}
}

// GetConfig returns a copy of the current lottery engine configuration
func (e *LotteryEngine) GetConfig() *Config {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.configManager.config
}

// UpdateConfig updates the lottery engine configuration at runtime
func (e *LotteryEngine) UpdateConfig(newConfig *Config) error {
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
	e.configManager.config = newConfig

	// Update the lock manager with new configuration
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.configManager.config.Engine.LockTimeout,
		e.configManager.config.Engine.RetryAttempts,
		e.configManager.config.Engine.RetryInterval,
		e.configManager.config.Engine.LockCacheTTL,
	)

	e.logger.Info(
		"Configuration updated successfully: LockTimeout=%v, RetryAttempts=%d, RetryInterval=%v, LockCacheTTL=%v",
		e.configManager.config.Engine.LockTimeout,
		e.configManager.config.Engine.RetryAttempts,
		e.configManager.config.Engine.RetryInterval,
		e.configManager.config.Engine.LockCacheTTL)
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

	e.configManager.config.Engine.LockTimeout = timeout
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.configManager.config.Engine.LockTimeout,
		e.configManager.config.Engine.RetryAttempts,
		e.configManager.config.Engine.RetryInterval,
		e.configManager.config.Engine.LockCacheTTL,
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

	e.configManager.config.Engine.RetryAttempts = attempts
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.configManager.config.Engine.LockTimeout,
		e.configManager.config.Engine.RetryAttempts,
		e.configManager.config.Engine.RetryInterval,
		e.configManager.config.Engine.LockCacheTTL,
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

	e.configManager.config.Engine.RetryInterval = interval
	e.lockManager = NewLockManagerWithRetry(
		e.redisClient,
		e.configManager.config.Engine.LockTimeout,
		e.configManager.config.Engine.RetryAttempts,
		e.configManager.config.Engine.RetryInterval,
		e.configManager.config.Engine.LockCacheTTL,
	)

	e.logger.Info("Retry interval updated to %v", interval)
	return nil
}

// SetLogger updates the logger at runtime
func (e *LotteryEngine) SetLogger(logger Logger) {
	if logger != nil && logger != e.logger {
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

func (e *LotteryEngine) executeWithBreaker(operation func() (any, error)) (any, error) {
	// Circuit breaker DON'T ENABLED => execute operation directly
	if e.configManager.config.CircuitBreaker.Enabled == false || e.circuitBreaker == nil {
		return operation()
	}

	result, err := e.circuitBreaker.Execute(operation)
	if err != nil {
		// 检查是否是熔断器错误
		if err == gobreaker.ErrOpenState {
			return nil, ErrCircuitBreakerOpen.WithDetails("circuit breaker is open, requests are being rejected")
		}
		if err == gobreaker.ErrTooManyRequests {
			return nil, ErrCircuitBreakerOpen.WithDetails("too many requests, circuit breaker is half-open")
		}
	}

	return result, err
}

// doDrawInRange draws a random number within the specified range using distributed lock
func (e *LotteryEngine) doDrawInRange(ctx context.Context, lockKey string, min, max int) (int, error) {
	e.logger.Debug("drawInRange called with lockKey=%s, min=%d, max=%d", lockKey, min, max)

	// Validate parameters
	if err := ValidateRange(min, max); err != nil {
		e.logger.Error("drawInRange validation failed: %v", err)
		return 0, err
	}
	if lockKey == "" {
		e.logger.Error("drawInRange failed: empty lock key")
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
		e.logger.Error("drawInRange lock acquisition error for key %s: %v", lockKey, err)
		return 0, err
	}
	if !acquired {
		e.logger.Error("drawInRange failed to acquire lock for key %s", lockKey)
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
	result, err := e.random.GenerateInRange(min, max)
	if err != nil {
		e.logger.Error("drawInRange random generation failed: %v", err)
		return 0, err
	}

	e.logger.Info("drawInRange successful: lockKey=%s, result=%d", lockKey, result)
	return result, nil
}

// DrawInRange draws a random number within the specified range using distributed lock [with lock cache]
func (e *LotteryEngine) drawInRangeWithLockCache(ctx context.Context, lockKey string, min, max int) (int, error) {
	startTime := time.Now()

	// Check lock cache (fast path)
	if cachedTime, exists := e.lockCache.Load(lockKey); exists {
		if time.Since(cachedTime.(time.Time)) < e.configManager.config.Engine.LockCacheTTL {
			// Lock Still in cache => return Failure directly
			duration := time.Since(startTime)
			e.performanceMonitor.RecordDraw(false, duration)
			e.performanceMonitor.RecordLockAcquisition(false, duration)
			return 0, ErrLockAcquisitionFailed
		}
	}

	// 调用原始的抽奖方法
	result, err := e.doDrawInRange(ctx, lockKey, min, max)

	// 记录性能指标
	duration := time.Since(startTime)
	e.performanceMonitor.RecordDraw(err == nil, duration) // TAG: Record draw

	switch err {
	case ErrLockAcquisitionFailed:
		e.performanceMonitor.RecordLockAcquisition(false, duration)
		// 更新锁缓存
		e.lockCache.Store(lockKey, time.Now())
	case nil:
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

// DrawInRange draws a random number within the specified range using distributed lock [with lock cache and circuit breaker]
func (e *LotteryEngine) DrawInRange(ctx context.Context, lockKey string, min, max int) (int, error) {
	result, err := e.executeWithBreaker(func() (any, error) {
		return e.drawInRangeWithLockCache(ctx, lockKey, min, max)
	})
	if err != nil {
		return 0, err
	}

	return result.(int), nil
}

// doDrawFromPrizes draws a prize from the given prize pool using distributed lock
func (e *LotteryEngine) doDrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize) (*Prize, error) {
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

	// Select prize using engine's reusable selector
	selectedPrize, err := e.prizeSelector.SelectPrize(prizes)
	if err != nil {
		e.logger.Error("DrawFromPrizes prize selection failed: %v", err)
		return nil, err
	}

	e.logger.Info("DrawFromPrizes successful: lockKey=%s, prize=%s (ID: %s)", lockKey, selectedPrize.Name, selectedPrize.ID)
	return selectedPrize, nil
}

// DrawFromPrizesdraws a prize from the given prize pool using distributed lock [with lock cache]
func (e *LotteryEngine) drawFromPrizesWithLockCache(
	ctx context.Context, lockKey string, prizes []Prize,
) (*Prize, error) {
	startTime := time.Now()

	// Check lock cache => fast path
	if cachedTime, exists := e.lockCache.Load(lockKey); exists {
		if time.Since(cachedTime.(time.Time)) < e.configManager.config.Engine.LockCacheTTL {
			// Lock in cache => return failure directly
			duration := time.Since(startTime)
			e.performanceMonitor.RecordDraw(false, duration)
			e.performanceMonitor.RecordLockAcquisition(false, duration)
			return nil, ErrLockAcquisitionFailed
		}
	}

	// call doDrawFromPrizes
	result, err := e.doDrawFromPrizes(ctx, lockKey, prizes)

	// Record performance
	duration := time.Since(startTime)
	e.performanceMonitor.RecordDraw(err == nil, duration)

	switch err {
	case ErrLockAcquisitionFailed:
		e.performanceMonitor.RecordLockAcquisition(false, duration)
		// Update lock cache
		e.lockCache.Store(lockKey, time.Now())
	case nil:
		e.performanceMonitor.RecordLockAcquisition(true, duration)
		e.performanceMonitor.RecordLockRelease()
		// Clear lock cache
		e.lockCache.Delete(lockKey)
	}

	if err != nil && err != ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordRedisError()
	}

	return result, err
}

// DrawFromPrizesdraws a prize from the given prize pool using distributed lock [with lock cache and circuit breaker]
func (e *LotteryEngine) DrawFromPrizes(
	ctx context.Context, lockKey string, prizes []Prize,
) (*Prize, error) {
	result, err := e.executeWithBreaker(func() (any, error) {
		return e.drawFromPrizesWithLockCache(ctx, lockKey, prizes)
	})
	if err != nil {
		return nil, err
	}

	return result.(*Prize), nil
}

// DrawMultipleInRange draws multiple random numbers within the specified range using distributed lock [with lock cache]
func (e *LotteryEngine) drawMultipleInRangeWithLockCache(
	ctx context.Context, lockKey string, min, max, count int, progressCallback ProgressCallback,
) (*MultiDrawResult, error) {
	e.logger.Debug("DrawMultipleInRange called with lockKey=%s, min=%d, max=%d, count=%d", lockKey, min, max, count)

	// Validate parameters
	if err := ValidateRange(min, max); err != nil {
		e.logger.Error("DrawMultipleInRange validation failed: %v", err)
		return nil, err
	}
	if err := ValidateCount(count); err != nil {
		e.logger.Error("DrawMultipleInRange count validation failed: %v", err)
		return nil, err
	}
	if lockKey == "" {
		e.logger.Error("DrawMultipleInRange failed: empty lock key")
		return nil, ErrInvalidParameters
	}

	// Initialize draw state for recovery purposes
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

	generator := e.random

	// Determine batch size for optimization
	batchSize := calculateOptimalBatchSize(count)

	// Process in batches
	for batchStart := 0; batchStart < count; batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > count {
			batchEnd = count
		}
		currentBatchSize := batchEnd - batchStart

		// Check for context cancellation before starting a new batch
		select {
		case <-ctx.Done():
			e.logger.Info("DrawMultipleInRange cancelled after %d draws, returning partial results", result.Completed)
			result.PartialSuccess = true
			result.LastError = ctx.Err()
			return result, ErrDrawInterrupted
		default:
		}

		// Try to acquire a single lock for the whole batch to reduce Redis round trips
		batchLockValue := generateLockValue()
		batchAcquired, batchErr := lockManager.AcquireLock(ctx, lockKey, batchLockValue, DefaultLockExpiration)
		if batchErr == nil && batchAcquired {
			// Process without per-draw locking
			for i := 0; i < currentBatchSize; i++ {
				drawIndex := batchStart + i

				drawResult, err := generator.GenerateInRange(min, max)
				if err != nil {
					e.logger.Error("DrawMultipleInRange generation error at draw %d: %v", drawIndex+1, err)
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

				result.Results = append(result.Results, drawResult)
				result.Completed++

				drawState.Results = append(drawState.Results, drawResult)
				drawState.CompletedCount = result.Completed
				drawState.LastUpdateTime = time.Now().Unix()

				e.logger.Debug("Draw %d completed successfully: result=%d", drawIndex+1, drawResult)

				if progressCallback != nil {
					progressCallback(result.Completed, result.TotalRequested, drawResult)
				}
			}

			// Release batch lock
			if released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, batchLockValue); releaseErr != nil {
				e.logger.Error("Failed to release batch lock: %v", releaseErr)
			} else if !released {
				e.logger.Debug("Batch lock was already released or expired")
			}
		} else {
			// Fallback to per-draw locking on failure
			for i := 0; i < currentBatchSize; i++ {
				drawIndex := batchStart + i

				// Generate a unique lock value for this draw
				lockValue := generateLockValue()

				// Acquire distributed lock for this draw
				acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, DefaultLockExpiration)
				if err != nil {
					e.logger.Error("DrawMultipleInRange lock acquisition error at draw %d: %v", drawIndex+1, err)
					drawError := DrawError{
						DrawIndex: drawIndex + 1,
						Error:     err,
						Timestamp: time.Now().Unix(),
					}
					result.ErrorDetails = append(result.ErrorDetails, drawError)
					result.Failed++
					result.LastError = err

					// Abort on critical errors
					if e.shouldAbortOnError(err) {
						e.logger.Error("Aborting DrawMultipleInRange due to critical error: %v", err)
						result.PartialSuccess = result.Completed > 0
						return result, err
					}
					continue // Continue to next draw on non-critical errors
				}

				if !acquired {
					err := ErrLockAcquisitionFailed
					e.logger.Error("DrawMultipleInRange failed to acquire lock at draw %d", drawIndex+1)
					drawError := DrawError{
						DrawIndex: drawIndex + 1,
						Error:     err,
						Timestamp: time.Now().Unix(),
					}
					result.ErrorDetails = append(result.ErrorDetails, drawError)
					result.Failed++
					result.LastError = err
					continue // Continue to next draw
				}

				// Generate secure random number
				drawResult, err := generator.GenerateInRange(min, max)

				// Release lock immediately
				released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, lockValue)
				if releaseErr != nil {
					e.logger.Error("Failed to release lock at draw %d: %v", drawIndex+1, releaseErr)
				} else if !released {
					e.logger.Debug("Lock was already released or expired at draw %d", drawIndex+1)
				}

				if err != nil {
					e.logger.Error("DrawMultipleInRange generation error at draw %d: %v", drawIndex+1, err)
					drawError := DrawError{
						DrawIndex: drawIndex + 1,
						Error:     err,
						Timestamp: time.Now().Unix(),
					}
					result.ErrorDetails = append(result.ErrorDetails, drawError)
					result.Failed++
					result.LastError = err
					continue // Continue to next draw
				}

				// Success
				result.Results = append(result.Results, drawResult)
				result.Completed++

				// Update draw state for recovery
				drawState.Results = append(drawState.Results, drawResult)
				drawState.CompletedCount = result.Completed
				drawState.LastUpdateTime = time.Now().Unix()

				e.logger.Debug("Draw %d completed successfully: result=%d", drawIndex+1, drawResult)

				// Trigger progress callback if provided
				if progressCallback != nil {
					progressCallback(result.Completed, result.TotalRequested, drawResult)
				}
			}
		}
	}

	// Update final state
	drawState.CompletedCount = result.Completed
	drawState.LastUpdateTime = time.Now().Unix()

	// Determine final result status
	if result.Failed > 0 && result.Completed > 0 {
		result.PartialSuccess = true
		e.logger.Info("DrawMultipleInRange completed with partial success: %d/%d successful", result.Completed, result.TotalRequested)
		return result, ErrPartialDrawFailure
	} else if result.Failed > 0 {
		e.logger.Error("DrawMultipleInRange failed completely: 0/%d successful", result.TotalRequested)
		return result, result.LastError
	}

	e.logger.Info("DrawMultipleInRange successful: lockKey=%s, count=%d", lockKey, result.Completed)
	return result, nil
}

// DrawMultipleInRange draws multiple random numbers within the specified range using distributed lock [with lock cache and circuit breaker]
func (e *LotteryEngine) DrawMultipleInRange(
	ctx context.Context, lockKey string, min, max, count int, progressCallback ProgressCallback,
) (*MultiDrawResult, error) {
	result, err := e.executeWithBreaker(func() (any, error) {
		return e.drawMultipleInRangeWithLockCache(ctx, lockKey, min, max, count, progressCallback)
	})
	if err != nil {
		// For validation errors (invalid parameters, range, count), return nil result
		if err == ErrInvalidParameters || err == ErrInvalidRange || err == ErrInvalidCount {
			return nil, err
		}

		// For circuit breaker errors or other runtime errors, return a proper MultiDrawResult structure
		errorResult := &MultiDrawResult{
			Results:        make([]int, 0),
			TotalRequested: count,
			Completed:      0,
			Failed:         count,
			PartialSuccess: false,
			LastError:      err,
			ErrorDetails:   make([]DrawError, 0),
		}
		return errorResult, err
	}

	return result.(*MultiDrawResult), nil
}

// DrawMultipleFromPrizes draws multiple prizes from the given prize pool using distributed lock [with lock cache]
func (e *LotteryEngine) drawMultipleFromPrizesWithLockCache(ctx context.Context, lockKey string, prizes []Prize, count int, progressCallback ProgressCallback,
) (*MultiDrawResult, error) {
	startTime := time.Now()
	e.logger.Debug("DrawMultipleFromPrizes called for lockKey=%s, count=%d", lockKey, count)

	// Validate input parameters
	if lockKey == "" {
		e.logger.Error("DrawMultipleFromPrizes lockKey validation failed: empty lockKey")
		return nil, ErrInvalidParameters
	}
	if len(prizes) == 0 {
		e.logger.Error("DrawMultipleFromPrizes prizes validation failed: empty prize pool")
		return nil, ErrEmptyPrizePool
	}
	if err := ValidateCount(count); err != nil {
		e.logger.Error("DrawMultipleFromPrizes count validation failed: %v", err)
		return nil, err
	}

	// 综合奖品验证：既使用批量验证也进行详细验证
	if err := ValidatePrizePool(prizes); err != nil {
		e.logger.Error("DrawMultipleFromPrizes prize pool validation failed: %v", err)
		return nil, err
	}

	for _, prize := range prizes {
		if err := prize.Validate(); err != nil {
			e.logger.Error("DrawMultipleFromPrizes individual prize validation failed: %v", err)
			return nil, err
		}
	}

	// 初始化状态管理
	drawState := &DrawState{
		LockKey:        lockKey,
		TotalCount:     count,
		CompletedCount: 0,
		PrizeResults:   make([]*Prize, 0, count),
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}

	// 初始化结果结构
	result := &MultiDrawResult{
		TotalRequested: count,
		Completed:      0,
		Failed:         0,
		PartialSuccess: false,
		PrizeResults:   make([]*Prize, 0, count),
		ErrorDetails:   make([]DrawError, 0),
	}

	// 获取锁管理器（使用引擎的锁管理器并加读锁保护）
	e.mu.RLock()
	engineLockManager := e.lockManager
	e.mu.RUnlock()

	// 同时创建独立的锁管理器作为备用
	backupLockManager := NewLockManager(e.redisClient, e.configManager.config.Engine.LockTimeout)

	// 优先使用引擎锁管理器，失败时使用备用
	lockManager := engineLockManager
	if lockManager == nil {
		lockManager = backupLockManager
		e.logger.Debug("Using backup lock manager")
	}

	// 预计算一次概率归一化和累积概率，减少批量内重复计算
	normalizedPrizes, err := e.prizeSelector.NormalizeProbabilities(prizes)
	if err != nil {
		e.logger.Error("DrawMultipleFromPrizes normalization failed: %v", err)
		return nil, err
	}
	cumulativeProbabilities, err := e.prizeSelector.CalculateCumulativeProbabilities(normalizedPrizes)
	if err != nil {
		e.logger.Error("DrawMultipleFromPrizes cumulative probabilities failed: %v", err)
		return nil, err
	}

	// 计算最优批次大小
	batchSize := calculateOptimalBatchSize(count)

	// 分批处理，每批内部仍然逐个处理以保证细粒度控制
	for batchStart := 0; batchStart < count; batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > count {
			batchEnd = count
		}
		currentBatchSize := batchEnd - batchStart

		// 检查上下文取消
		select {
		case <-ctx.Done():
			e.logger.Info("DrawMultipleFromPrizes cancelled after %d draws", result.Completed)
			result.PartialSuccess = true
			result.LastError = ctx.Err()
			return result, ErrDrawInterrupted
		default:
		}

		// 批次处理：优先尝试批内单次加锁，失败则回退到逐次加锁
		batchLockValue := generateLockValue()
		batchAcquired, batchErr := lockManager.AcquireLock(ctx, lockKey, batchLockValue, DefaultLockExpiration)
		if batchErr == nil && batchAcquired {
			batchSuccesses := 0
			for i := 0; i < currentBatchSize; i++ {
				drawIndex := batchStart + i

				// 更新状态
				drawState.CompletedCount = drawIndex
				drawState.LastUpdateTime = time.Now().Unix()

				// 执行奖品选择（使用预计算累积概率）
				randomValue, randErr := e.random.GenerateFloat()
				if randErr != nil {
					e.logger.Error("DrawMultipleFromPrizes random error at draw %d: %v", drawIndex+1, randErr)
					drawError := DrawError{
						DrawIndex: drawIndex + 1,
						Error:     randErr,
						ErrorMsg:  randErr.Error(),
						Timestamp: time.Now().Unix(),
					}
					result.ErrorDetails = append(result.ErrorDetails, drawError)
					result.Failed++
					result.LastError = randErr
					continue
				}

				idx := findPrizeIndex(cumulativeProbabilities, randomValue)
				sp := normalizedPrizes[idx]
				selectedPrize := &sp

				// 成功完成这次抽奖
				result.PrizeResults = append(result.PrizeResults, selectedPrize)
				result.Completed++
				batchSuccesses++

				// 更新状态
				drawState.PrizeResults = append(drawState.PrizeResults, selectedPrize)
				drawState.CompletedCount = result.Completed

				e.logger.Debug("Draw %d completed successfully: prize=%s (ID: %s)", drawIndex+1, selectedPrize.Name, selectedPrize.ID)

				// 调用进度回调
				if progressCallback != nil {
					progressCallback(result.Completed, count, selectedPrize)
				}
			}

			// 释放批次锁
			if released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, batchLockValue); releaseErr != nil {
				e.logger.Error("Failed to release batch lock: %v", releaseErr)
			} else if !released {
				e.logger.Debug("Batch lock was already released or expired")
			}

			// 记录批次完成情况
			e.logger.Debug("Batch completed: %d/%d draws successful in batch starting at %d",
				batchSuccesses, currentBatchSize, batchStart)
		} else {
			batchSuccesses := 0
			for i := 0; i < currentBatchSize; i++ {
				drawIndex := batchStart + i

				// 更新状态
				drawState.CompletedCount = drawIndex
				drawState.LastUpdateTime = time.Now().Unix()

				// 生成唯一锁值
				lockValue := generateLockValue()

				// 尝试获取分布式锁
				acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, DefaultLockExpiration)
				if err != nil {
					e.logger.Error("DrawMultipleFromPrizes lock acquisition error at draw %d: %v", drawIndex+1, err)

					drawError := DrawError{
						DrawIndex: drawIndex + 1,
						Error:     err,
						ErrorMsg:  err.Error(),
						Timestamp: time.Now().Unix(),
					}
					result.ErrorDetails = append(result.ErrorDetails, drawError)
					result.Failed++
					result.LastError = err

					// 智能错误恢复：判断是否应该中止
					if e.shouldAbortOnError(err) {
						e.logger.Error("Aborting DrawMultipleFromPrizes due to critical error: %v", err)
						result.PartialSuccess = result.Completed > 0
						return result, err
					}

					// 非关键错误，继续下一次抽奖
					e.logger.Info("Continuing DrawMultipleFromPrizes after non-critical error at draw %d", drawIndex+1)
					continue
				}

				if !acquired {
					err := ErrLockAcquisitionFailed
					e.logger.Error("DrawMultipleFromPrizes failed to acquire lock at draw %d", drawIndex+1)

					drawError := DrawError{
						DrawIndex: drawIndex + 1,
						Error:     err,
						ErrorMsg:  err.Error(),
						Timestamp: time.Now().Unix(),
					}
					result.ErrorDetails = append(result.ErrorDetails, drawError)
					result.Failed++
					result.LastError = err
					continue
				}

				// 执行奖品选择（使用预计算的累积概率）
				var selectedPrize *Prize
				randomValue, err := e.random.GenerateFloat()
				if err == nil {
					idx := findPrizeIndex(cumulativeProbabilities, randomValue)
					sp := normalizedPrizes[idx]
					selectedPrize = &sp
				}

				// 立即释放锁
				released, releaseErr := lockManager.ReleaseLock(ctx, lockKey, lockValue)
				if releaseErr != nil {
					e.logger.Error("Failed to release lock for draw %d: %v", drawIndex+1, releaseErr)
				} else if !released {
					e.logger.Debug("Lock was already released or expired at draw %d", drawIndex+1)
				}

				if err != nil {
					e.logger.Error("DrawMultipleFromPrizes prize selection error at draw %d: %v", drawIndex+1, err)

					drawError := DrawError{
						DrawIndex: drawIndex + 1,
						Error:     err,
						ErrorMsg:  err.Error(),
						Timestamp: time.Now().Unix(),
					}
					result.ErrorDetails = append(result.ErrorDetails, drawError)
					result.Failed++
					result.LastError = err
					continue
				}

				// 成功完成这次抽奖
				result.PrizeResults = append(result.PrizeResults, selectedPrize)
				result.Completed++
				batchSuccesses++

				// 更新状态
				drawState.PrizeResults = append(drawState.PrizeResults, selectedPrize)
				drawState.CompletedCount = result.Completed

				e.logger.Debug("Draw %d completed successfully: prize=%s (ID: %s)", drawIndex+1, selectedPrize.Name, selectedPrize.ID)

				// 调用进度回调
				if progressCallback != nil {
					progressCallback(result.Completed, count, selectedPrize)
				}
			}

			// 记录批次完成情况
			e.logger.Debug("Batch completed: %d/%d draws successful in batch starting at %d",
				batchSuccesses, currentBatchSize, batchStart)
		}
	}

	// 更新最终状态
	drawState.CompletedCount = result.Completed
	drawState.LastUpdateTime = time.Now().Unix()

	// 设置部分成功标志
	if result.Failed > 0 && result.Completed > 0 {
		result.PartialSuccess = true
	}

	// 记录最终结果
	duration := time.Since(startTime)
	e.logger.Info("DrawMultipleFromPrizes completed: lockKey=%s, requested=%d, completed=%d, failed=%d, duration=%v",
		lockKey, count, result.Completed, result.Failed, duration)

	// 性能监控记录
	e.performanceMonitor.RecordDraw(result.Failed == 0, duration)

	// 根据结果返回相应的错误
	if result.Completed == 0 {
		e.logger.Error("DrawMultipleFromPrizes failed completely: 0/%d successful", result.TotalRequested)
		return result, result.LastError
	}

	if result.Failed > 0 {
		e.logger.Info("DrawMultipleFromPrizes completed with partial success: %d/%d successful", result.Completed, result.TotalRequested)
		return result, ErrPartialDrawFailure
	}

	e.logger.Info("DrawMultipleFromPrizes successful: lockKey=%s, count=%d", lockKey, result.Completed)
	return result, nil
}

func (e *LotteryEngine) DrawMultipleFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int, progressCallback ProgressCallback) (*MultiDrawResult, error) {
	result, err := e.executeWithBreaker(func() (any, error) {
		return e.drawMultipleFromPrizesWithLockCache(ctx, lockKey, prizes, count, progressCallback)
	})
	if err != nil {
		// For validation errors (invalid parameters, empty prize pool, invalid count), return nil result
		if err == ErrInvalidParameters || err == ErrEmptyPrizePool || err == ErrInvalidCount {
			return nil, err
		}

		// For circuit breaker errors or other runtime errors, return a proper MultiDrawResult structure
		errorResult := &MultiDrawResult{
			Results:        make([]int, 0),
			PrizeResults:   make([]*Prize, 0),
			TotalRequested: count,
			Completed:      0,
			Failed:         count,
			PartialSuccess: false,
			LastError:      err,
			ErrorDetails:   make([]DrawError, 0),
		}
		return errorResult, err
	}

	return result.(*MultiDrawResult), nil
}

// RollbackMultiDraw attempts to rollback a partially completed multi-draw operation
func (e *LotteryEngine) rollbackMultiDraw(ctx context.Context, drawState *DrawState) error {
	// Check for nil draw state first
	if drawState == nil {
		e.logger.Error("RollbackMultiDraw failed: nil draw state")
		return ErrDrawStateCorrupted
	}

	e.logger.Info("RollbackMultiDraw called for lockKey=%s, completed=%d/%d",
		drawState.LockKey, drawState.CompletedCount, drawState.TotalCount)

	// Validate draw state
	if err := drawState.Validate(); err != nil {
		e.logger.Error("RollbackMultiDraw failed: invalid draw state: %v", err)
		return ErrDrawStateCorrupted
	}

	// Create state persistence manager for cleanup operations
	spm := NewStatePersistenceManager(e.redisClient, e.logger)

	// Find all state keys for this lock key to clean up
	stateKeys, err := spm.findStateKeys(ctx, drawState.LockKey)
	if err != nil {
		// Log the error but don't fail the rollback - cleanup is best effort
		e.logger.Error("RollbackMultiDraw failed to find state keys for cleanup: lockKey=%s, error=%v",
			drawState.LockKey, err)
	} else {
		// Clean up all found state keys
		cleanupCount := 0
		for _, key := range stateKeys {
			if cleanupErr := spm.deleteState(ctx, key); cleanupErr != nil {
				// Log cleanup failures but continue with rollback
				e.logger.Error("RollbackMultiDraw failed to cleanup state key: key=%s, error=%v",
					key, cleanupErr)
			} else {
				cleanupCount++
				e.logger.Debug("RollbackMultiDraw cleaned up state key: %s", key)
			}
		}

		if cleanupCount > 0 {
			e.logger.Info("RollbackMultiDraw cleaned up %d state keys for lockKey=%s",
				cleanupCount, drawState.LockKey)
		} else if len(stateKeys) > 0 {
			e.logger.Error("RollbackMultiDraw failed to cleanup any of %d state keys for lockKey=%s",
				len(stateKeys), drawState.LockKey)
		} else {
			e.logger.Debug("RollbackMultiDraw found no state keys to cleanup for lockKey=%s",
				drawState.LockKey)
		}
	}

	// Log rollback completion for audit trail
	e.logger.Info("Rollback completed for lockKey=%s, progress was %.1f%% (%d/%d completed)",
		drawState.LockKey, drawState.Progress(), drawState.CompletedCount, drawState.TotalCount)

	return nil
}

// RollbackMultiDraw attempts to rollback a partially completed multi-draw operation
func (e *LotteryEngine) RollbackMultiDraw(ctx context.Context, drawState *DrawState) error {
	_, err := e.executeWithBreaker(func() (any, error) {
		return nil, e.rollbackMultiDraw(ctx, drawState)
	})

	return err
}

// SaveDrawState saves the current state of a multi-draw operation to Redis
func (e *LotteryEngine) saveDrawState(ctx context.Context, drawState *DrawState) error {
	// Check for nil draw state first
	if drawState == nil {
		e.logger.Error("SaveDrawState failed: nil draw state")
		return ErrDrawStateCorrupted
	}

	e.logger.Debug("SaveDrawState called for lockKey=%s, progress=%.1f%%",
		drawState.LockKey, drawState.Progress())

	// Validate draw state
	if err := drawState.Validate(); err != nil {
		e.logger.Error("SaveDrawState failed: invalid draw state: %v", err)
		return ErrDrawStateCorrupted
	}

	// Update timestamp
	drawState.LastUpdateTime = time.Now().Unix()

	// Create state persistence manager
	spm := NewStatePersistenceManager(e.redisClient, e.logger)

	// Generate Redis key for this state
	stateKey := generateStateKey(drawState.LockKey)
	if stateKey == "" {
		e.logger.Error("SaveDrawState failed: unable to generate state key for lockKey=%s", drawState.LockKey)
		return ErrInvalidParameters
	}

	// Save state to Redis with TTL
	if err := spm.saveState(ctx, stateKey, drawState, DefaultStateTTL); err != nil {
		e.logger.Error("SaveDrawState failed to save to Redis: lockKey=%s, error=%v", drawState.LockKey, err)
		return err
	}

	e.logger.Info("Draw state saved successfully: lockKey=%s, stateKey=%s, progress=%.1f%%",
		drawState.LockKey, stateKey, drawState.Progress())
	return nil
}

// SaveDrawState saves the current state of a multi-draw operation to Redis
func (e *LotteryEngine) SaveDrawState(ctx context.Context, drawState *DrawState) error {
	_, err := e.executeWithBreaker(func() (any, error) {
		return nil, e.saveDrawState(ctx, drawState)
	})

	return err
}

// LoadDrawState loads a previously saved draw state from Redis
func (e *LotteryEngine) loadDrawState(ctx context.Context, lockKey string) (*DrawState, error) {
	e.logger.Debug("LoadDrawState called for lockKey=%s", lockKey)

	if lockKey == "" {
		e.logger.Error("LoadDrawState failed: empty lock key")
		return nil, ErrInvalidParameters
	}

	// Create state persistence manager
	spm := NewStatePersistenceManager(e.redisClient, e.logger)

	// Find all state keys for this lock key
	stateKeys, err := spm.findStateKeys(ctx, lockKey)
	if err != nil {
		e.logger.Error("LoadDrawState failed to find state keys: lockKey=%s, error=%v", lockKey, err)
		return nil, err
	}

	if len(stateKeys) == 0 {
		e.logger.Debug("No saved state found for lockKey=%s", lockKey)
		return nil, nil
	}

	// Find the most recent state key (based on operation ID timestamp)
	var mostRecentKey string
	var mostRecentOperationID string
	for _, key := range stateKeys {
		_, operationID, parseErr := parseStateKey(key)
		if parseErr != nil {
			e.logger.Debug("Skipping invalid state key: %s, error=%v", key, parseErr)
			continue
		}

		if mostRecentOperationID == "" || operationID > mostRecentOperationID {
			mostRecentOperationID = operationID
			mostRecentKey = key
		}
	}

	if mostRecentKey == "" {
		e.logger.Debug("No valid state keys found for lockKey=%s", lockKey)
		return nil, nil
	}

	// Load the most recent state
	drawState, err := spm.loadState(ctx, mostRecentKey)
	if err != nil {
		e.logger.Error("LoadDrawState failed to load state: lockKey=%s, stateKey=%s, error=%v",
			lockKey, mostRecentKey, err)
		return nil, err
	}

	if drawState == nil {
		e.logger.Debug("State key exists but no data found: lockKey=%s, stateKey=%s", lockKey, mostRecentKey)
		return nil, nil
	}

	// Validate the loaded state
	if err := drawState.Validate(); err != nil {
		e.logger.Error("LoadDrawState loaded invalid state: lockKey=%s, stateKey=%s, error=%v",
			lockKey, mostRecentKey, err)
		return nil, ErrDrawStateCorrupted
	}

	e.logger.Info("Draw state loaded successfully: lockKey=%s, stateKey=%s, progress=%.1f%%",
		lockKey, mostRecentKey, drawState.Progress())
	return drawState, nil
}

// LoadDrawState loads a previously saved draw state from Redis
func (e *LotteryEngine) LoadDrawState(ctx context.Context, lockKey string) (*DrawState, error) {
	result, err := e.executeWithBreaker(func() (any, error) {
		return e.loadDrawState(ctx, lockKey)
	})
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	return result.(*DrawState), nil
}

// ResumeMultiDrawInRange resumes a previously interrupted multi-draw range operation
func (e *LotteryEngine) resumeMultiDrawInRange(
	ctx context.Context, lockKey string, min, max, count int,
) (*MultiDrawResult, error) {
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
		return e.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)
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
	remainingResult, err := e.DrawMultipleInRange(ctx, lockKey, min, max, remainingCount, nil)
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

// ResumeMultiDrawInRange resumes a previously interrupted multi-draw range operation
func (e *LotteryEngine) ResumeMultiDrawInRange(ctx context.Context, lockKey string, min, max, count int) (*MultiDrawResult, error) {
	result, err := e.executeWithBreaker(func() (any, error) {
		return e.resumeMultiDrawInRange(ctx, lockKey, min, max, count)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// ResumeMultiDrawFromPrizes resumes a previously interrupted multi-draw prize operation
func (e *LotteryEngine) resumeMultiDrawFromPrizes(
	ctx context.Context, lockKey string, prizes []Prize, count int,
) (*MultiDrawResult, error) {
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
		return e.DrawMultipleFromPrizes(ctx, lockKey, prizes, count, nil)
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
	remainingResult, err := e.DrawMultipleFromPrizes(ctx, lockKey, prizes, remainingCount, nil)
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

// ResumeMultiDrawFromPrizes resumes a previously interrupted multi-draw prize operation
func (e *LotteryEngine) ResumeMultiDrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int) (*MultiDrawResult, error) {
	result, err := e.executeWithBreaker(func() (any, error) {
		return e.resumeMultiDrawFromPrizes(ctx, lockKey, prizes, count)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// PerformanceMetrics 获取性能指标
func (e *LotteryEngine) PerformanceMetrics() PerformanceMetrics {
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

// EnableCircuitBreaker 启用熔断器
func (e *LotteryEngine) EnableCircuitBreaker() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.configManager.config.CircuitBreaker.Enabled = true

	if e.circuitBreaker == nil {
		breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        e.configManager.config.CircuitBreaker.Name,
			MaxRequests: e.configManager.config.CircuitBreaker.MaxRequests,
			Interval:    e.configManager.config.CircuitBreaker.Interval,
			Timeout:     e.configManager.config.CircuitBreaker.Timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				// 当请求数达到最小要求且失败率超过阈值时触发熔断
				return counts.Requests >= e.configManager.config.CircuitBreaker.MinRequests &&
					float64(counts.TotalFailures)/float64(counts.Requests) >= e.configManager.config.CircuitBreaker.FailureRatio
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if e.configManager.config.CircuitBreaker.OnStateChange && e.logger != nil {
					e.logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
				}
			},
		})
		e.circuitBreaker = breaker
	}
}

// DisableCircuitBreaker 禁用熔断器
func (e *LotteryEngine) DisableCircuitBreaker() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.configManager.config.CircuitBreaker.Enabled = false
}

// GetCircuitBreakerState 获取熔断器状态
func (e *LotteryEngine) GetCircuitBreakerState() string {
	if e.circuitBreaker == nil {
		return "disabled"
	}

	switch e.circuitBreaker.State() {
	case gobreaker.StateClosed:
		return "closed"
	case gobreaker.StateHalfOpen:
		return "half-open"
	case gobreaker.StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// GetCircuitBreakerCounts 获取熔断器统计信息
func (e *LotteryEngine) GetCircuitBreakerCounts() gobreaker.Counts {
	if e.circuitBreaker == nil {
		return gobreaker.Counts{}
	}

	return e.circuitBreaker.Counts()
}

// ResetCircuitBreaker 重置熔断器 (重新创建熔断器实例)
func (e *LotteryEngine) ResetCircuitBreaker() {
	if e.circuitBreaker != nil {
		// gobreaker 没有 Reset 方法，我们重新创建一个实例
		e.circuitBreaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        e.configManager.config.CircuitBreaker.Name,
			MaxRequests: e.configManager.config.CircuitBreaker.MaxRequests,
			Interval:    e.configManager.config.CircuitBreaker.Interval,
			Timeout:     e.configManager.config.CircuitBreaker.Timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
				return counts.Requests >= e.configManager.config.CircuitBreaker.MinRequests && failureRatio >= e.configManager.config.CircuitBreaker.FailureRatio
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if e.configManager.config.CircuitBreaker.OnStateChange && e.logger != nil {
					e.logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
				}
			},
		})
		if e.logger != nil {
			e.logger.Info("Circuit breaker '%s' has been reset (recreated)", e.configManager.config.CircuitBreaker.Name)
		}
	}
}

func (e *LotteryEngine) CircuitBreakerHealthCheck() map[string]any {
	result := map[string]any{
		"circuit_breaker_enabled": e.configManager.config.CircuitBreaker.Enabled,
	}

	if e.configManager.config.CircuitBreaker.Enabled && e.configManager.config.CircuitBreaker != nil {
		state := e.GetCircuitBreakerState()
		counts := e.GetCircuitBreakerCounts()

		result["state"] = state
		result["requests"] = counts.Requests
		result["total_successes"] = counts.TotalSuccesses
		result["total_failures"] = counts.TotalFailures
		result["consecutive_successes"] = counts.ConsecutiveSuccesses
		result["consecutive_failures"] = counts.ConsecutiveFailures

		// 计算成功率
		if counts.Requests > 0 {
			result["success_rate"] = float64(counts.TotalSuccesses) / float64(counts.Requests)
			result["failure_rate"] = float64(counts.TotalFailures) / float64(counts.Requests)
		} else {
			result["success_rate"] = 0.0
			result["failure_rate"] = 0.0
		}

		// 健康状态判断
		healthy := true
		switch state {
		case "open":
			healthy = false
		case "half-open":
			// 半开状态下，如果连续失败次数过多，认为不健康
			if counts.ConsecutiveFailures > 2 {
				healthy = false
			}
		}

		result["healthy"] = healthy
	} else {
		result["state"] = "disabled"
		result["healthy"] = true
	}

	return result
}

func (e *LotteryEngine) CircuitBreakerMetrics() map[string]any {
	metrics := map[string]any{
		"circuit_breaker_enabled": e.configManager.config.CircuitBreaker.Enabled,
		"timestamp":               time.Now().Unix(),
	}

	if e.configManager.config.CircuitBreaker.Enabled && e.configManager.config.CircuitBreaker != nil {
		stateToNumeric := func(state string) int {
			switch state {
			case "closed":
				return 0
			case "half-open":
				return 1
			case "open":
				return 2
			default:
				return -1
			}
		}

		state := e.GetCircuitBreakerState()
		counts := e.GetCircuitBreakerCounts()

		// 状态指标
		metrics["circuit_breaker_state"] = state
		metrics["circuit_breaker_state_numeric"] = stateToNumeric(state)

		// 计数指标
		metrics["circuit_breaker_requests_total"] = counts.Requests
		metrics["circuit_breaker_successes_total"] = counts.TotalSuccesses
		metrics["circuit_breaker_failures_total"] = counts.TotalFailures
		metrics["circuit_breaker_consecutive_successes"] = counts.ConsecutiveSuccesses
		metrics["circuit_breaker_consecutive_failures"] = counts.ConsecutiveFailures

		// 比率指标
		if counts.Requests > 0 {
			metrics["circuit_breaker_success_rate"] = float64(counts.TotalSuccesses) / float64(counts.Requests)
			metrics["circuit_breaker_failure_rate"] = float64(counts.TotalFailures) / float64(counts.Requests)
		} else {
			metrics["circuit_breaker_success_rate"] = 0.0
			metrics["circuit_breaker_failure_rate"] = 0.0
		}

		// 配置指标
		metrics["circuit_breaker_max_requests"] = e.configManager.config.CircuitBreaker.MaxRequests
		metrics["circuit_breaker_failure_ratio_threshold"] = e.configManager.config.CircuitBreaker.FailureRatio
		metrics["circuit_breaker_min_requests"] = e.configManager.config.CircuitBreaker.MinRequests
		metrics["circuit_breaker_interval_seconds"] = e.configManager.config.CircuitBreaker.Interval.Seconds()
		metrics["circuit_breaker_timeout_seconds"] = e.configManager.config.CircuitBreaker.Timeout.Seconds()
	}

	return metrics
}

// DrawInRangeWithMonitoring 带性能监控的范围抽奖
func (e *LotteryEngine) DrawInRangeWithMonitoring(ctx context.Context, lockKey string, min, max int) (int, error) {
	startTime := time.Now()

	// 调用原始的抽奖方法
	result, err := e.DrawInRange(ctx, lockKey, min, max)

	duration := time.Since(startTime)
	switch err {
	case ErrLockAcquisitionFailed:
		e.performanceMonitor.RecordLockAcquisition(false, duration)
	case nil:
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

	switch err {
	case ErrLockAcquisitionFailed:
		e.performanceMonitor.RecordLockAcquisition(false, duration)
	case nil:
		e.performanceMonitor.RecordLockAcquisition(true, duration)
		e.performanceMonitor.RecordLockRelease()
	}

	if err != nil && err != ErrLockAcquisitionFailed {
		e.performanceMonitor.RecordRedisError()
	}

	return result, err
}
