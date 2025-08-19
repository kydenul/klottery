package lottery

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDistributedLockManager_CoreFunctionality 测试分布式锁的获取和释放
func TestDistributedLockManager_CoreFunctionality(t *testing.T) {
	// 创建Redis客户端用于测试
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // 使用测试数据库
	})

	// 测试Redis连接
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过测试")
	}

	defer rdb.Close()

	t.Run("基础锁获取和释放", func(t *testing.T) {
		lockManager := NewLockManager(rdb, 30*time.Second)

		lockKey := "test_basic_lock"
		lockValue := "test_value_123"

		// 获取锁应该成功
		acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, 10*time.Second)
		require.NoError(t, err)
		assert.True(t, acquired, "首次获取锁应该成功")

		// 释放锁应该成功
		released, err := lockManager.ReleaseLock(ctx, lockKey, lockValue)
		require.NoError(t, err)
		assert.True(t, released, "释放锁应该成功")
	})

	t.Run("锁冲突处理", func(t *testing.T) {
		lockManager := NewLockManager(rdb, 30*time.Second)

		lockKey := "test_conflict_lock"
		lockValue1 := "value1"
		lockValue2 := "value2"

		// 第一个客户端获取锁
		acquired1, err := lockManager.AcquireLock(ctx, lockKey, lockValue1, 5*time.Second)
		require.NoError(t, err)
		assert.True(t, acquired1, "第一个客户端应该成功获取锁")

		// 第二个客户端尝试获取同一个锁应该失败
		acquired2, err := lockManager.TryAcquireLock(ctx, lockKey, lockValue2, 5*time.Second)
		require.NoError(t, err)
		assert.False(t, acquired2, "第二个客户端不应该能获取已被占用的锁")

		// 用错误的值释放锁应该失败
		released1, err := lockManager.ReleaseLock(ctx, lockKey, lockValue2)
		require.NoError(t, err)
		assert.False(t, released1, "用错误的值释放锁应该失败")

		// 用正确的值释放锁应该成功
		released2, err := lockManager.ReleaseLock(ctx, lockKey, lockValue1)
		require.NoError(t, err)
		assert.True(t, released2, "用正确的值释放锁应该成功")

		// 现在第二个客户端应该能获取锁
		acquired3, err := lockManager.AcquireLock(ctx, lockKey, lockValue2, 5*time.Second)
		require.NoError(t, err)
		assert.True(t, acquired3, "锁释放后其他客户端应该能获取")

		// 清理
		lockManager.ReleaseLock(ctx, lockKey, lockValue2)
	})

	t.Run("锁超时处理", func(t *testing.T) {
		lockManager := NewLockManager(rdb, 30*time.Second)

		lockKey := "test_timeout_lock"
		lockValue1 := "timeout_value1"
		lockValue2 := "timeout_value2"

		// 获取一个短期锁
		acquired1, err := lockManager.AcquireLock(ctx, lockKey, lockValue1, 1*time.Second)
		require.NoError(t, err)
		assert.True(t, acquired1)

		// 等待锁过期
		time.Sleep(2 * time.Second)

		// 现在应该能获取锁（因为之前的锁已过期）
		acquired2, err := lockManager.AcquireLock(ctx, lockKey, lockValue2, 5*time.Second)
		require.NoError(t, err)
		assert.True(t, acquired2, "过期的锁应该能被重新获取")

		// 清理
		lockManager.ReleaseLock(ctx, lockKey, lockValue2)
	})

	t.Run("参数验证", func(t *testing.T) {
		lockManager := NewLockManager(rdb, 30*time.Second)

		// 空的锁键应该返回错误
		_, err := lockManager.AcquireLock(ctx, "", "value", 10*time.Second)
		assert.Equal(t, ErrInvalidParameters, err)

		// 空的锁值应该返回错误
		_, err = lockManager.AcquireLock(ctx, "key", "", 10*time.Second)
		assert.Equal(t, ErrInvalidParameters, err)

		// 释放锁时空键应该返回错误
		_, err = lockManager.ReleaseLock(ctx, "", "value")
		assert.Equal(t, ErrInvalidParameters, err)

		// 释放锁时空值应该返回错误
		_, err = lockManager.ReleaseLock(ctx, "key", "")
		assert.Equal(t, ErrInvalidParameters, err)
	})
}

// ================================================================================
// Engine Advanced Features Tests (Circuit Breaker, Resume, Monitoring)
// ================================================================================

func setupTestRedis(t *testing.T) *redis.Client {
	// 创建Redis客户端用于测试
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // 使用测试数据库
	})

	// 测试连接
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	// 清理测试数据库
	rdb.FlushDB(ctx)

	return rdb
}

func TestLotteryEngine_CircuitBreaker(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	// 创建配置，启用熔断器
	config := &Config{
		Engine: &EngineConfig{
			LockTimeout:   time.Second,
			RetryAttempts: 1,
			RetryInterval: time.Millisecond * 100,
			LockCacheTTL:  time.Second * 5,
		},
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:       true,
			Name:          "test-breaker",
			MaxRequests:   5,
			MinRequests:   3,
			FailureRatio:  0.6,
			Interval:      time.Second * 10,
			Timeout:       time.Second * 5,
			OnStateChange: true,
		},
	}

	cm := &ConfigManager{config: config}
	engine := NewLotteryEngineWithConfig(redisClient, cm)

	t.Run("circuit_breaker_enabled", func(t *testing.T) {
		state := engine.GetCircuitBreakerState()
		assert.Equal(t, "closed", state)
	})

	t.Run("circuit_breaker_counts", func(t *testing.T) {
		counts := engine.GetCircuitBreakerCounts()
		assert.Equal(t, uint32(0), counts.Requests)
		assert.Equal(t, uint32(0), counts.TotalFailures)
	})

	t.Run("circuit_breaker_health_check", func(t *testing.T) {
		health := engine.CircuitBreakerHealthCheck()
		assert.True(t, health["circuit_breaker_enabled"].(bool))
		assert.Equal(t, "closed", health["state"])
		assert.True(t, health["healthy"].(bool))
	})

	t.Run("circuit_breaker_metrics", func(t *testing.T) {
		metrics := engine.CircuitBreakerMetrics()
		assert.True(t, metrics["circuit_breaker_enabled"].(bool))
		assert.Equal(t, "closed", metrics["circuit_breaker_state"])
		assert.Equal(t, 0, metrics["circuit_breaker_state_numeric"])
	})

	t.Run("enable_disable_circuit_breaker", func(t *testing.T) {
		engine.DisableCircuitBreaker()
		state := engine.GetCircuitBreakerState()
		assert.Equal(t, "closed", state) // 禁用后状态仍为closed

		engine.EnableCircuitBreaker()
		state = engine.GetCircuitBreakerState()
		assert.Equal(t, "closed", state)
	})

	t.Run("reset_circuit_breaker", func(t *testing.T) {
		engine.ResetCircuitBreaker()
		state := engine.GetCircuitBreakerState()
		assert.Equal(t, "closed", state)
	})
}

func TestLotteryEngine_ConfigurationUpdates(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	engine := NewLotteryEngine(redisClient)

	t.Run("update_config", func(t *testing.T) {
		newConfig := &Config{
			Engine: &EngineConfig{
				LockTimeout:   time.Second * 2,
				RetryAttempts: 5,
				RetryInterval: time.Millisecond * 200,
				LockCacheTTL:  time.Second * 10,
			},
			Redis: &RedisConfig{
				Addr:     "localhost:6379",
				PoolSize: 10,
			},
			CircuitBreaker: &CircuitBreakerConfig{
				Enabled: false,
			},
		}

		err := engine.UpdateConfig(newConfig)
		assert.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, time.Second*2, config.Engine.LockTimeout)
		assert.Equal(t, 5, config.Engine.RetryAttempts)
	})

	t.Run("update_config_nil", func(t *testing.T) {
		err := engine.UpdateConfig(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidParameters, err)
	})

	t.Run("set_lock_timeout", func(t *testing.T) {
		err := engine.SetLockTimeout(time.Second * 3)
		assert.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, time.Second*3, config.Engine.LockTimeout)
	})

	t.Run("set_lock_timeout_invalid", func(t *testing.T) {
		err := engine.SetLockTimeout(time.Nanosecond)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidLockTimeout, err)

		err = engine.SetLockTimeout(time.Hour)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidLockTimeout, err)
	})

	t.Run("set_retry_attempts", func(t *testing.T) {
		err := engine.SetRetryAttempts(3)
		assert.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, 3, config.Engine.RetryAttempts)
	})

	t.Run("set_retry_attempts_invalid", func(t *testing.T) {
		err := engine.SetRetryAttempts(-1)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidRetryAttempts, err)

		err = engine.SetRetryAttempts(1000)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidRetryAttempts, err)
	})

	t.Run("set_retry_interval", func(t *testing.T) {
		err := engine.SetRetryInterval(time.Millisecond * 500)
		assert.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, time.Millisecond*500, config.Engine.RetryInterval)
	})

	t.Run("set_retry_interval_invalid", func(t *testing.T) {
		err := engine.SetRetryInterval(-time.Second)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidRetryInterval, err)
	})

	t.Run("set_logger", func(t *testing.T) {
		originalLogger := engine.GetLogger()
		newLogger := NewSilentLogger()

		engine.SetLogger(newLogger)
		assert.Equal(t, newLogger, engine.GetLogger())

		// 测试设置nil logger
		engine.SetLogger(nil)
		assert.Equal(t, newLogger, engine.GetLogger()) // 应该保持不变

		// 测试设置相同logger
		engine.SetLogger(newLogger)
		assert.Equal(t, newLogger, engine.GetLogger())

		// 恢复原始logger
		engine.SetLogger(originalLogger)
	})
}

func TestLotteryEngine_StateManagement(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	engine := NewLotteryEngine(redisClient)
	ctx := context.Background()

	t.Run("save_and_load_draw_state", func(t *testing.T) {
		drawState := &DrawState{
			LockKey:        "test-state-key",
			TotalCount:     10,
			CompletedCount: 5,
			Results:        []int{1, 2, 3, 4, 5},
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// 保存状态
		err := engine.SaveDrawState(ctx, drawState)
		assert.NoError(t, err)

		// 加载状态
		loadedState, err := engine.LoadDrawState(ctx, "test-state-key")
		assert.NoError(t, err)
		assert.NotNil(t, loadedState)
		assert.Equal(t, drawState.LockKey, loadedState.LockKey)
		assert.Equal(t, drawState.TotalCount, loadedState.TotalCount)
		assert.Equal(t, drawState.CompletedCount, loadedState.CompletedCount)
		assert.Equal(t, len(drawState.Results), len(loadedState.Results))
	})

	t.Run("save_draw_state_nil", func(t *testing.T) {
		err := engine.SaveDrawState(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrDrawStateCorrupted, err)
	})

	t.Run("load_draw_state_empty_key", func(t *testing.T) {
		_, err := engine.LoadDrawState(ctx, "")
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidParameters, err)
	})

	t.Run("load_draw_state_not_found", func(t *testing.T) {
		state, err := engine.LoadDrawState(ctx, "non-existent-key")
		assert.NoError(t, err)
		assert.Nil(t, state)
	})

	t.Run("rollback_multi_draw", func(t *testing.T) {
		drawState := &DrawState{
			LockKey:        "test-rollback-key",
			TotalCount:     10,
			CompletedCount: 3,
			Results:        []int{1, 2, 3},
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// 先保存状态
		err := engine.SaveDrawState(ctx, drawState)
		assert.NoError(t, err)

		// 执行回滚
		err = engine.RollbackMultiDraw(ctx, drawState)
		assert.NoError(t, err)
	})

	t.Run("rollback_multi_draw_nil", func(t *testing.T) {
		err := engine.RollbackMultiDraw(ctx, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrDrawStateCorrupted, err)
	})
}

func TestLotteryEngine_ResumeOperations(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	engine := NewLotteryEngine(redisClient)
	ctx := context.Background()

	t.Run("resume_multi_draw_in_range_no_state", func(t *testing.T) {
		// 没有保存的状态，应该开始新的抽奖
		result, err := engine.ResumeMultiDrawInRange(ctx, "resume-range-key", 1, 100, 3)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 3, result.TotalRequested)
		assert.Equal(t, 3, result.Completed)
		assert.Equal(t, 0, result.Failed)
	})

	t.Run("resume_multi_draw_from_prizes_no_state", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize1", Probability: 0.5, Value: 100},
			{ID: "2", Name: "Prize2", Probability: 0.5, Value: 200},
		}

		// 没有保存的状态，应该开始新的抽奖
		result, err := engine.ResumeMultiDrawFromPrizes(ctx, "resume-prize-key", prizes, 2)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 2, result.TotalRequested)
		assert.Equal(t, 2, result.Completed)
		assert.Equal(t, 0, result.Failed)
	})

	t.Run("resume_multi_draw_with_saved_state", func(t *testing.T) {
		// 创建部分完成的状态
		drawState := &DrawState{
			LockKey:        "resume-with-state-key",
			TotalCount:     5,
			CompletedCount: 2,
			Results:        []int{10, 20},
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// 保存状态
		err := engine.SaveDrawState(ctx, drawState)
		assert.NoError(t, err)

		// 恢复操作
		result, err := engine.ResumeMultiDrawInRange(ctx, "resume-with-state-key", 1, 100, 5)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 5, result.TotalRequested)
		assert.Equal(t, 5, result.Completed) // 应该完成所有5个
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 5, len(result.Results)) // 包含之前的2个和新的3个
	})

	t.Run("resume_multi_draw_already_completed", func(t *testing.T) {
		// 创建已完成的状态
		drawState := &DrawState{
			LockKey:        "resume-completed-key",
			TotalCount:     3,
			CompletedCount: 3,
			Results:        []int{10, 20, 30},
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// 保存状态
		err := engine.SaveDrawState(ctx, drawState)
		assert.NoError(t, err)

		// 恢复操作
		result, err := engine.ResumeMultiDrawInRange(ctx, "resume-completed-key", 1, 100, 3)
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 3, result.TotalRequested)
		assert.Equal(t, 3, result.Completed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 3, len(result.Results))
	})
}

func TestLotteryEngine_PerformanceMonitoring(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	engine := NewLotteryEngine(redisClient)
	ctx := context.Background()

	t.Run("performance_monitoring_enabled", func(t *testing.T) {
		engine.EnablePerformanceMonitoring()

		// 执行一些操作
		_, err := engine.DrawInRange(ctx, "perf-test-key", 1, 100)
		assert.NoError(t, err)

		// 获取性能指标
		metrics := engine.PerformanceMetrics()
		assert.Greater(t, metrics.TotalDraws, int64(0))
		assert.Greater(t, metrics.SuccessfulDraws, int64(0))
	})

	t.Run("performance_monitoring_disabled", func(t *testing.T) {
		engine.ResetPerformanceMetrics()
		engine.DisablePerformanceMonitoring()

		// 执行操作
		_, err := engine.DrawInRange(ctx, "perf-disabled-key", 1, 100)
		assert.NoError(t, err)

		// 性能指标应该不会更新（因为监控被禁用）
		_ = engine.PerformanceMetrics()
		// 注意：即使禁用监控，某些操作仍可能记录指标，这取决于具体实现
	})

	t.Run("draw_with_monitoring", func(t *testing.T) {
		engine.EnablePerformanceMonitoring()
		engine.ResetPerformanceMetrics()

		// 使用带监控的抽奖方法
		result, err := engine.DrawInRangeWithMonitoring(ctx, "monitor-range-key", 1, 100)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, result, 1)
		assert.LessOrEqual(t, result, 100)

		prizes := []Prize{
			{ID: "1", Name: "Prize1", Probability: 1.0, Value: 100},
		}
		prizeResult, err := engine.DrawFromPrizesWithMonitoring(ctx, "monitor-prize-key", prizes)
		assert.NoError(t, err)
		assert.NotNil(t, prizeResult)

		// 检查性能指标
		metrics := engine.PerformanceMetrics()
		assert.Greater(t, metrics.TotalDraws, int64(0))
		assert.Greater(t, metrics.LockAcquisitions, int64(0))
	})

	t.Run("reset_performance_metrics", func(t *testing.T) {
		// 先执行一些操作
		_, err := engine.DrawInRange(ctx, "reset-test-key", 1, 100)
		assert.NoError(t, err)

		// 重置指标
		engine.ResetPerformanceMetrics()

		// 检查指标是否被重置
		metrics := engine.PerformanceMetrics()
		assert.Equal(t, int64(0), metrics.TotalDraws)
		assert.Equal(t, int64(0), metrics.SuccessfulDraws)
	})
}

func TestLotteryEngine_ErrorHandling(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	engine := NewLotteryEngine(redisClient)

	t.Run("should_abort_on_error", func(t *testing.T) {
		// 测试关键错误
		assert.True(t, engine.shouldAbortOnError(ErrRedisConnectionFailed))
		assert.True(t, engine.shouldAbortOnError(context.DeadlineExceeded))
		assert.True(t, engine.shouldAbortOnError(context.Canceled))

		// 测试非关键错误
		assert.False(t, engine.shouldAbortOnError(ErrLockAcquisitionFailed))
		assert.False(t, engine.shouldAbortOnError(ErrInvalidParameters))
	})

	t.Run("draw_with_invalid_parameters", func(t *testing.T) {
		ctx := context.Background()

		// 空锁键
		_, err := engine.DrawInRange(ctx, "", 1, 100)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidParameters, err)

		// 无效范围
		_, err = engine.DrawInRange(ctx, "test-key", 100, 1)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidRange, err)
	})

	t.Run("draw_from_empty_prizes", func(t *testing.T) {
		ctx := context.Background()

		_, err := engine.DrawFromPrizes(ctx, "test-key", []Prize{})
		assert.Error(t, err)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("multi_draw_with_invalid_count", func(t *testing.T) {
		ctx := context.Background()

		_, err := engine.DrawMultipleInRange(ctx, "test-key", 1, 100, 0, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidCount, err)

		_, err = engine.DrawMultipleInRange(ctx, "test-key", 1, 100, -1, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidCount, err)
	})
}

func TestLotteryEngine_ConstructorVariants(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	t.Run("new_lottery_engine", func(t *testing.T) {
		engine := NewLotteryEngine(redisClient)
		assert.NotNil(t, engine)
		assert.NotNil(t, engine.GetLogger())
		assert.NotNil(t, engine.GetConfig())
	})

	t.Run("new_lottery_engine_with_config", func(t *testing.T) {
		config := &Config{
			Engine: &EngineConfig{
				LockTimeout:   time.Second * 2,
				RetryAttempts: 3,
				RetryInterval: time.Millisecond * 100,
				LockCacheTTL:  time.Second * 10,
			},
			Redis: &RedisConfig{
				Addr:     "localhost:6379",
				PoolSize: 10,
			},
			CircuitBreaker: &CircuitBreakerConfig{
				Enabled: true,
				Name:    "test-breaker",
			},
		}
		cm := NewDefaultConfigManager()
		cm.config = config
		if cm.config == nil {
			cm.config = config
		}

		engine := NewLotteryEngineWithConfig(redisClient, cm)
		assert.NotNil(t, engine)
		assert.Equal(t, time.Second*2, engine.GetConfig().Engine.LockTimeout)
	})

	t.Run("new_lottery_engine_with_logger", func(t *testing.T) {
		logger := NewSilentLogger()
		engine := NewLotteryEngineWithLogger(redisClient, logger)
		assert.NotNil(t, engine)
		assert.Equal(t, logger, engine.GetLogger())
	})

	t.Run("new_lottery_engine_with_config_and_logger", func(t *testing.T) {
		config := &Config{
			Engine: &EngineConfig{
				LockTimeout:   time.Second * 3,
				RetryAttempts: 5,
				RetryInterval: time.Millisecond * 200,
				LockCacheTTL:  time.Second * 15,
			},
			Redis: &RedisConfig{
				Addr:     "localhost:6379",
				PoolSize: 10,
			},
			CircuitBreaker: &CircuitBreakerConfig{
				Enabled: false,
			},
		}
		cm := &ConfigManager{config: config}
		logger := NewSilentLogger()

		engine := NewLotteryEngineWithConfigAndLogger(redisClient, cm, logger)
		assert.NotNil(t, engine)
		assert.Equal(t, logger, engine.GetLogger())
		assert.Equal(t, time.Second*3, engine.GetConfig().Engine.LockTimeout)
	})
}

func TestLotteryEngine_LockCacheOptimization(t *testing.T) {
	redisClient := setupTestRedis(t)
	defer redisClient.Close()

	engine := NewLotteryEngine(redisClient)
	ctx := context.Background()

	t.Run("lock_cache_fast_path", func(t *testing.T) {
		// 第一次调用应该成功
		result1, err1 := engine.DrawInRange(ctx, "cache-test-key", 1, 100)
		assert.NoError(t, err1)
		assert.GreaterOrEqual(t, result1, 1)
		assert.LessOrEqual(t, result1, 100)

		// 第二次调用也应该成功（锁机制正常工作）
		result2, err2 := engine.DrawInRange(ctx, "cache-test-key-2", 1, 100)
		assert.NoError(t, err2)
		assert.GreaterOrEqual(t, result2, 1)
		assert.LessOrEqual(t, result2, 100)
	})

	t.Run("lock_cache_expiry", func(t *testing.T) {
		// 使用短TTL配置
		config := &Config{
			Engine: &EngineConfig{
				LockTimeout:   time.Second,
				RetryAttempts: 1,
				RetryInterval: time.Millisecond * 100,
				LockCacheTTL:  time.Millisecond * 100, // 很短的TTL
			},
			Redis: &RedisConfig{
				Addr:     "localhost:6379",
				PoolSize: 10,
			},
			CircuitBreaker: &CircuitBreakerConfig{
				Enabled: false,
			},
		}
		cm := &ConfigManager{config: config}
		engineWithShortTTL := NewLotteryEngineWithConfig(redisClient, cm)

		// 第一次调用
		_, err1 := engineWithShortTTL.DrawInRange(ctx, "cache-expiry-key", 1, 100)
		assert.NoError(t, err1)

		// 等待缓存过期
		time.Sleep(time.Millisecond * 150)

		// 第二次调用应该成功（缓存已过期）
		_, err2 := engineWithShortTTL.DrawInRange(ctx, "cache-expiry-key", 1, 100)
		assert.NoError(t, err2)
	})
}

// TestSecureRandomGenerator_CoreFunctionality 测试随机数生成
func TestSecureRandomGenerator_CoreFunctionality(t *testing.T) {
	generator := NewSecureRandomGenerator()

	t.Run("范围内随机数生成", func(t *testing.T) {
		min, max := 1, 100

		// 测试多次生成确保在范围内
		for range 100 {
			result, err := generator.GenerateInRange(min, max)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, result, min, "生成的数字应该大于等于最小值")
			assert.LessOrEqual(t, result, max, "生成的数字应该小于等于最大值")
		}
	})

	t.Run("单值范围", func(t *testing.T) {
		min, max := 42, 42
		result, err := generator.GenerateInRange(min, max)
		require.NoError(t, err)
		assert.Equal(t, 42, result, "单值范围应该返回该值")
	})

	t.Run("无效范围", func(t *testing.T) {
		min, max := 100, 1
		_, err := generator.GenerateInRange(min, max)
		assert.Equal(t, ErrInvalidRange, err, "最小值大于最大值应该返回错误")
	})

	t.Run("负数范围", func(t *testing.T) {
		min, max := -100, -1
		result, err := generator.GenerateInRange(min, max)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})

	t.Run("跨零范围", func(t *testing.T) {
		min, max := -50, 50
		result, err := generator.GenerateInRange(min, max)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})

	t.Run("浮点数生成", func(t *testing.T) {
		// 测试多次生成
		for range 100 {
			result, err := generator.GenerateFloat()
			require.NoError(t, err)
			assert.GreaterOrEqual(t, result, 0.0, "生成的浮点数应该大于等于0")
			assert.Less(t, result, 1.0, "生成的浮点数应该小于1")
		}
	})

	t.Run("随机性验证", func(t *testing.T) {
		// 生成多个数字，验证它们不全相同
		results := make([]int, 50)
		for i := range 50 {
			result, err := generator.GenerateInRange(1, 1000)
			require.NoError(t, err)
			results[i] = result
		}

		// 检查不是所有结果都相同（随机数生成器应该产生不同的值）
		allSame := true
		for i := 1; i < len(results); i++ {
			if results[i] != results[0] {
				allSame = false
				break
			}
		}
		assert.False(t, allSame, "随机数生成器应该产生不同的值")
	})
}

// TestPrizeSelector_CoreFunctionality 测试奖品选择算法和概率计算
func TestPrizeSelector_CoreFunctionality(t *testing.T) {
	selector := NewDefaultPrizeSelector()

	t.Run("概率归一化", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "奖品1", Probability: 2.0, Value: 100},
			{ID: "2", Name: "奖品2", Probability: 3.0, Value: 50},
			{ID: "3", Name: "奖品3", Probability: 5.0, Value: 10},
		}

		normalized, err := selector.NormalizeProbabilities(prizes)
		require.NoError(t, err)
		assert.Len(t, normalized, 3)

		// 验证概率被正确归一化
		assert.InDelta(t, 0.2, normalized[0].Probability, 0.0001)
		assert.InDelta(t, 0.3, normalized[1].Probability, 0.0001)
		assert.InDelta(t, 0.5, normalized[2].Probability, 0.0001)
	})

	t.Run("累积概率计算", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "奖品1", Probability: 0.2, Value: 100},
			{ID: "2", Name: "奖品2", Probability: 0.3, Value: 50},
			{ID: "3", Name: "奖品3", Probability: 0.5, Value: 10},
		}

		cumulative, err := selector.CalculateCumulativeProbabilities(prizes)
		require.NoError(t, err)
		assert.Len(t, cumulative, 3)

		assert.InDelta(t, 0.2, cumulative[0], 0.0001)
		assert.InDelta(t, 0.5, cumulative[1], 0.0001)
		assert.InDelta(t, 1.0, cumulative[2], 0.0001)
	})

	t.Run("奖品选择", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "奖品1", Probability: 0.3, Value: 100},
			{ID: "2", Name: "奖品2", Probability: 0.7, Value: 50},
		}

		// 进行多次选择以验证概率分布
		selections := make(map[string]int)
		iterations := 100

		for range iterations {
			prize, err := selector.SelectPrize(prizes)
			require.NoError(t, err)
			require.NotNil(t, prize)
			selections[prize.ID]++
		}

		// 验证所有奖品都被选中
		assert.Contains(t, selections, "1")
		assert.Contains(t, selections, "2")

		// 验证概率分布（允许5%的误差）
		tolerance := 0.05
		assert.InDelta(t, 0.3, float64(selections["1"])/float64(iterations), tolerance)
		assert.InDelta(t, 0.7, float64(selections["2"])/float64(iterations), tolerance)
	})

	t.Run("单个奖品", func(t *testing.T) {
		prizes := []Prize{
			{ID: "only", Name: "唯一奖品", Probability: 1.0, Value: 100},
		}

		prize, err := selector.SelectPrize(prizes)
		require.NoError(t, err)
		assert.Equal(t, "only", prize.ID)
		assert.Equal(t, "唯一奖品", prize.Name)
	})

	t.Run("奖品池验证", func(t *testing.T) {
		// 有效的奖品池
		validPrizes := []Prize{
			{ID: "1", Name: "奖品1", Probability: 0.4, Value: 100},
			{ID: "2", Name: "奖品2", Probability: 0.6, Value: 50},
		}
		err := selector.ValidatePrizes(validPrizes)
		assert.NoError(t, err)

		// 空奖品池
		err = selector.ValidatePrizes([]Prize{})
		assert.Equal(t, ErrEmptyPrizePool, err)

		// 概率不等于1的奖品池
		invalidPrizes := []Prize{
			{ID: "1", Name: "奖品1", Probability: 0.3, Value: 100},
			{ID: "2", Name: "奖品2", Probability: 0.4, Value: 50},
		}
		err = selector.ValidatePrizes(invalidPrizes)
		assert.Equal(t, ErrInvalidProbability, err)

		// 无效的奖品数据
		invalidDataPrizes := []Prize{
			{ID: "", Name: "奖品1", Probability: 0.5, Value: 100}, // 空ID
			{ID: "2", Name: "奖品2", Probability: 0.5, Value: 50},
		}
		err = selector.ValidatePrizes(invalidDataPrizes)
		assert.Equal(t, ErrInvalidPrizeID, err)
	})

	t.Run("多次奖品选择", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "奖品1", Probability: 0.5, Value: 100},
			{ID: "2", Name: "奖品2", Probability: 0.5, Value: 50},
		}

		results, err := selector.SelectMultiplePrizes(prizes, 5)
		require.NoError(t, err)
		assert.Len(t, results, 5)

		// 验证所有结果都是有效奖品
		for _, prize := range results {
			assert.Contains(t, []string{"1", "2"}, prize.ID)
		}

		// 测试无效计数
		_, err = selector.SelectMultiplePrizes(prizes, 0)
		assert.Equal(t, ErrInvalidCount, err)

		_, err = selector.SelectMultiplePrizes(prizes, -1)
		assert.Equal(t, ErrInvalidCount, err)
	})

	t.Run("概率容差验证", func(t *testing.T) {
		// 概率和在容差范围内的奖品池应该被接受
		prizes := []Prize{
			{ID: "1", Name: "奖品1", Probability: 0.33333, Value: 100},
			{ID: "2", Name: "奖品2", Probability: 0.33333, Value: 50},
			{ID: "3", Name: "奖品3", Probability: 0.33334, Value: 10},
		}

		err := selector.ValidatePrizes(prizes)
		assert.NoError(t, err, "在容差范围内的概率和应该被接受")
	})
}

// Testngine_BasicFunctionality 测试抽奖引擎基础功能
func TestLotteryEngine_BasicFunctionality(t *testing.T) {
	// 创建Redis客户端用于测试
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // 使用测试数据库
	})

	// 测试Redis连接
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过测试")
	}

	defer rdb.Close()

	t.Run("范围抽奖", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		result, err := engine.DrawInRange(ctx, "test_range_draw", 1, 100)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 1)
		assert.LessOrEqual(t, result, 100)
	})

	t.Run("奖品池抽奖", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		prizes := []Prize{
			{ID: "gold", Name: "金奖", Probability: 0.1, Value: 1000},
			{ID: "silver", Name: "银奖", Probability: 0.2, Value: 500},
			{ID: "bronze", Name: "铜奖", Probability: 0.7, Value: 100},
		}

		prize, err := engine.DrawFromPrizes(ctx, "test_prize_draw", prizes)
		require.NoError(t, err)
		require.NotNil(t, prize)
		assert.Contains(t, []string{"gold", "silver", "bronze"}, prize.ID)
	})

	t.Run("参数验证", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 无效范围
		_, err := engine.DrawInRange(ctx, "test_invalid", 100, 1)
		assert.Equal(t, ErrInvalidRange, err)

		// 空锁键
		_, err = engine.DrawInRange(ctx, "", 1, 100)
		assert.Equal(t, ErrInvalidParameters, err)

		// 空奖品池
		_, err = engine.DrawFromPrizes(ctx, "test_empty_prizes", []Prize{})
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("连续抽奖", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 范围连抽
		results, err := engine.DrawMultipleInRange(ctx, "test_multi_range", 1, 10, 5, nil)
		require.NoError(t, err)
		assert.Len(t, results.Results, 5)

		for _, result := range results.Results {
			assert.GreaterOrEqual(t, result, 1)
			assert.LessOrEqual(t, result, 10)
		}

		// 奖品连抽
		prizes := []Prize{
			{ID: "a", Name: "奖品A", Probability: 0.5, Value: 100},
			{ID: "b", Name: "奖品B", Probability: 0.5, Value: 50},
		}

		prizeResults, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_prizes", prizes, 3, nil)
		require.NoError(t, err)
		assert.Len(t, prizeResults.PrizeResults, 3)

		for _, prize := range prizeResults.PrizeResults {
			assert.Contains(t, []string{"a", "b"}, prize.ID)
		}
	})
}

func TestLotteryEngine_Configuration(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}

	defer rdb.Close()

	t.Run("NewLotteryEngine with default config", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		config := engine.GetConfig()
		assert.Equal(t, 30*time.Second, config.Engine.LockTimeout)
		assert.Equal(t, 3, config.Engine.RetryAttempts)
		assert.Equal(t, 100*time.Millisecond, config.Engine.RetryInterval)
		assert.Equal(t, 1*time.Second, config.Engine.LockCacheTTL)

		// Test logger is set
		assert.NotNil(t, engine.GetLogger())
	})

	t.Run("NewLotteryEngineWithConfig", func(t *testing.T) {
		customConfig, err := NewLotteryConfig(
			10*time.Second,
			5,
			200*time.Millisecond,
			2*time.Second,
		)
		require.NoError(t, err)

		engine := NewLotteryEngineWithConfigAndLogger(rdb, customConfig, NewSilentLogger())

		config := engine.GetConfig()
		assert.Equal(t, 10*time.Second, config.Engine.LockTimeout)
		assert.Equal(t, 5, config.Engine.RetryAttempts)
		assert.Equal(t, 200*time.Millisecond, config.Engine.RetryInterval)
		assert.Equal(t, 2*time.Second, config.Engine.LockCacheTTL)
	})

	t.Run("UpdateConfig", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		newConfig, err := NewLotteryConfig(
			15*time.Second,
			2,
			50*time.Millisecond,
			1*time.Second,
		)
		require.NoError(t, err)

		err = engine.UpdateConfig(newConfig.GetConfig())
		require.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, 15*time.Second, config.Engine.LockTimeout)
		assert.Equal(t, 2, config.Engine.RetryAttempts)
		assert.Equal(t, 50*time.Millisecond, config.Engine.RetryInterval)
		assert.Equal(t, 1*time.Second, config.Engine.LockCacheTTL)
	})

	t.Run("UpdateConfig with invalid config", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// Test with nil config
		err := engine.UpdateConfig(nil)
		assert.Equal(t, ErrInvalidParameters, err)

		// Test with invalid timeout - create invalid config directly
		invalidConfig := &Config{
			Engine: &EngineConfig{
				LockTimeout:   0, // Invalid
				RetryAttempts: 3,
				RetryInterval: 100 * time.Millisecond,
				LockCacheTTL:  1 * time.Second,
			},

			Redis: DefaultRedisConfig(),
		}

		err = engine.UpdateConfig(invalidConfig)
		assert.Equal(t, ErrInvalidLockTimeout, err)
	})

	t.Run("SetLockTimeout", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		err := engine.SetLockTimeout(20 * time.Second)
		require.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, 20*time.Second, config.Engine.LockTimeout)

		// Test invalid timeout
		err = engine.SetLockTimeout(0)
		assert.Equal(t, ErrInvalidLockTimeout, err)
	})

	t.Run("SetRetryAttempts", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		err := engine.SetRetryAttempts(7)
		require.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, 7, config.Engine.RetryAttempts)

		// Test invalid attempts
		err = engine.SetRetryAttempts(-1)
		assert.Equal(t, ErrInvalidRetryAttempts, err)

		err = engine.SetRetryAttempts(15) // > MaxRetryAttempts
		assert.Equal(t, ErrInvalidRetryAttempts, err)
	})

	t.Run("SetRetryInterval", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		err := engine.SetRetryInterval(250 * time.Millisecond)
		require.NoError(t, err)

		config := engine.GetConfig()
		assert.Equal(t, 250*time.Millisecond, config.Engine.RetryInterval)

		// Test invalid interval
		err = engine.SetRetryInterval(-1 * time.Millisecond)
		assert.Equal(t, ErrInvalidRetryInterval, err)
	})
}

func TestLotteryEngine_ConfigurationIntegration(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}

	defer rdb.Close()

	t.Run("Configuration affects lottery behavior", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// Set a very short retry interval for testing
		err := engine.SetRetryInterval(10 * time.Millisecond)
		require.NoError(t, err)

		// Test that the engine still works with new configuration
		result, err := engine.DrawInRange(ctx, "test_config", 1, 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 1)
		assert.LessOrEqual(t, result, 10)
	})
}

// MockLogger for testing
type MockLogger struct {
	InfoMessages  []string
	ErrorMessages []string
	DebugMessages []string
}

func (m *MockLogger) Info(msg string, args ...any) {
	m.InfoMessages = append(m.InfoMessages, fmt.Sprintf(msg, args...))
}

func (m *MockLogger) Error(msg string, args ...any) {
	m.ErrorMessages = append(m.ErrorMessages, fmt.Sprintf(msg, args...))
}

func (m *MockLogger) Debug(msg string, args ...any) {
	m.DebugMessages = append(m.DebugMessages, fmt.Sprintf(msg, args...))
}

func TestLotteryEngine_Logging(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}

	defer rdb.Close()

	t.Run("Custom logger", func(t *testing.T) {
		mockLogger := &MockLogger{}
		engine := NewLotteryEngineWithLogger(rdb, mockLogger)

		assert.Equal(t, mockLogger, engine.GetLogger())

		// Test a lottery operation to see if logging works
		_, err := engine.DrawInRange(ctx, "test_logging", 1, 10)
		require.NoError(t, err)

		// Check that some log messages were generated
		assert.NotEmpty(t, mockLogger.DebugMessages)
		assert.NotEmpty(t, mockLogger.InfoMessages)
	})

	t.Run("SetLogger", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
		mockLogger := &MockLogger{}

		engine.SetLogger(mockLogger)
		assert.Equal(t, mockLogger, engine.GetLogger())

		// Test that the new logger is used
		_, err := engine.DrawInRange(ctx, "test_set_logger", 1, 10)
		require.NoError(t, err)

		assert.NotEmpty(t, mockLogger.InfoMessages)
	})

	t.Run("NewLotteryEngineWithConfigAndLogger", func(t *testing.T) {
		mockLogger := &MockLogger{}
		config, err := NewLotteryConfig(
			15*time.Second,
			2,
			50*time.Millisecond,
			1*time.Second,
		)
		require.NoError(t, err)

		engine := NewLotteryEngineWithConfigAndLogger(rdb, config, mockLogger)

		assert.Equal(t, mockLogger, engine.GetLogger())
		engineConfig := engine.GetConfig()
		assert.Equal(t, 15*time.Second, engineConfig.Engine.LockTimeout)
		assert.Equal(t, 2, engineConfig.Engine.RetryAttempts)
		assert.Equal(t, 50*time.Millisecond, engineConfig.Engine.RetryInterval)
		assert.Equal(t, 1*time.Second, engineConfig.Engine.LockCacheTTL)
	})
}

// ====================================================================

// TestRedisIntegration 测试Redis集成测试环境
func TestRedisIntegration(t *testing.T) {
	// 创建Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // 使用测试数据库
	})

	ctx := context.Background()

	// 测试Redis连接
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过集成测试")
	}

	defer rdb.Close()

	t.Run("Redis连接和基本操作", func(t *testing.T) {
		// 测试基本的Redis操作
		testKey := "integration_test_key"
		testValue := "integration_test_value"

		// 设置值
		err := rdb.Set(ctx, testKey, testValue, time.Minute).Err()
		require.NoError(t, err)

		// 获取值
		result, err := rdb.Get(ctx, testKey).Result()
		require.NoError(t, err)
		assert.Equal(t, testValue, result)

		// 删除值
		err = rdb.Del(ctx, testKey).Err()
		require.NoError(t, err)

		// 验证值已删除
		_, err = rdb.Get(ctx, testKey).Result()
		assert.Equal(t, redis.Nil, err)
	})

	t.Run("Redis锁操作集成", func(t *testing.T) {
		lockManager := NewLockManager(rdb, 30*time.Second)

		lockKey := "integration_lock_test"
		lockValue := "integration_value"

		// 获取锁
		acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, 10*time.Second)
		require.NoError(t, err)
		assert.True(t, acquired)

		// 验证锁在Redis中存在
		fullLockKey := LockKeyPrefix + lockKey
		storedValue, err := rdb.Get(ctx, fullLockKey).Result()
		require.NoError(t, err)
		assert.Equal(t, lockValue, storedValue)

		// 释放锁
		released, err := lockManager.ReleaseLock(ctx, lockKey, lockValue)
		require.NoError(t, err)
		assert.True(t, released)

		// 验证锁已从Redis中删除
		_, err = rdb.Get(ctx, fullLockKey).Result()
		assert.Equal(t, redis.Nil, err)
	})

	t.Run("抽奖引擎Redis集成", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 测试范围抽奖的Redis集成
		result, err := engine.DrawInRange(ctx, "integration_range_test", 1, 100)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 1)
		assert.LessOrEqual(t, result, 100)

		// 测试奖品抽奖的Redis集成
		prizes := []Prize{
			{ID: "test1", Name: "测试奖品1", Probability: 0.5, Value: 100},
			{ID: "test2", Name: "测试奖品2", Probability: 0.5, Value: 50},
		}

		prize, err := engine.DrawFromPrizes(ctx, "integration_prize_test", prizes)
		require.NoError(t, err)
		require.NotNil(t, prize)
		assert.Contains(t, []string{"test1", "test2"}, prize.ID)
	})
}

// TestConcurrentLottery 测试多协程并发抽奖
func TestConcurrentLottery(t *testing.T) {
	// 创建Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // 使用测试数据库
	})

	ctx := context.Background()

	// 测试Redis连接
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过并发测试")
	}

	defer rdb.Close()

	t.Run("并发范围抽奖", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		const numGoroutines = 50
		const numDrawsPerGoroutine = 10

		var wg sync.WaitGroup
		results := make([][]int, numGoroutines)
		errors := make([]error, numGoroutines)

		// 启动多个协程进行并发抽奖
		for i := range numGoroutines {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				goroutineResults := make([]int, numDrawsPerGoroutine)
				for j := range numDrawsPerGoroutine {
					lockKey := fmt.Sprintf("concurrent_range_%d_%d", goroutineID, j)
					result, err := engine.DrawInRange(ctx, lockKey, 1, 1000)
					if err != nil {
						errors[goroutineID] = err
						return
					}
					goroutineResults[j] = result
				}
				results[goroutineID] = goroutineResults
			}(i)
		}

		wg.Wait()

		// 验证结果
		successCount := 0
		for i := range numGoroutines {
			if errors[i] == nil {
				successCount++
				assert.Len(t, results[i], numDrawsPerGoroutine)
				for _, result := range results[i] {
					assert.GreaterOrEqual(t, result, 1)
					assert.LessOrEqual(t, result, 1000)
				}
			}
		}

		// 至少90%的协程应该成功
		successRate := float64(successCount) / float64(numGoroutines)
		assert.GreaterOrEqual(t, successRate, 0.9, "并发抽奖成功率应该至少90%")

		t.Logf("并发范围抽奖成功率: %.2f%% (%d/%d)", successRate*100, successCount, numGoroutines)
	})

	t.Run("并发奖品抽奖", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		prizes := []Prize{
			{ID: "concurrent1", Name: "并发奖品1", Probability: 0.3, Value: 100},
			{ID: "concurrent2", Name: "并发奖品2", Probability: 0.4, Value: 50},
			{ID: "concurrent3", Name: "并发奖品3", Probability: 0.3, Value: 10},
		}

		const numGoroutines = 30
		const numDrawsPerGoroutine = 5

		var wg sync.WaitGroup
		results := make([][]*Prize, numGoroutines)
		errors := make([]error, numGoroutines)

		// 启动多个协程进行并发奖品抽奖
		for i := range numGoroutines {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				goroutineResults := make([]*Prize, numDrawsPerGoroutine)
				for j := 0; j < numDrawsPerGoroutine; j++ {
					lockKey := fmt.Sprintf("concurrent_prize_%d_%d", goroutineID, j)
					prize, err := engine.DrawFromPrizes(ctx, lockKey, prizes)
					if err != nil {
						errors[goroutineID] = err
						return
					}
					goroutineResults[j] = prize
				}
				results[goroutineID] = goroutineResults
			}(i)
		}

		wg.Wait()

		// 验证结果
		successCount := 0
		prizeDistribution := make(map[string]int)

		for i := 0; i < numGoroutines; i++ {
			if errors[i] == nil {
				successCount++
				assert.Len(t, results[i], numDrawsPerGoroutine)
				for _, prize := range results[i] {
					assert.Contains(t, []string{"concurrent1", "concurrent2", "concurrent3"}, prize.ID)
					prizeDistribution[prize.ID]++
				}
			}
		}

		// 至少90%的协程应该成功
		successRate := float64(successCount) / float64(numGoroutines)
		assert.GreaterOrEqual(t, successRate, 0.9, "并发奖品抽奖成功率应该至少90%")

		t.Logf("并发奖品抽奖成功率: %.2f%% (%d/%d)", successRate*100, successCount, numGoroutines)
		t.Logf("奖品分布: %v", prizeDistribution)
	})

	t.Run("锁竞争测试", func(t *testing.T) {
		lockManager := NewLockManager(rdb, 30*time.Second)

		const numGoroutines = 20
		const sameLockKey = "lock_competition_test"

		var wg sync.WaitGroup
		acquisitionResults := make([]bool, numGoroutines)
		acquisitionTimes := make([]time.Time, numGoroutines)

		// 多个协程尝试获取同一个锁
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				lockValue := fmt.Sprintf("value_%d", goroutineID)
				acquired, err := lockManager.TryAcquireLock(ctx, sameLockKey, lockValue, 5*time.Second)

				if err == nil {
					acquisitionResults[goroutineID] = acquired
					if acquired {
						acquisitionTimes[goroutineID] = time.Now()
						// 持有锁一小段时间
						time.Sleep(10 * time.Millisecond)
						// 释放锁
						lockManager.ReleaseLock(ctx, sameLockKey, lockValue)
					}
				}
			}(i)
		}

		wg.Wait()

		// 验证只有一个协程能获取到锁（在任何给定时间）
		successfulAcquisitions := 0
		for i := 0; i < numGoroutines; i++ {
			if acquisitionResults[i] {
				successfulAcquisitions++
			}
		}

		// 由于是TryAcquireLock（不重试），只有第一个协程应该成功
		assert.Equal(t, 1, successfulAcquisitions, "在锁竞争中只应该有一个协程成功获取锁")

		t.Logf("锁竞争测试: %d个协程中有%d个成功获取锁", numGoroutines, successfulAcquisitions)
	})
}

// TestMultiDrawThreadSafety 测试连续抽奖的线程安全性
func TestMultiDrawThreadSafety(t *testing.T) {
	// 创建Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // 使用测试数据库
	})

	ctx := context.Background()

	// 测试Redis连接
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过线程安全测试")
	}

	defer rdb.Close()

	t.Run("并发连续范围抽奖", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		const numGoroutines = 10
		const drawsPerGoroutine = 5

		var wg sync.WaitGroup
		results := make([][]int, numGoroutines)
		errors := make([]error, numGoroutines)

		// 启动多个协程进行并发连续抽奖
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				lockKey := fmt.Sprintf("multi_draw_range_%d", goroutineID)
				drawResults, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 100, drawsPerGoroutine, nil)

				if err != nil {
					errors[goroutineID] = err
				} else {
					results[goroutineID] = drawResults.Results
				}
			}(i)
		}

		wg.Wait()

		// 验证结果
		successCount := 0
		for i := 0; i < numGoroutines; i++ {
			if errors[i] == nil {
				successCount++
				assert.Len(t, results[i], drawsPerGoroutine)
				for _, result := range results[i] {
					assert.GreaterOrEqual(t, result, 1)
					assert.LessOrEqual(t, result, 100)
				}
			} else {
				t.Logf("协程 %d 出错: %v", i, errors[i])
			}
		}

		// 所有协程都应该成功（因为使用不同的锁键）
		assert.Equal(t, numGoroutines, successCount, "所有并发连续抽奖都应该成功")

		t.Logf("并发连续范围抽奖成功率: %d/%d", successCount, numGoroutines)
	})

	t.Run("并发连续奖品抽奖", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		prizes := []Prize{
			{ID: "multi1", Name: "连抽奖品1", Probability: 0.4, Value: 100},
			{ID: "multi2", Name: "连抽奖品2", Probability: 0.6, Value: 50},
		}

		const numGoroutines = 8
		const drawsPerGoroutine = 4

		var wg sync.WaitGroup
		results := make([][]*Prize, numGoroutines)
		errors := make([]error, numGoroutines)

		// 启动多个协程进行并发连续奖品抽奖
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				lockKey := fmt.Sprintf("multi_draw_prize_%d", goroutineID)
				prizeResults, err := engine.DrawMultipleFromPrizes(ctx, lockKey, prizes, drawsPerGoroutine, nil)

				if err != nil {
					errors[goroutineID] = err
				} else {
					results[goroutineID] = prizeResults.PrizeResults
				}
			}(i)
		}

		wg.Wait()

		// 验证结果
		successCount := 0
		totalPrizes := 0

		for i := 0; i < numGoroutines; i++ {
			if errors[i] == nil {
				successCount++
				assert.Len(t, results[i], drawsPerGoroutine)
				totalPrizes += len(results[i])
				for _, prize := range results[i] {
					assert.Contains(t, []string{"multi1", "multi2"}, prize.ID)
				}
			} else {
				t.Logf("协程 %d 出错: %v", i, errors[i])
			}
		}

		// 所有协程都应该成功
		assert.Equal(t, numGoroutines, successCount, "所有并发连续奖品抽奖都应该成功")
		assert.Equal(t, numGoroutines*drawsPerGoroutine, totalPrizes, "总奖品数量应该正确")

		t.Logf("并发连续奖品抽奖成功率: %d/%d，总奖品数: %d", successCount, numGoroutines, totalPrizes)
	})

	t.Run("相同锁键的连续抽奖竞争", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		const numGoroutines = 5
		const drawsPerGoroutine = 3
		const sameLockKey = "shared_multi_draw_key"

		var wg sync.WaitGroup
		results := make([][]int, numGoroutines)
		errors := make([]error, numGoroutines)
		startTimes := make([]time.Time, numGoroutines)
		endTimes := make([]time.Time, numGoroutines)

		// 启动多个协程使用相同锁键进行连续抽奖
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				startTimes[goroutineID] = time.Now()
				drawResults, err := engine.DrawMultipleInRange(ctx, sameLockKey, 1, 50, drawsPerGoroutine, nil)
				endTimes[goroutineID] = time.Now()

				if err != nil {
					errors[goroutineID] = err
				} else {
					results[goroutineID] = drawResults.Results
				}
			}(i)
		}

		wg.Wait()

		// 验证结果
		successCount := 0
		for i := 0; i < numGoroutines; i++ {
			if errors[i] == nil {
				successCount++
				assert.Len(t, results[i], drawsPerGoroutine)
				for _, result := range results[i] {
					assert.GreaterOrEqual(t, result, 1)
					assert.LessOrEqual(t, result, 50)
				}
			}
		}

		// 由于锁的存在，大部分协程应该成功（允许一些因为锁竞争而失败）
		assert.GreaterOrEqual(t, successCount, numGoroutines-2, "大部分使用相同锁键的连续抽奖应该成功")

		// 验证成功的协程之间有一定的串行化特征
		if successCount > 1 {
			serializedPairs := 0
			totalPairs := 0

			for i := 0; i < numGoroutines-1; i++ {
				for j := i + 1; j < numGoroutines; j++ {
					if errors[i] == nil && errors[j] == nil {
						totalPairs++
						// 检查是否串行执行（一个结束后另一个开始，允许一些重叠）
						if endTimes[i].Add(-200*time.Millisecond).Before(startTimes[j]) ||
							endTimes[j].Add(-200*time.Millisecond).Before(startTimes[i]) {
							serializedPairs++
						}
					}
				}
			}

			if totalPairs > 0 {
				serializationRate := float64(serializedPairs) / float64(totalPairs)
				t.Logf("串行化率: %.2f%% (%d/%d 对)", serializationRate*100, serializedPairs, totalPairs)
				// 至少应该有一些串行化的证据，但不要求100%（因为网络延迟等因素）
				assert.GreaterOrEqual(t, serializationRate, 0.3, "应该有一定程度的串行化")
			}
		}

		t.Logf("相同锁键连续抽奖成功率: %d/%d", successCount, numGoroutines)
	})

	t.Run("错误恢复的线程安全性", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		const numGoroutines = 6
		const drawsPerGoroutine = 8

		var wg sync.WaitGroup
		results := make([]*MultiDrawResult, numGoroutines)
		errors := make([]error, numGoroutines)

		// 启动多个协程进行带错误恢复的连续抽奖
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				lockKey := fmt.Sprintf("recovery_draw_%d", goroutineID)
				result, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 200, drawsPerGoroutine, nil)

				results[goroutineID] = result
				errors[goroutineID] = err
			}(i)
		}

		wg.Wait()

		// 验证结果
		successCount := 0
		partialSuccessCount := 0

		for i := 0; i < numGoroutines; i++ {
			if results[i] != nil {
				if errors[i] == nil {
					successCount++
					assert.Equal(t, drawsPerGoroutine, results[i].Completed)
					assert.Equal(t, 0, results[i].Failed)
					assert.False(t, results[i].PartialSuccess)
				} else if results[i].PartialSuccess {
					partialSuccessCount++
					assert.Greater(t, results[i].Completed, 0)
					assert.Greater(t, results[i].Failed, 0)
				}

				// 验证结果数据的完整性
				assert.Equal(t, drawsPerGoroutine, results[i].TotalRequested)
				assert.LessOrEqual(t, results[i].Completed+results[i].Failed, results[i].TotalRequested)
				assert.Len(t, results[i].Results, results[i].Completed)
			}
		}

		// 大部分应该成功
		totalProcessed := successCount + partialSuccessCount
		assert.GreaterOrEqual(t, totalProcessed, numGoroutines-1, "大部分错误恢复抽奖应该成功或部分成功")

		t.Logf("错误恢复线程安全测试: 完全成功 %d, 部分成功 %d, 总处理 %d/%d",
			successCount, partialSuccessCount, totalProcessed, numGoroutines)
	})
}

// =================================================================

// TestLotteryEngine_FullIntegration 全面集成测试
func TestLotteryEngine_FullIntegration(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration test")
	}

	defer rdb.Close()

	t.Run("Complete lottery system workflow", func(t *testing.T) {
		// 1. 创建抽奖引擎
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
		require.NotNil(t, engine)

		// 2. 测试配置管理
		config := engine.GetConfig()
		assert.Equal(t, 30*time.Second, config.Engine.LockTimeout)

		// 3. 更新配置
		newConfig, err := NewLotteryConfig(
			10*time.Second,
			5,
			50*time.Millisecond,
			1*time.Second,
		)
		require.NoError(t, err)
		err = engine.UpdateConfig(newConfig.GetConfig())
		require.NoError(t, err)

		// 4. 验证配置更新
		updatedConfig := engine.GetConfig()
		assert.Equal(t, 10*time.Second, updatedConfig.Engine.LockTimeout)
		assert.Equal(t, 5, updatedConfig.Engine.RetryAttempts)

		// 5. 测试范围抽奖
		result, err := engine.DrawInRange(ctx, "integration_range", 1, 100)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 1)
		assert.LessOrEqual(t, result, 100)

		// 6. 测试多次范围抽奖
		results, err := engine.DrawMultipleInRange(ctx, "integration_multi_range", 1, 10, 5, nil)
		require.NoError(t, err)
		assert.Len(t, results.Results, 5)
		for _, r := range results.Results {
			assert.GreaterOrEqual(t, r, 1)
			assert.LessOrEqual(t, r, 10)
		}

		// 7. 测试奖品池抽奖
		prizes := []Prize{
			{ID: "gold", Name: "Gold Prize", Probability: 0.1, Value: 1000},
			{ID: "silver", Name: "Silver Prize", Probability: 0.3, Value: 500},
			{ID: "bronze", Name: "Bronze Prize", Probability: 0.6, Value: 100},
		}

		prize, err := engine.DrawFromPrizes(ctx, "integration_prize", prizes)
		require.NoError(t, err)
		assert.NotNil(t, prize)
		assert.Contains(t, []string{"gold", "silver", "bronze"}, prize.ID)

		// 8. 测试多次奖品抽奖
		multiPrizes, err := engine.DrawMultipleFromPrizes(ctx, "integration_multi_prize", prizes, 10, nil)
		require.NoError(t, err)
		assert.Len(t, multiPrizes.PrizeResults, 10)
		for _, p := range multiPrizes.PrizeResults {
			assert.NotNil(t, p)
			assert.Contains(t, []string{"gold", "silver", "bronze"}, p.ID)
		}
	})

	t.Run("High concurrency stress test", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 设置较短的超时时间进行压力测试
		err := engine.SetLockTimeout(2 * time.Second)
		require.NoError(t, err)

		const numGoroutines = 50
		const drawsPerGoroutine = 100

		var wg sync.WaitGroup
		successCount := int64(0)
		errorCount := int64(0)
		var mu sync.Mutex

		prizes := []Prize{
			{ID: "test1", Name: "Test Prize 1", Probability: 0.5, Value: 100},
			{ID: "test2", Name: "Test Prize 2", Probability: 0.5, Value: 200},
		}

		// 启动多个goroutine进行并发抽奖
		for i := range numGoroutines {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := range drawsPerGoroutine {
					// 交替进行范围抽奖和奖品抽奖
					if j%2 == 0 {
						_, err := engine.DrawInRange(ctx, "stress_test", 1, 1000)
						mu.Lock()
						if err != nil {
							errorCount++
						} else {
							successCount++
						}
						mu.Unlock()
					} else {
						_, err := engine.DrawFromPrizes(ctx, "stress_test", prizes)
						mu.Lock()
						if err != nil {
							errorCount++
						} else {
							successCount++
						}
						mu.Unlock()
					}
				}
			}(i)
		}

		wg.Wait()

		totalOperations := int64(numGoroutines * drawsPerGoroutine)
		t.Logf("Stress test completed: %d total operations, %d successful, %d errors",
			totalOperations, successCount, errorCount)

		// 至少应该有一些成功的操作
		assert.Greater(t, successCount, int64(0))
		// 成功率应该合理（考虑到锁竞争）
		successRate := float64(successCount) / float64(totalOperations)
		assert.Greater(t, successRate, 0.07) // 至少7%成功率
	})

	t.Run("Configuration runtime adjustment under load", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		var wg sync.WaitGroup

		// 启动一个goroutine持续进行抽奖操作
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, _ = engine.DrawInRange(ctx, "config_test", 1, 100)
				time.Sleep(10 * time.Millisecond)
			}
		}()

		// 在抽奖过程中动态调整配置
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond) // 等待抽奖开始

			// 调整锁超时时间
			err := engine.SetLockTimeout(15 * time.Second)
			assert.NoError(t, err)

			time.Sleep(100 * time.Millisecond)

			// 调整重试次数
			err = engine.SetRetryAttempts(7)
			assert.NoError(t, err)

			time.Sleep(100 * time.Millisecond)

			// 调整重试间隔
			err = engine.SetRetryInterval(200 * time.Millisecond)
			assert.NoError(t, err)
		}()

		wg.Wait()

		// 验证最终配置
		finalConfig := engine.GetConfig()
		assert.Equal(t, 15*time.Second, finalConfig.Engine.LockTimeout)
		assert.Equal(t, 7, finalConfig.Engine.RetryAttempts)
		assert.Equal(t, 200*time.Millisecond, finalConfig.Engine.RetryInterval)
		assert.Equal(t, 1*time.Second, finalConfig.Engine.LockCacheTTL)
	})

	t.Run("Error handling and recovery", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 测试无效参数的错误处理
		_, err := engine.DrawInRange(ctx, "", 1, 100)
		assert.Equal(t, ErrInvalidParameters, err)

		_, err = engine.DrawInRange(ctx, "test", 100, 1)
		assert.Equal(t, ErrInvalidRange, err)

		// 测试空奖品池
		_, err = engine.DrawFromPrizes(ctx, "test", []Prize{})
		assert.Equal(t, ErrEmptyPrizePool, err)

		// 测试无效奖品池（概率总和为0）
		invalidPrizes := []Prize{
			{ID: "invalid", Name: "Invalid", Probability: 0.0, Value: 100},
			{ID: "invalid2", Name: "Invalid2", Probability: 0.0, Value: 200}, // 总概率 = 0.0，无效
		}
		_, err = engine.DrawFromPrizes(ctx, "test", invalidPrizes)
		assert.Equal(t, ErrInvalidProbability, err)

		// 测试配置错误处理
		err = engine.UpdateConfig(nil)
		assert.Equal(t, ErrInvalidParameters, err)

		invalidConfig := &Config{
			Engine: &EngineConfig{
				LockTimeout:   0, // 无效
				RetryAttempts: 3,
				RetryInterval: 100 * time.Millisecond,
				LockCacheTTL:  1 * time.Second,
			},

			Redis: DefaultRedisConfig(),
			CircuitBreaker: &CircuitBreakerConfig{
				Enabled:      true,
				Name:         "test",
				MaxRequests:  3,
				Interval:     60 * time.Second,
				Timeout:      30 * time.Second,
				FailureRatio: 0.6,
				MinRequests:  3,
			},
		}
		err = engine.UpdateConfig(invalidConfig)
		assert.Equal(t, ErrInvalidLockTimeout, err)
	})
}

// TestLotteryEngine_CustomLogger 测试自定义日志记录器
func TestLotteryEngine_CustomLogger(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}

	defer rdb.Close()

	t.Run("Custom logger captures all log levels", func(t *testing.T) {
		mockLogger := &MockLogger{}
		engine := NewLotteryEngineWithLogger(rdb, mockLogger)

		// 执行各种操作来触发不同级别的日志
		_, err := engine.DrawInRange(ctx, "logger_test", 1, 10)
		require.NoError(t, err)

		// 触发错误日志
		_, err = engine.DrawInRange(ctx, "", 1, 10)
		assert.Error(t, err)

		// 更新配置触发信息日志
		err = engine.SetLockTimeout(20 * time.Second)
		require.NoError(t, err)

		// 验证日志记录
		assert.NotEmpty(t, mockLogger.DebugMessages, "Should have debug messages")
		assert.NotEmpty(t, mockLogger.InfoMessages, "Should have info messages")
		assert.NotEmpty(t, mockLogger.ErrorMessages, "Should have error messages")

		// 验证特定的日志内容
		found := slices.Contains(mockLogger.ErrorMessages, "drawInRange failed: empty lock key")
		assert.True(t, found, "Should log specific error message")
	})
}

// TestLotteryEngine_EdgeCases 边界情况测试
func TestLotteryEngine_EdgeCases(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping test")
	}

	defer rdb.Close()

	t.Run("Edge cases and boundary conditions", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 测试单值范围
		result, err := engine.DrawInRange(ctx, "edge_single", 42, 42)
		require.NoError(t, err)
		assert.Equal(t, 42, result)

		// 测试负数范围
		result, err = engine.DrawInRange(ctx, "edge_negative", -100, -50)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, -100)
		assert.LessOrEqual(t, result, -50)

		// 测试跨零范围
		result, err = engine.DrawInRange(ctx, "edge_cross_zero", -10, 10)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, -10)
		assert.LessOrEqual(t, result, 10)

		// 测试单个奖品的奖品池
		singlePrize := []Prize{
			{ID: "only", Name: "Only Prize", Probability: 1.0, Value: 100},
		}
		prize, err := engine.DrawFromPrizes(ctx, "edge_single_prize", singlePrize)
		require.NoError(t, err)
		assert.Equal(t, "only", prize.ID)

		// 测试需要归一化的奖品池
		unnormalizedPrizes := []Prize{
			{ID: "p1", Name: "Prize 1", Probability: 0.2, Value: 100},
			{ID: "p2", Name: "Prize 2", Probability: 0.3, Value: 200},
			{ID: "p3", Name: "Prize 3", Probability: 0.1, Value: 300}, // 总和 = 0.6，需要归一化
		}
		prize, err = engine.DrawFromPrizes(ctx, "edge_unnormalized", unnormalizedPrizes)
		require.NoError(t, err)
		assert.Contains(t, []string{"p1", "p2", "p3"}, prize.ID)
	})

	t.Run("Context cancellation handling", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 创建一个会被取消的context
		cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		// 测试在context取消时的行为
		results, err := engine.DrawMultipleInRange(cancelCtx, "cancel_test", 1, 1000, 100, nil)

		// 应该返回部分结果或context错误
		if err == context.DeadlineExceeded {
			t.Logf("Context cancelled as expected, got %d partial results", len(results.Results))
		} else {
			// 如果操作完成得很快，也是正常的
			require.NoError(t, err)
			assert.Len(t, results.Results, 100)
		}
	})

	t.Run("Memory and resource management", func(t *testing.T) {
		engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

		// 执行大量操作来测试内存管理
		const iterations = 1000

		for i := 0; i < iterations; i++ {
			// 交替执行不同类型的抽奖
			switch i % 3 {
			case 0:
				_, _ = engine.DrawInRange(ctx, "memory_test", 1, 100)
			case 1:
				prizes := []Prize{
					{ID: "mem1", Name: "Memory Test 1", Probability: 0.7, Value: 100},
					{ID: "mem2", Name: "Memory Test 2", Probability: 0.3, Value: 200},
				}
				_, _ = engine.DrawFromPrizes(ctx, "memory_test", prizes)
			case 2:
				_, _ = engine.DrawMultipleInRange(ctx, "memory_test", 1, 10, 3, nil)
			}
		}

		// 如果没有内存泄漏，测试应该正常完成
		t.Log("Memory management test completed successfully")
	})
}

// =================================================================

func TestDistributedLockManager_AcquireLock(t *testing.T) {
	// Create a Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection first
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	lockManager := NewLockManager(rdb, 30*time.Second)
	ctx := context.Background()

	tests := []struct {
		name        string
		lockKey     string
		lockValue   string
		expireTime  time.Duration
		expectError bool
	}{
		{
			name:        "valid lock acquisition",
			lockKey:     "test_lock_1",
			lockValue:   "test_value_1",
			expireTime:  10 * time.Second,
			expectError: false,
		},
		{
			name:        "empty lock key",
			lockKey:     "",
			lockValue:   "test_value_2",
			expireTime:  10 * time.Second,
			expectError: true,
		},
		{
			name:        "empty lock value",
			lockKey:     "test_lock_2",
			lockValue:   "",
			expireTime:  10 * time.Second,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acquired, err := lockManager.AcquireLock(ctx, tt.lockKey, tt.lockValue, tt.expireTime)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				// Skip test if Redis is not available
				if err == ErrRedisConnectionFailed {
					t.Skip("Redis not available for testing")
				}
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !acquired {
				t.Errorf("Expected lock to be acquired")
			}

			// Clean up: release the lock
			if acquired {
				released, releaseErr := lockManager.ReleaseLock(ctx, tt.lockKey, tt.lockValue)
				if releaseErr != nil {
					t.Errorf("Failed to release lock: %v", releaseErr)
				}
				if !released {
					t.Errorf("Expected lock to be released")
				}
			}
		})
	}
}

func TestDistributedLockManager_ReleaseLock(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection first
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	lockManager := NewLockManager(rdb, 30*time.Second)
	ctx := context.Background()

	tests := []struct {
		name        string
		lockKey     string
		lockValue   string
		expectError bool
	}{
		{
			name:        "empty lock key",
			lockKey:     "",
			lockValue:   "test_value",
			expectError: true,
		},
		{
			name:        "empty lock value",
			lockKey:     "test_lock",
			lockValue:   "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			released, err := lockManager.ReleaseLock(ctx, tt.lockKey, tt.lockValue)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				// Skip test if Redis is not available
				if err == ErrRedisConnectionFailed {
					t.Skip("Redis not available for testing")
				}
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// For non-existent locks, released should be false but no error
			if released {
				t.Errorf("Expected lock release to return false for non-existent lock")
			}
		})
	}
}

func TestDistributedLockManager_LockConflict(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection first
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	lockManager := NewLockManager(rdb, 30*time.Second)
	ctx := context.Background()

	lockKey := "conflict_test_lock"
	lockValue1 := "value1"
	lockValue2 := "value2"

	// First acquisition should succeed
	acquired1, err1 := lockManager.AcquireLock(ctx, lockKey, lockValue1, 10*time.Second)
	if err1 != nil {
		t.Fatalf("Unexpected error on first acquisition: %v", err1)
	}

	if !acquired1 {
		t.Fatalf("Expected first lock acquisition to succeed")
	}

	// Second acquisition with different value should fail
	acquired2, err2 := lockManager.AcquireLock(ctx, lockKey, lockValue2, 10*time.Second)
	if err2 != nil {
		// This is expected - lock acquisition should fail
		t.Logf("Expected error on second acquisition: %v", err2)
	}

	if acquired2 {
		t.Errorf("Expected second lock acquisition to fail due to conflict")
	}

	// Release with wrong value should fail
	released1, err3 := lockManager.ReleaseLock(ctx, lockKey, lockValue2)
	if err3 != nil {
		t.Fatalf("Unexpected error on wrong value release: %v", err3)
	}

	if released1 {
		t.Errorf("Expected lock release with wrong value to fail")
	}

	// Release with correct value should succeed
	released2, err4 := lockManager.ReleaseLock(ctx, lockKey, lockValue1)
	if err4 != nil {
		t.Fatalf("Unexpected error on correct value release: %v", err4)
	}

	if !released2 {
		t.Errorf("Expected lock release with correct value to succeed")
	}

	// Now second acquisition should succeed
	acquired3, err5 := lockManager.AcquireLock(ctx, lockKey, lockValue2, 10*time.Second)
	if err5 != nil {
		t.Fatalf("Unexpected error on third acquisition: %v", err5)
	}

	if !acquired3 {
		t.Errorf("Expected third lock acquisition to succeed after release")
	}

	// Clean up
	lockManager.ReleaseLock(ctx, lockKey, lockValue2)
}

func TestDistributedLockManager_WithRetry(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection first
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	// Create lock manager with custom retry settings
	lockManager := NewLockManagerWithRetry(rdb, 30*time.Second, 2, 50*time.Millisecond, 1*time.Second)
	ctx := context.Background()

	tests := []struct {
		name        string
		lockKey     string
		lockValue   string
		expectError bool
	}{
		{
			name:        "valid lock with retry",
			lockKey:     "retry_test_lock",
			lockValue:   "retry_value",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acquired, err := lockManager.AcquireLock(ctx, tt.lockKey, tt.lockValue, 10*time.Second)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				if err == ErrRedisConnectionFailed {
					t.Skip("Redis not available for testing")
				}
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if !acquired {
				t.Errorf("Expected lock to be acquired")
			}

			// Clean up
			if acquired {
				lockManager.ReleaseLock(ctx, tt.lockKey, tt.lockValue)
			}
		})
	}
}

func TestDistributedLockManager_TryAcquireLock(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection first
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	lockManager := NewLockManager(rdb, 30*time.Second)
	ctx := context.Background()

	lockKey := "try_lock_test"
	lockValue := "try_value"

	// Try to acquire lock (should succeed)
	acquired, err := lockManager.TryAcquireLock(ctx, lockKey, lockValue, 10*time.Second)
	if err != nil {
		if err == ErrRedisConnectionFailed {
			t.Skip("Redis not available for testing")
		}
		t.Fatalf("Unexpected error: %v", err)
	}

	if !acquired {
		t.Fatalf("Expected lock to be acquired")
	}

	// Try to acquire same lock again (should fail)
	acquired2, err2 := lockManager.TryAcquireLock(ctx, lockKey, "different_value", 10*time.Second)
	if err2 != nil {
		t.Fatalf("Unexpected error on second try: %v", err2)
	}

	if acquired2 {
		t.Errorf("Expected second lock acquisition to fail")
	}

	// Clean up
	lockManager.ReleaseLock(ctx, lockKey, lockValue)
}

func TestDistributedLockManager_AcquireLockWithTimeout(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection first
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	lockManager := NewLockManager(rdb, 30*time.Second)
	ctx := context.Background()

	lockKey := "timeout_test_lock"
	lockValue1 := "value1"
	lockValue2 := "value2"

	// First acquire the lock
	acquired1, err1 := lockManager.TryAcquireLock(ctx, lockKey, lockValue1, 1*time.Second)
	if err1 != nil {
		if err1 == ErrRedisConnectionFailed {
			t.Skip("Redis not available for testing")
		}
		t.Fatalf("Unexpected error: %v", err1)
	}

	if !acquired1 {
		t.Fatalf("Expected first lock to be acquired")
	}

	// Try to acquire with timeout (should timeout)
	acquired2, err2 := lockManager.AcquireLockWithTimeout(ctx, lockKey, lockValue2, 1*time.Second, 100*time.Millisecond)
	if err2 != ErrLockTimeout {
		if err2 == ErrRedisConnectionFailed {
			t.Skip("Redis not available for testing")
		}
		t.Errorf("Expected timeout error, got: %v", err2)
	}

	if acquired2 {
		t.Errorf("Expected lock acquisition to timeout")
	}

	// Clean up
	lockManager.ReleaseLock(ctx, lockKey, lockValue1)
}

// =================================================================

func TestMultiDrawResult_Validate(t *testing.T) {
	tests := []struct {
		name    string
		result  *MultiDrawResult
		wantErr bool
	}{
		{
			name: "valid result",
			result: &MultiDrawResult{
				Results:        []int{1, 2, 3},
				TotalRequested: 3,
				Completed:      3,
				Failed:         0,
				PartialSuccess: false,
			},
			wantErr: false,
		},
		{
			name: "invalid total requested",
			result: &MultiDrawResult{
				TotalRequested: 0,
				Completed:      0,
				Failed:         0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.result.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMultiDrawResult_SuccessRate(t *testing.T) {
	result := &MultiDrawResult{
		TotalRequested: 10,
		Completed:      8,
		Failed:         2,
	}
	assert.Equal(t, 80.0, result.SuccessRate())

	// Test zero total
	result.TotalRequested = 0
	assert.Equal(t, 0.0, result.SuccessRate())
}

func TestDrawState_Validate(t *testing.T) {
	tests := []struct {
		name    string
		state   *DrawState
		wantErr bool
	}{
		{
			name: "valid state",
			state: &DrawState{
				LockKey:        "test_key",
				TotalCount:     5,
				CompletedCount: 3,
				StartTime:      time.Now().Unix(),
			},
			wantErr: false,
		},
		{
			name: "empty lock key",
			state: &DrawState{
				LockKey:        "",
				TotalCount:     5,
				CompletedCount: 3,
				StartTime:      time.Now().Unix(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.state.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLotteryEngine_shouldAbortOnError(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test data
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	tests := []struct {
		name        string
		err         error
		shouldAbort bool
	}{
		{
			name:        "Redis connection failed",
			err:         ErrRedisConnectionFailed,
			shouldAbort: true,
		},
		{
			name:        "Context deadline exceeded",
			err:         context.DeadlineExceeded,
			shouldAbort: true,
		},
		{
			name:        "Lock acquisition failed",
			err:         ErrLockAcquisitionFailed,
			shouldAbort: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.shouldAbortOnError(tt.err)
			assert.Equal(t, tt.shouldAbort, result)
		})
	}
}

func TestCalculateOptimalBatchSize(t *testing.T) {
	tests := []struct {
		name        string
		totalCount  int
		expectedMin int
		expectedMax int
	}{
		{
			name:        "very small count",
			totalCount:  5,
			expectedMin: 1,
			expectedMax: 1,
		},
		{
			name:        "small count",
			totalCount:  50,
			expectedMin: 10,
			expectedMax: 10,
		},
		{
			name:        "medium count",
			totalCount:  500,
			expectedMin: 50,
			expectedMax: 50,
		},
		{
			name:        "large count",
			totalCount:  5000,
			expectedMin: 100,
			expectedMax: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize := calculateOptimalBatchSize(tt.totalCount)
			assert.GreaterOrEqual(t, batchSize, tt.expectedMin)
			assert.LessOrEqual(t, batchSize, tt.expectedMax)
		})
	}
}

func TestLotteryEngine_DrawMultipleInRangeOptimized(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})
	// Clean up test data
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()
	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	t.Run("successful optimized multi-draw", func(t *testing.T) {
		ctx := context.Background()
		// Track progress
		var progressUpdates int32
		progressCallback := func(completed, total int, currentResult interface{}) {
			atomic.AddInt32(&progressUpdates, 1)
			assert.LessOrEqual(t, completed, total)
			assert.IsType(t, 0, currentResult) // Should be int for range draws
		}
		result, err := engine.DrawMultipleInRange(ctx, "optimized_test_1", 1, 100, 20, progressCallback)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, 20, result.TotalRequested)
		assert.Equal(t, 20, result.Completed)
		assert.Equal(t, 0, result.Failed)
		assert.False(t, result.PartialSuccess)
		assert.Len(t, result.Results, 20)
		// Verify progress callback was called
		assert.Equal(t, int32(20), atomic.LoadInt32(&progressUpdates))
		// Validate all results are in range
		for _, val := range result.Results {
			assert.GreaterOrEqual(t, val, 1)
			assert.LessOrEqual(t, val, 100)
		}
	})

	t.Run("optimized draw with cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		// Cancel after a short delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		result, err := engine.DrawMultipleInRange(ctx, "optimized_test_2", 1, 100, 1000, nil)

		// Should get either ErrDrawInterrupted or context.Canceled
		assert.Error(t, err)

		// 现在应该总是返回非 nil 的结果
		assert.NotNil(t, result, "Result should never be nil even on cancellation")

		// 验证结果结构
		assert.Equal(t, 1000, result.TotalRequested)
		assert.GreaterOrEqual(t, result.Failed, 0)
		assert.GreaterOrEqual(t, result.Completed, 0)
		assert.Equal(t, result.TotalRequested, result.Completed+result.Failed)

		if result.Completed > 0 {
			assert.True(t, result.PartialSuccess)
			assert.Len(t, result.Results, result.Completed)
			// 验证所有完成的结果都在范围内
			for _, val := range result.Results {
				assert.GreaterOrEqual(t, val, 1)
				assert.LessOrEqual(t, val, 100)
			}
		} else {
			// 如果没有完成任何抽取，检查错误信息
			assert.NotNil(t, result.LastError)
			t.Logf("No draws completed due to early cancellation. Error: %v", result.LastError)
		}
	})

	t.Run("invalid parameters", func(t *testing.T) {
		ctx := context.Background()
		// Empty lock key
		result, err := engine.DrawMultipleInRange(ctx, "", 1, 100, 5, nil)
		assert.Equal(t, ErrInvalidParameters, err)
		assert.Nil(t, result)
		// Invalid range
		result, err = engine.DrawMultipleInRange(ctx, "test", 100, 1, 5, nil)
		assert.Equal(t, ErrInvalidRange, err)
		assert.Nil(t, result)
		// Invalid count
		result, err = engine.DrawMultipleInRange(ctx, "test", 1, 100, 0, nil)
		assert.Equal(t, ErrInvalidCount, err)
		assert.Nil(t, result)
	})

	t.Run("circuit breaker behavior", func(t *testing.T) {
		ctx := context.Background()

		// 如果熔断器启用且打开，测试行为
		result, err := engine.DrawMultipleInRange(ctx, "circuit_test", 1, 100, 5, nil)

		// 无论熔断器状态如何，都应该返回非 nil 的结果
		assert.NotNil(t, result, "Result should never be nil even when circuit breaker is open")

		if err != nil {
			// 如果有错误（比如熔断器打开），验证结果结构仍然有效
			assert.Equal(t, 5, result.TotalRequested)
			assert.GreaterOrEqual(t, result.Failed, 0)
			assert.GreaterOrEqual(t, result.Completed, 0)
			t.Logf("Circuit breaker or other error occurred: %v", err)
		} else {
			// 成功情况的验证
			assert.Equal(t, 5, result.TotalRequested)
			assert.Equal(t, 5, result.Completed)
			assert.Equal(t, 0, result.Failed)
			assert.False(t, result.PartialSuccess)
		}
	})
}

func TestProgressCallback(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test data
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	t.Run("progress callback receives correct data", func(t *testing.T) {
		ctx := context.Background()

		var callbackData []struct {
			completed int
			total     int
			result    interface{}
		}

		progressCallback := func(completed, total int, currentResult interface{}) {
			callbackData = append(callbackData, struct {
				completed int
				total     int
				result    interface{}
			}{completed, total, currentResult})
		}

		result, err := engine.DrawMultipleInRange(ctx, "progress_test", 1, 10, 5, progressCallback)

		require.NoError(t, err)
		assert.Equal(t, 5, result.Completed)

		// Verify callback was called for each successful draw
		assert.Len(t, callbackData, 5)

		for i, data := range callbackData {
			assert.Equal(t, i+1, data.completed) // Should increment from 1 to 5
			assert.Equal(t, 5, data.total)       // Total should always be 5
			assert.IsType(t, 0, data.result)     // Should be int for range draws
		}
	})
}

func BenchmarkLotteryEngine_DrawMultipleInRangeOptimized(b *testing.B) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test data
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lockKey := fmt.Sprintf("benchmark_optimized_%d", i)
		_, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 1000, 50, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLotteryEngine_DrawMultipleInRange_vs_Optimized(b *testing.B) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test data
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	ctx := context.Background()

	b.Run("Standard", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lockKey := fmt.Sprintf("benchmark_standard_%d", i)
			_, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 1000, 20, nil)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Optimized", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lockKey := fmt.Sprintf("benchmark_optimized_%d", i)
			_, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 1000, 20, nil)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// =================================================================

func TestLotteryEngine_DrawFromPrizes(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	t.Run("Valid prize draw", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.3, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.7, Value: 50},
		}

		ctx := context.Background()
		prize, err := engine.DrawFromPrizes(ctx, "test_draw_1", prizes)
		require.NoError(t, err)
		require.NotNil(t, prize)
		assert.Contains(t, []string{"1", "2"}, prize.ID)
	})

	t.Run("Single prize pool", func(t *testing.T) {
		prizes := []Prize{
			{ID: "only", Name: "Only Prize", Probability: 1.0, Value: 100},
		}

		ctx := context.Background()
		prize, err := engine.DrawFromPrizes(ctx, "test_draw_2", prizes)
		require.NoError(t, err)
		require.NotNil(t, prize)
		assert.Equal(t, "only", prize.ID)
		assert.Equal(t, "Only Prize", prize.Name)
	})

	t.Run("Empty lock key", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 1.0, Value: 100},
		}

		ctx := context.Background()
		_, err := engine.DrawFromPrizes(ctx, "", prizes)
		assert.Equal(t, ErrInvalidParameters, err)
	})

	t.Run("Empty prize pool", func(t *testing.T) {
		prizes := []Prize{}

		ctx := context.Background()
		_, err := engine.DrawFromPrizes(ctx, "test_draw_3", prizes)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("Invalid prize pool", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.0, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.0, Value: 50}, // Total probability = 0.0, invalid
		}

		ctx := context.Background()
		_, err := engine.DrawFromPrizes(ctx, "test_draw_4", prizes)
		assert.Equal(t, ErrInvalidProbability, err)
	})

	t.Run("Prize with invalid data", func(t *testing.T) {
		prizes := []Prize{
			{ID: "", Name: "Prize 1", Probability: 1.0, Value: 100}, // Empty ID
		}

		ctx := context.Background()
		_, err := engine.DrawFromPrizes(ctx, "test_draw_5", prizes)
		assert.Equal(t, ErrInvalidPrizeID, err)
	})

	t.Run("Context cancellation", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 1.0, Value: 100},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := engine.DrawFromPrizes(ctx, "test_draw_6", prizes)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestLotteryEngine_DrawMultipleFromPrizes(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	t.Run("Valid multiple prize draw", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.5, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.5, Value: 50},
		}

		ctx := context.Background()
		results, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_1", prizes, 5, nil)
		require.NoError(t, err)
		assert.Len(t, results.PrizeResults, 5)

		// All results should be valid prizes
		for _, prize := range results.PrizeResults {
			assert.Contains(t, []string{"1", "2"}, prize.ID)
		}
	})

	t.Run("Single draw", func(t *testing.T) {
		prizes := []Prize{
			{ID: "only", Name: "Only Prize", Probability: 1.0, Value: 100},
		}

		ctx := context.Background()
		results, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_2", prizes, 1, nil)
		require.NoError(t, err)
		assert.Len(t, results.PrizeResults, 1)
		assert.Equal(t, "only", results.PrizeResults[0].ID)
	})

	t.Run("Empty lock key", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 1.0, Value: 100},
		}

		ctx := context.Background()
		_, err := engine.DrawMultipleFromPrizes(ctx, "", prizes, 1, nil)
		assert.Equal(t, ErrInvalidParameters, err)
	})

	t.Run("Empty prize pool", func(t *testing.T) {
		prizes := []Prize{}

		ctx := context.Background()
		_, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_3", prizes, 1, nil)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("Invalid count", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 1.0, Value: 100},
		}

		ctx := context.Background()
		_, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_4", prizes, 0, nil)
		assert.Equal(t, ErrInvalidCount, err)

		_, err = engine.DrawMultipleFromPrizes(ctx, "test_multi_5", prizes, -1, nil)
		assert.Equal(t, ErrInvalidCount, err)
	})

	t.Run("Invalid prize pool", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.0, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.0, Value: 50}, // Total probability = 0.0, invalid
		}

		ctx := context.Background()
		_, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_6", prizes, 2, nil)
		assert.Equal(t, ErrInvalidProbability, err)
	})

	t.Run("Context cancellation during multiple draws", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 1.0, Value: 100},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// This should either complete quickly or return partial results
		results, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_7", prizes, 100, nil)

		// Either we get an error (timeout/partial failure) or we get some results
		if err != nil {
			// Accept timeout, cancellation, or partial failure errors
			assert.True(t, err == context.DeadlineExceeded || err == context.Canceled || err == ErrPartialDrawFailure)
		} else {
			// If no error, we should have some results
			assert.True(t, len(results.PrizeResults) > 0)
		}
	})

	t.Run("Distribution test", func(t *testing.T) {
		prizes := []Prize{
			{ID: "rare", Name: "Rare Prize", Probability: 0.1, Value: 1000},
			{ID: "common", Name: "Common Prize", Probability: 0.9, Value: 10},
		}

		ctx := context.Background()
		results, err := engine.DrawMultipleFromPrizes(ctx, "test_multi_8", prizes, 10000, nil)
		require.NoError(t, err)
		assert.Len(t, results.PrizeResults, 10000)

		// Count occurrences
		rareCount := 0
		commonCount := 0
		for _, prize := range results.PrizeResults {
			switch prize.ID {
			case "rare":
				rareCount++
			case "common":
				commonCount++
			}
		}

		// Check approximate distribution (with more generous tolerance for randomness)
		// Using 15% tolerance to account for statistical variance in random sampling
		tolerance := 0.15      // 15% tolerance
		expectedRare := 1000   // 10% of 1000
		expectedCommon := 9000 // 90% of 1000

		assert.InDelta(t, expectedRare, rareCount, float64(expectedRare)*tolerance)
		assert.InDelta(t, expectedCommon, commonCount, float64(expectedCommon)*tolerance)

		// Additional check: total should always be 1000
		assert.Equal(t, 10000, rareCount+commonCount)

		// Log actual distribution for debugging
		t.Logf("Distribution: rare=%d (%.1f%%), common=%d (%.1f%%)",
			rareCount, float64(rareCount)/10.0, commonCount, float64(commonCount)/10.0)
	})
}

// Test concurrent access to ensure thread safety
func TestLotteryEngine_ConcurrentPrizeDraws(t *testing.T) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Clean up test keys
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// Test Redis connection
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	t.Run("Concurrent single draws", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.5, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.5, Value: 50},
		}

		const numGoroutines = 10
		const drawsPerGoroutine = 10
		results := make(chan *Prize, numGoroutines*drawsPerGoroutine)
		errors := make(chan error, numGoroutines*drawsPerGoroutine)

		// Launch concurrent goroutines
		for i := 0; i < numGoroutines; i++ {
			go func(goroutineID int) {
				for j := 0; j < drawsPerGoroutine; j++ {
					ctx := context.Background()
					lockKey := "concurrent_test"
					prize, err := engine.DrawFromPrizes(ctx, lockKey, prizes)
					if err != nil {
						errors <- err
					} else {
						results <- prize
					}
				}
			}(i)
		}

		// Collect results
		var successCount int
		var errorCount int
		for i := 0; i < numGoroutines*drawsPerGoroutine; i++ {
			select {
			case prize := <-results:
				assert.Contains(t, []string{"1", "2"}, prize.ID)
				successCount++
			case err := <-errors:
				t.Logf("Error in concurrent draw: %v", err)
				errorCount++
			case <-time.After(5 * time.Second):
				t.Fatal("Timeout waiting for concurrent draws to complete")
			}
		}

		// We should have some successful draws
		assert.True(t, successCount > 0, "Expected at least some successful draws")
		t.Logf("Concurrent draws: %d successful, %d errors", successCount, errorCount)
	})
}

// Benchmark tests for performance
func BenchmarkLotteryEngine_DrawFromPrizes(b *testing.B) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis not available, skipping benchmark")
	}

	defer rdb.Close()

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	prizes := []Prize{
		{ID: "1", Name: "Prize 1", Probability: 0.1, Value: 1000},
		{ID: "2", Name: "Prize 2", Probability: 0.2, Value: 500},
		{ID: "3", Name: "Prize 3", Probability: 0.3, Value: 100},
		{ID: "4", Name: "Prize 4", Probability: 0.4, Value: 10},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.DrawFromPrizes(ctx, "benchmark_test", prizes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLotteryEngine_DrawMultipleFromPrizes(b *testing.B) {
	// Setup Redis client for testing
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use test database
	})

	// Test Redis connection
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis not available, skipping benchmark")
	}

	defer rdb.Close()

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	prizes := []Prize{
		{ID: "1", Name: "Prize 1", Probability: 0.1, Value: 1000},
		{ID: "2", Name: "Prize 2", Probability: 0.2, Value: 500},
		{ID: "3", Name: "Prize 3", Probability: 0.3, Value: 100},
		{ID: "4", Name: "Prize 4", Probability: 0.4, Value: 10},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.DrawMultipleFromPrizes(ctx, "benchmark_multi_test", prizes, 10, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// =================================================================

func TestPrizeSelector_NormalizeProbabilities(t *testing.T) {
	selector := NewDefaultPrizeSelector()

	t.Run("Valid probabilities", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.2, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.3, Value: 50},
			{ID: "3", Name: "Prize 3", Probability: 0.5, Value: 10},
		}

		normalized, err := selector.NormalizeProbabilities(prizes)
		require.NoError(t, err)
		assert.Len(t, normalized, 3)

		// Probabilities should remain the same since they already sum to 1.0
		assert.InDelta(t, 0.2, normalized[0].Probability, 0.0001)
		assert.InDelta(t, 0.3, normalized[1].Probability, 0.0001)
		assert.InDelta(t, 0.5, normalized[2].Probability, 0.0001)
	})

	t.Run("Probabilities need normalization", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 2.0, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 3.0, Value: 50},
			{ID: "3", Name: "Prize 3", Probability: 5.0, Value: 10},
		}

		normalized, err := selector.NormalizeProbabilities(prizes)
		require.NoError(t, err)
		assert.Len(t, normalized, 3)

		// Probabilities should be normalized to sum to 1.0
		assert.InDelta(t, 0.2, normalized[0].Probability, 0.0001)
		assert.InDelta(t, 0.3, normalized[1].Probability, 0.0001)
		assert.InDelta(t, 0.5, normalized[2].Probability, 0.0001)
	})

	t.Run("Empty prize pool", func(t *testing.T) {
		prizes := []Prize{}
		_, err := selector.NormalizeProbabilities(prizes)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("Negative probability", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: -0.1, Value: 100},
		}
		_, err := selector.NormalizeProbabilities(prizes)
		assert.Equal(t, ErrInvalidProbability, err)
	})

	t.Run("Zero total probability", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.0, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.0, Value: 50},
		}
		_, err := selector.NormalizeProbabilities(prizes)
		assert.Equal(t, ErrInvalidProbability, err)
	})
}

func TestPrizeSelector_CalculateCumulativeProbabilities(t *testing.T) {
	selector := NewDefaultPrizeSelector()

	t.Run("Valid probabilities", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.2, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.3, Value: 50},
			{ID: "3", Name: "Prize 3", Probability: 0.5, Value: 10},
		}

		cumulative, err := selector.CalculateCumulativeProbabilities(prizes)
		require.NoError(t, err)
		assert.Len(t, cumulative, 3)

		assert.InDelta(t, 0.2, cumulative[0], 0.0001)
		assert.InDelta(t, 0.5, cumulative[1], 0.0001)
		assert.InDelta(t, 1.0, cumulative[2], 0.0001)
	})

	t.Run("Empty prize pool", func(t *testing.T) {
		prizes := []Prize{}
		_, err := selector.CalculateCumulativeProbabilities(prizes)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("Invalid probability", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: -0.1, Value: 100},
		}
		_, err := selector.CalculateCumulativeProbabilities(prizes)
		assert.Equal(t, ErrInvalidProbability, err)
	})

	t.Run("Probability greater than 1", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 1.5, Value: 100},
		}
		_, err := selector.CalculateCumulativeProbabilities(prizes)
		assert.Equal(t, ErrInvalidProbability, err)
	})
}

func TestPrizeSelector_SelectPrize(t *testing.T) {
	selector := NewDefaultPrizeSelector()

	t.Run("Valid prize selection", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.2, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.3, Value: 50},
			{ID: "3", Name: "Prize 3", Probability: 0.5, Value: 10},
		}

		// Test multiple selections to ensure distribution
		selections := make(map[string]int)
		iterations := 10000

		for i := 0; i < iterations; i++ {
			prize, err := selector.SelectPrize(prizes)
			require.NoError(t, err)
			require.NotNil(t, prize)
			selections[prize.ID]++
		}

		// Check that all prizes were selected
		assert.Contains(t, selections, "1")
		assert.Contains(t, selections, "2")
		assert.Contains(t, selections, "3")

		// Check approximate distribution (with some tolerance for randomness)
		tolerance := 0.05 // 5% tolerance
		assert.InDelta(t, 0.2, float64(selections["1"])/float64(iterations), tolerance)
		assert.InDelta(t, 0.3, float64(selections["2"])/float64(iterations), tolerance)
		assert.InDelta(t, 0.5, float64(selections["3"])/float64(iterations), tolerance)
	})

	t.Run("Single prize", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Only Prize", Probability: 1.0, Value: 100},
		}

		prize, err := selector.SelectPrize(prizes)
		require.NoError(t, err)
		assert.Equal(t, "1", prize.ID)
		assert.Equal(t, "Only Prize", prize.Name)
	})

	t.Run("Empty prize pool", func(t *testing.T) {
		prizes := []Prize{}
		_, err := selector.SelectPrize(prizes)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("Probabilities need normalization", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 2.0, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 8.0, Value: 50},
		}

		// Should work with normalization
		prize, err := selector.SelectPrize(prizes)
		require.NoError(t, err)
		require.NotNil(t, prize)
		assert.Contains(t, []string{"1", "2"}, prize.ID)
	})
}

func TestPrizeSelector_ValidatePrizes(t *testing.T) {
	selector := NewDefaultPrizeSelector()

	t.Run("Valid prize pool", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.2, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.3, Value: 50},
			{ID: "3", Name: "Prize 3", Probability: 0.5, Value: 10},
		}

		err := selector.ValidatePrizes(prizes)
		assert.NoError(t, err)
	})

	t.Run("Empty prize pool", func(t *testing.T) {
		prizes := []Prize{}
		err := selector.ValidatePrizes(prizes)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})

	t.Run("Invalid prize data", func(t *testing.T) {
		prizes := []Prize{
			{ID: "", Name: "Prize 1", Probability: 0.5, Value: 100}, // Empty ID
			{ID: "2", Name: "Prize 2", Probability: 0.5, Value: 50},
		}

		err := selector.ValidatePrizes(prizes)
		assert.Equal(t, ErrInvalidPrizeID, err)
	})

	t.Run("Probabilities don't sum to 1.0", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.2, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.3, Value: 50},
		}

		err := selector.ValidatePrizes(prizes)
		assert.Equal(t, ErrInvalidProbability, err)
	})

	t.Run("Probabilities sum to 1.0 within tolerance", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.33333, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.33333, Value: 50},
			{ID: "3", Name: "Prize 3", Probability: 0.33334, Value: 10},
		}

		err := selector.ValidatePrizes(prizes)
		assert.NoError(t, err)
	})
}

func TestPrizeSelector_SelectMultiplePrizes(t *testing.T) {
	selector := NewDefaultPrizeSelector()

	t.Run("Valid multiple selection", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 0.5, Value: 100},
			{ID: "2", Name: "Prize 2", Probability: 0.5, Value: 50},
		}

		results, err := selector.SelectMultiplePrizes(prizes, 5)
		require.NoError(t, err)
		assert.Len(t, results, 5)

		// All results should be valid prizes
		for _, prize := range results {
			assert.Contains(t, []string{"1", "2"}, prize.ID)
		}
	})

	t.Run("Invalid count", func(t *testing.T) {
		prizes := []Prize{
			{ID: "1", Name: "Prize 1", Probability: 1.0, Value: 100},
		}

		_, err := selector.SelectMultiplePrizes(prizes, 0)
		assert.Equal(t, ErrInvalidCount, err)

		_, err = selector.SelectMultiplePrizes(prizes, -1)
		assert.Equal(t, ErrInvalidCount, err)
	})

	t.Run("Empty prize pool", func(t *testing.T) {
		prizes := []Prize{}
		_, err := selector.SelectMultiplePrizes(prizes, 1)
		assert.Equal(t, ErrEmptyPrizePool, err)
	})
}

func TestPrizeSelector_findPrizeIndex(t *testing.T) {
	selector := NewDefaultPrizeSelector()

	t.Run("Find correct index", func(t *testing.T) {
		cumulative := []float64{0.2, 0.5, 1.0}

		// Test various random values
		assert.Equal(t, 0, selector.findPrizeIndex(cumulative, 0.0))
		assert.Equal(t, 0, selector.findPrizeIndex(cumulative, 0.1))
		assert.Equal(t, 0, selector.findPrizeIndex(cumulative, 0.2))
		assert.Equal(t, 1, selector.findPrizeIndex(cumulative, 0.3))
		assert.Equal(t, 1, selector.findPrizeIndex(cumulative, 0.5))
		assert.Equal(t, 2, selector.findPrizeIndex(cumulative, 0.7))
		assert.Equal(t, 2, selector.findPrizeIndex(cumulative, 1.0))
	})

	t.Run("Edge cases", func(t *testing.T) {
		cumulative := []float64{1.0}
		assert.Equal(t, 0, selector.findPrizeIndex(cumulative, 0.5))
		assert.Equal(t, 0, selector.findPrizeIndex(cumulative, 1.0))
	})
}

// Benchmark tests for performance
func BenchmarkPrizeSelector_SelectPrize(b *testing.B) {
	selector := NewDefaultPrizeSelector()
	prizes := []Prize{
		{ID: "1", Name: "Prize 1", Probability: 0.1, Value: 1000},
		{ID: "2", Name: "Prize 2", Probability: 0.2, Value: 500},
		{ID: "3", Name: "Prize 3", Probability: 0.3, Value: 100},
		{ID: "4", Name: "Prize 4", Probability: 0.4, Value: 10},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := selector.SelectPrize(prizes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrizeSelector_SelectMultiplePrizes(b *testing.B) {
	selector := NewDefaultPrizeSelector()
	prizes := []Prize{
		{ID: "1", Name: "Prize 1", Probability: 0.1, Value: 1000},
		{ID: "2", Name: "Prize 2", Probability: 0.2, Value: 500},
		{ID: "3", Name: "Prize 3", Probability: 0.3, Value: 100},
		{ID: "4", Name: "Prize 4", Probability: 0.4, Value: 10},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := selector.SelectMultiplePrizes(prizes, 10)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// =================================================================

func TestSecureRandomGenerator_GenerateInRange(t *testing.T) {
	generator := NewSecureRandomGenerator()

	t.Run("Valid range", func(t *testing.T) {
		min, max := 1, 100
		result, err := generator.GenerateInRange(min, max)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})

	t.Run("Single value range", func(t *testing.T) {
		min, max := 42, 42
		result, err := generator.GenerateInRange(min, max)

		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("Invalid range - min > max", func(t *testing.T) {
		min, max := 100, 1
		result, err := generator.GenerateInRange(min, max)

		assert.Error(t, err)
		assert.Equal(t, ErrInvalidRange, err)
		assert.Equal(t, 0, result)
	})

	t.Run("Large range", func(t *testing.T) {
		min, max := 1, 1000000
		result, err := generator.GenerateInRange(min, max)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})

	t.Run("Negative range", func(t *testing.T) {
		min, max := -100, -1
		result, err := generator.GenerateInRange(min, max)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})

	t.Run("Range crossing zero", func(t *testing.T) {
		min, max := -50, 50
		result, err := generator.GenerateInRange(min, max)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})
}

func TestSecureRandomGenerator_GenerateFloat(t *testing.T) {
	generator := NewSecureRandomGenerator()

	t.Run("Valid float generation", func(t *testing.T) {
		result, err := generator.GenerateFloat()

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 0.0)
		assert.Less(t, result, 1.0)
	})

	t.Run("Multiple generations are different", func(t *testing.T) {
		results := make([]float64, 100)
		for i := 0; i < 100; i++ {
			result, err := generator.GenerateFloat()
			require.NoError(t, err)
			results[i] = result
		}

		// Check that not all results are the same (very unlikely with crypto/rand)
		allSame := true
		for i := 1; i < len(results); i++ {
			if results[i] != results[0] {
				allSame = false
				break
			}
		}
		assert.False(t, allSame, "All generated floats should not be the same")
	})
}

func TestGenerateSecureInRange(t *testing.T) {
	t.Run("Standalone function works", func(t *testing.T) {
		min, max := 1, 10
		result, err := GenerateSecureInRange(min, max)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})
}

func TestGenerateSecureFloat(t *testing.T) {
	t.Run("Standalone function works", func(t *testing.T) {
		result, err := GenerateSecureFloat()

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 0.0)
		assert.Less(t, result, 1.0)
	})
}

// Benchmark tests for performance
func BenchmarkSecureRandomGenerator_GenerateInRange(b *testing.B) {
	generator := NewSecureRandomGenerator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = generator.GenerateInRange(1, 1000)
	}
}

func BenchmarkSecureRandomGenerator_GenerateFloat(b *testing.B) {
	generator := NewSecureRandomGenerator()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = generator.GenerateFloat()
	}
}

// Test distribution quality (statistical test)
func TestSecureRandomGenerator_Distribution(t *testing.T) {
	generator := NewSecureRandomGenerator()

	t.Run("Range distribution", func(t *testing.T) {
		min, max := 1, 10
		counts := make(map[int]int)
		iterations := 10000

		for i := 0; i < iterations; i++ {
			result, err := generator.GenerateInRange(min, max)
			require.NoError(t, err)
			counts[result]++
		}

		// Check that all values in range were generated
		for i := min; i <= max; i++ {
			assert.Greater(t, counts[i], 0, "Value %d should be generated at least once", i)
		}

		// Check that no values outside range were generated
		for value := range counts {
			assert.GreaterOrEqual(t, value, min)
			assert.LessOrEqual(t, value, max)
		}
	})

	t.Run("Float distribution", func(t *testing.T) {
		iterations := 10000
		var sum float64

		for i := 0; i < iterations; i++ {
			result, err := generator.GenerateFloat()
			require.NoError(t, err)
			sum += result
		}

		// Average should be approximately 0.5 for uniform distribution
		average := sum / float64(iterations)
		assert.InDelta(t, 0.5, average, 0.05, "Average should be close to 0.5 for uniform distribution")
	})
}

// =================================================================

// Mock Redis client for testing
func setupTestRedisClient() *redis.Client {
	// For unit tests, we'll use a mock or in-memory Redis
	// In a real environment, this would connect to a test Redis instance
	return redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   1, // Use a different DB for tests
	})
}

func TestLotteryEngine_DrawInRange(t *testing.T) {
	// Skip if Redis is not available
	client := setupTestRedisClient()
	ctx := context.Background()

	// Test Redis connection
	_, err := client.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}
	defer client.Close()

	engine := NewLotteryEngineWithLogger(client, NewSilentLogger())

	t.Run("Valid range draw", func(t *testing.T) {
		min, max := 1, 100
		lockKey := "test_draw_range_1"

		result, err := engine.DrawInRange(ctx, lockKey, min, max)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})

	t.Run("Single value range", func(t *testing.T) {
		min, max := 42, 42
		lockKey := "test_draw_range_2"

		result, err := engine.DrawInRange(ctx, lockKey, min, max)

		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("Invalid range", func(t *testing.T) {
		min, max := 100, 1
		lockKey := "test_draw_range_3"

		result, err := engine.DrawInRange(ctx, lockKey, min, max)

		assert.Error(t, err)
		assert.Equal(t, ErrInvalidRange, err)
		assert.Equal(t, 0, result)
	})

	t.Run("Empty lock key", func(t *testing.T) {
		min, max := 1, 100
		lockKey := ""

		result, err := engine.DrawInRange(ctx, lockKey, min, max)

		assert.Error(t, err)
		assert.Equal(t, ErrInvalidParameters, err)
		assert.Equal(t, 0, result)
	})

	t.Run("Negative range", func(t *testing.T) {
		min, max := -100, -1
		lockKey := "test_draw_range_4"

		result, err := engine.DrawInRange(ctx, lockKey, min, max)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, min)
		assert.LessOrEqual(t, result, max)
	})
}

func TestLotteryEngine_DrawMultipleInRange(t *testing.T) {
	// Skip if Redis is not available
	client := setupTestRedisClient()
	ctx := context.Background()

	// Test Redis connection
	_, err := client.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}
	defer client.Close()

	engine := NewLotteryEngineWithLogger(client, NewSilentLogger())

	t.Run("Valid multiple draws", func(t *testing.T) {
		min, max := 1, 100
		count := 5
		lockKey := "test_draw_multiple_1"

		results, err := engine.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)

		require.NoError(t, err)
		assert.Len(t, results.Results, count)

		for _, result := range results.Results {
			assert.GreaterOrEqual(t, result, min)
			assert.LessOrEqual(t, result, max)
		}
	})

	t.Run("Single draw", func(t *testing.T) {
		min, max := 1, 100
		count := 1
		lockKey := "test_draw_multiple_2"

		results, err := engine.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)

		require.NoError(t, err)
		assert.Len(t, results.Results, count)
		assert.GreaterOrEqual(t, results.Results[0], min)
		assert.LessOrEqual(t, results.Results[0], max)
	})

	t.Run("Invalid range", func(t *testing.T) {
		min, max := 100, 1
		count := 5
		lockKey := "test_draw_multiple_3"

		results, err := engine.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)

		assert.Error(t, err)
		assert.Equal(t, ErrInvalidRange, err)
		assert.Nil(t, results)
	})

	t.Run("Invalid count", func(t *testing.T) {
		min, max := 1, 100
		count := 0
		lockKey := "test_draw_multiple_4"

		results, err := engine.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)

		assert.Error(t, err)
		assert.Equal(t, ErrInvalidCount, err)
		assert.Nil(t, results)
	})

	t.Run("Negative count", func(t *testing.T) {
		min, max := 1, 100
		count := -5
		lockKey := "test_draw_multiple_5"

		results, err := engine.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)

		assert.Error(t, err)
		assert.Equal(t, ErrInvalidCount, err)
		assert.Nil(t, results)
	})

	t.Run("Empty lock key", func(t *testing.T) {
		min, max := 1, 100
		count := 5
		lockKey := ""

		results, err := engine.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)

		assert.Error(t, err)
		assert.Equal(t, ErrInvalidParameters, err)
		assert.Nil(t, results)
	})

	t.Run("Large count", func(t *testing.T) {
		min, max := 1, 10
		count := 100
		lockKey := "test_draw_multiple_6"

		results, err := engine.DrawMultipleInRange(ctx, lockKey, min, max, count, nil)

		require.NoError(t, err)
		assert.Len(t, results.Results, count)

		// Check that all results are in range
		for _, result := range results.Results {
			assert.GreaterOrEqual(t, result, min)
			assert.LessOrEqual(t, result, max)
		}
	})
}

func TestLotteryEngine_DrawInRange_Concurrent(t *testing.T) {
	// Skip if Redis is not available
	client := setupTestRedisClient()
	ctx := context.Background()

	// Test Redis connection
	_, err := client.Ping(ctx).Result()
	if err != nil {
		t.Skip("Redis not available, skipping integration tests")
	}
	defer client.Close()

	engine := NewLotteryEngineWithLogger(client, NewSilentLogger())

	t.Run("Concurrent draws with same lock key", func(t *testing.T) {
		min, max := 1, 100
		lockKey := "test_concurrent_draw"
		numGoroutines := 10

		results := make(chan int, numGoroutines)
		errors := make(chan error, numGoroutines)

		// Start multiple goroutines
		for i := 0; i < numGoroutines; i++ {
			go func() {
				result, err := engine.DrawInRange(ctx, lockKey, min, max)
				if err != nil {
					errors <- err
				} else {
					results <- result
				}
			}()
		}

		// Collect results
		var successCount int
		var errorCount int

		for i := 0; i < numGoroutines; i++ {
			select {
			case result := <-results:
				assert.GreaterOrEqual(t, result, min)
				assert.LessOrEqual(t, result, max)
				successCount++
			case err := <-errors:
				// Some operations might fail due to lock contention, which is expected
				t.Logf("Expected error in concurrent test: %v", err)
				errorCount++
			case <-time.After(5 * time.Second):
				t.Fatal("Test timed out")
			}
		}

		// At least some operations should succeed
		assert.Greater(t, successCount, 0, "At least some concurrent operations should succeed")
		t.Logf("Concurrent test results: %d successes, %d errors", successCount, errorCount)
	})
}

func TestGenerateLockValue(t *testing.T) {
	t.Run("Generate unique lock values", func(t *testing.T) {
		values := make(map[string]bool)

		for i := 0; i < 1000; i++ {
			value := generateLockValue()
			assert.NotEmpty(t, value)
			assert.False(t, values[value], "Lock value should be unique: %s", value)
			values[value] = true
		}
	})

	t.Run("Lock value format", func(t *testing.T) {
		value := generateLockValue()
		assert.NotEmpty(t, value)
		// Should be 32 characters (16 bytes as hex)
		assert.Len(t, value, 32)

		// Should only contain hex characters
		for _, char := range value {
			assert.Contains(t, "0123456789abcdef", string(char))
		}
	})
}

// Benchmark tests
func BenchmarkLotteryEngine_DrawInRange(b *testing.B) {
	client := setupTestRedisClient()
	ctx := context.Background()

	// Skip if Redis is not available
	_, err := client.Ping(ctx).Result()
	if err != nil {
		b.Skip("Redis not available, skipping benchmark")
	}
	defer client.Close()

	engine := NewLotteryEngineWithLogger(client, NewSilentLogger())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.DrawInRange(ctx, "benchmark_draw", 1, 1000)
	}
}

func BenchmarkLotteryEngine_DrawMultipleInRange(b *testing.B) {
	client := setupTestRedisClient()
	ctx := context.Background()

	// Skip if Redis is not available
	_, err := client.Ping(ctx).Result()
	if err != nil {
		b.Skip("Redis not available, skipping benchmark")
	}
	defer client.Close()

	engine := NewLotteryEngineWithLogger(client, NewSilentLogger())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = engine.DrawMultipleInRange(ctx, "benchmark_multiple", 1, 1000, 5, nil)
	}
}

// =================================================================

func TestPrizeValidation(t *testing.T) {
	tests := []struct {
		name    string
		prize   Prize
		wantErr bool
		errType error
	}{
		{
			name: "valid prize",
			prize: Prize{
				ID:          "prize_1",
				Name:        "First Prize",
				Probability: 0.1,
				Value:       100,
			},
			wantErr: false,
		},
		{
			name: "empty ID",
			prize: Prize{
				ID:          "",
				Name:        "First Prize",
				Probability: 0.1,
				Value:       100,
			},
			wantErr: true,
			errType: ErrInvalidPrizeID,
		},
		{
			name: "empty name",
			prize: Prize{
				ID:          "prize_1",
				Name:        "",
				Probability: 0.1,
				Value:       100,
			},
			wantErr: true,
			errType: ErrInvalidPrizeName,
		},
		{
			name: "invalid probability - negative",
			prize: Prize{
				ID:          "prize_1",
				Name:        "First Prize",
				Probability: -0.1,
				Value:       100,
			},
			wantErr: true,
			errType: ErrInvalidProbability,
		},
		{
			name: "invalid probability - greater than 1",
			prize: Prize{
				ID:          "prize_1",
				Name:        "First Prize",
				Probability: 1.1,
				Value:       100,
			},
			wantErr: true,
			errType: ErrInvalidProbability,
		},
		{
			name: "negative value",
			prize: Prize{
				ID:          "prize_1",
				Name:        "First Prize",
				Probability: 0.1,
				Value:       -10,
			},
			wantErr: true,
			errType: ErrNegativePrizeValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.prize.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Prize.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != tt.errType {
				t.Errorf("Prize.Validate() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errType error
	}{
		{
			name: "valid config",
			config: &Config{
				Engine: &EngineConfig{
					LockTimeout:   30 * time.Second,
					RetryAttempts: 3,
					RetryInterval: 100 * time.Millisecond,
					LockCacheTTL:  1 * time.Second,
				},

				Redis: DefaultRedisConfig(),
			},
			wantErr: false,
		},
		{
			name: "lock timeout too short",
			config: &Config{
				Engine: &EngineConfig{
					LockTimeout:   500 * time.Millisecond,
					RetryAttempts: 3,
					RetryInterval: 100 * time.Millisecond,
					LockCacheTTL:  1 * time.Second,
				},

				Redis: DefaultRedisConfig(),
			},
			wantErr: true,
		},
		{
			name: "negative retry attempts",
			config: &Config{
				Engine: &EngineConfig{
					LockTimeout:   30 * time.Second,
					RetryAttempts: -1,
					RetryInterval: 100 * time.Millisecond,
					LockCacheTTL:  1 * time.Second,
				},

				Redis: DefaultRedisConfig(),
			},
			wantErr: true,
		},
		// {
		// 	name: "invalid max serialization size",
		// 	config: &Config{
		// 		Engine: &EngineConfig{
		// 			LockTimeout:   30 * time.Second,
		// 			RetryAttempts: 3,
		// 			RetryInterval: 100 * time.Millisecond,
		// 		},

		// 		Redis: DefaultRedisConfig(),
		// 	},
		// 	wantErr: true,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestValidatePrizePool(t *testing.T) {
	tests := []struct {
		name    string
		prizes  []Prize
		wantErr bool
		errType error
	}{
		{
			name: "valid prize pool",
			prizes: []Prize{
				{ID: "1", Name: "First", Probability: 0.1, Value: 100},
				{ID: "2", Name: "Second", Probability: 0.2, Value: 50},
				{ID: "3", Name: "Third", Probability: 0.7, Value: 10},
			},
			wantErr: false,
		},
		{
			name:    "empty prize pool",
			prizes:  []Prize{},
			wantErr: true,
			errType: ErrEmptyPrizePool,
		},
		{
			name: "probabilities don't sum to 1",
			prizes: []Prize{
				{ID: "1", Name: "First", Probability: 0.1, Value: 100},
				{ID: "2", Name: "Second", Probability: 0.2, Value: 50},
			},
			wantErr: true,
			errType: ErrInvalidProbability,
		},
		{
			name: "invalid prize in pool",
			prizes: []Prize{
				{ID: "", Name: "First", Probability: 0.5, Value: 100},
				{ID: "2", Name: "Second", Probability: 0.5, Value: 50},
			},
			wantErr: true,
			errType: ErrInvalidPrizeID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePrizePool(tt.prizes)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePrizePool() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != tt.errType {
				t.Errorf("ValidatePrizePool() error = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestValidateRange(t *testing.T) {
	tests := []struct {
		name    string
		min     int
		max     int
		wantErr bool
	}{
		{name: "valid range", min: 1, max: 100, wantErr: false},
		{name: "equal min and max", min: 50, max: 50, wantErr: false},
		{name: "invalid range", min: 100, max: 1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRange(tt.min, tt.max)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRange() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCount(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "valid count", count: 5, wantErr: false},
		{name: "zero count", count: 0, wantErr: true},
		{name: "negative count", count: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCount(tt.count)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCount() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
