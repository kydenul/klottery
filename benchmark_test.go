package lottery

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBenchmarkRedisClient 创建用于基准测试的Redis客户端
func setupBenchmarkRedisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         "localhost:6379",
		DB:           2,  // 使用专门的基准测试数据库
		PoolSize:     20, // 增加连接池大小以支持高并发
		MinIdleConns: 5,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
}

// BenchmarkSingleDraw 单次抽奖性能基准测试
func BenchmarkSingleDraw(b *testing.B) {
	rdb := setupBenchmarkRedisClient()
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis不可用，跳过基准测试")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	ctx := context.Background()

	b.Run("范围抽奖", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; b.Loop(); i++ {
			lockKey := fmt.Sprintf("single_range_%d", i)
			_, err := engine.DrawInRange(ctx, lockKey, 1, 1000)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("奖品池抽奖", func(b *testing.B) {
		prizes := []Prize{
			{ID: "prize_1", Name: "一等奖", Probability: 0.01, Value: 1000},
			{ID: "prize_2", Name: "二等奖", Probability: 0.05, Value: 500},
			{ID: "prize_3", Name: "三等奖", Probability: 0.1, Value: 100},
			{ID: "prize_4", Name: "谢谢参与", Probability: 0.84, Value: 0},
		}

		b.ResetTimer()
		for i := 0; b.Loop(); i++ {
			lockKey := fmt.Sprintf("single_prize_%d", i)
			_, err := engine.DrawFromPrizes(ctx, lockKey, prizes)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkMultipleDraw 连续抽奖性能基准测试
func BenchmarkMultipleDraw(b *testing.B) {
	rdb := setupBenchmarkRedisClient()
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis不可用，跳过基准测试")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	ctx := context.Background()

	testCases := []struct {
		name  string
		count int
	}{
		{"小批量_10次", 10},
		{"中批量_50次", 50},
		{"大批量_100次", 100},
		{"超大批量_500次", 500},
	}

	for _, tc := range testCases {
		b.Run(fmt.Sprintf("范围抽奖_%s", tc.name), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				lockKey := fmt.Sprintf("multi_range_%s_%d", tc.name, i)
				_, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 1000, tc.count)
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("奖品池抽奖_%s", tc.name), func(b *testing.B) {
			prizes := []Prize{
				{ID: "prize_1", Name: "一等奖", Probability: 0.01, Value: 1000},
				{ID: "prize_2", Name: "二等奖", Probability: 0.05, Value: 500},
				{ID: "prize_3", Name: "三等奖", Probability: 0.1, Value: 100},
				{ID: "prize_4", Name: "谢谢参与", Probability: 0.84, Value: 0},
			}

			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				lockKey := fmt.Sprintf("multi_prize_%s_%d", tc.name, i)
				_, err := engine.DrawMultipleFromPrizes(ctx, lockKey, prizes, tc.count)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkConcurrentDraw 高并发抽奖性能基准测试
func BenchmarkConcurrentDraw(b *testing.B) {
	rdb := setupBenchmarkRedisClient()
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis不可用，跳过基准测试")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	concurrencyLevels := []int{10, 50, 100, 200}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("并发_%d协程_范围抽奖", concurrency), func(b *testing.B) {
			b.SetParallelism(concurrency)
			b.RunParallel(func(pb *testing.PB) {
				ctx := context.Background()
				i := 0
				for pb.Next() {
					lockKey := fmt.Sprintf("concurrent_range_%d_%d", concurrency, i)
					_, err := engine.DrawInRange(ctx, lockKey, 1, 1000)
					if err != nil && err != ErrLockAcquisitionFailed {
						// 只有非锁获取失败的错误才报告，锁获取失败是并发测试的正常现象
						b.Error(err)
					}
					i++
				}
			})
		})

		b.Run(fmt.Sprintf("并发_%d协程_奖品池抽奖", concurrency), func(b *testing.B) {
			prizes := []Prize{
				{ID: "prize_1", Name: "一等奖", Probability: 0.01, Value: 1000},
				{ID: "prize_2", Name: "二等奖", Probability: 0.05, Value: 500},
				{ID: "prize_3", Name: "三等奖", Probability: 0.1, Value: 100},
				{ID: "prize_4", Name: "谢谢参与", Probability: 0.84, Value: 0},
			}

			b.SetParallelism(concurrency)
			b.RunParallel(func(pb *testing.PB) {
				ctx := context.Background()
				i := 0
				for pb.Next() {
					lockKey := fmt.Sprintf("concurrent_prize_%d_%d", concurrency, i)
					_, err := engine.DrawFromPrizes(ctx, lockKey, prizes)
					if err != nil && err != ErrLockAcquisitionFailed {
						// 只有非锁获取失败的错误才报告，锁获取失败是并发测试的正常现象
						b.Error(err)
					}
					i++
				}
			})
		})
	}
}

// BenchmarkOptimizedVsStandard 优化版本与标准版本性能对比
func BenchmarkOptimizedVsStandard(b *testing.B) {
	rdb := setupBenchmarkRedisClient()
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis不可用，跳过基准测试")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	ctx := context.Background()

	drawCounts := []int{10, 50, 100, 200}

	for _, count := range drawCounts {
		b.Run(fmt.Sprintf("标准版本_%d次抽奖", count), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				lockKey := fmt.Sprintf("standard_%d_%d", count, i)
				_, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 1000, count)
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("优化版本_%d次抽奖", count), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				lockKey := fmt.Sprintf("optimized_%d_%d", count, i)
				_, err := engine.DrawMultipleInRangeOptimized(ctx, lockKey, 1, 1000, count, nil)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkLockPerformance 分布式锁性能基准测试
func BenchmarkLockPerformance(b *testing.B) {
	rdb := setupBenchmarkRedisClient()
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis不可用，跳过基准测试")
	}

	lockManager := NewLockManager(rdb, 30*time.Second)
	ctx := context.Background()

	b.Run("锁获取和释放", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; b.Loop(); i++ {
			lockKey := fmt.Sprintf("lock_perf_%d", i)
			lockValue := generateLockValue()

			// 获取锁
			acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, 30*time.Second)
			if err != nil {
				b.Fatal(err)
			}
			if !acquired {
				b.Fatal("获取锁失败")
			}

			// 释放锁
			released, err := lockManager.ReleaseLock(ctx, lockKey, lockValue)
			if err != nil {
				b.Fatal(err)
			}
			if !released {
				b.Fatal("释放锁失败")
			}
		}
	})

	b.Run("并发锁竞争", func(b *testing.B) {
		const goroutines = 10
		lockKey := "concurrent_lock_test"

		b.ResetTimer()
		for b.Loop() {
			var wg sync.WaitGroup

			for j := range goroutines {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					lockValue := generateLockValue()

					acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, 1*time.Second)
					if err == nil && acquired {
						// 模拟一些工作
						time.Sleep(1 * time.Millisecond)
						lockManager.ReleaseLock(ctx, lockKey, lockValue)
					}
				}(j)
			}

			wg.Wait()
		}
	})
}

// BenchmarkMemoryUsage 内存使用性能基准测试
func BenchmarkMemoryUsage(b *testing.B) {
	rdb := setupBenchmarkRedisClient()
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis不可用，跳过基准测试")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())
	ctx := context.Background()

	b.Run("大批量抽奖内存使用", func(b *testing.B) {
		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		b.ResetTimer()
		for i := 0; b.Loop(); i++ {
			lockKey := fmt.Sprintf("memory_test_%d", i)
			_, err := engine.DrawMultipleInRange(ctx, lockKey, 1, 10000, 1000)
			if err != nil {
				b.Fatal(err)
			}
		}

		runtime.GC()
		runtime.ReadMemStats(&m2)

		b.ReportMetric(float64(m2.Alloc-m1.Alloc)/float64(b.N), "bytes/op")
		b.ReportMetric(float64(m2.Mallocs-m1.Mallocs)/float64(b.N), "allocs/op")
	})
}

// TestConcurrentPerformance 并发性能测试（非基准测试，用于验证并发正确性）
func TestConcurrentPerformance(t *testing.T) {
	rdb := setupBenchmarkRedisClient()
	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过并发测试")
	}

	engine := NewLotteryEngineWithLogger(rdb, NewSilentLogger())

	t.Run("高并发范围抽奖正确性", func(t *testing.T) {
		const (
			goroutines        = 100
			drawsPerGoroutine = 10
		)

		var wg sync.WaitGroup
		results := make(chan int, goroutines*drawsPerGoroutine)
		errors := make(chan error, goroutines*drawsPerGoroutine)

		startTime := time.Now()

		for i := range goroutines {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ctx := context.Background()

				for j := range drawsPerGoroutine {
					lockKey := fmt.Sprintf("concurrent_test_%d_%d", id, j)
					result, err := engine.DrawInRange(ctx, lockKey, 1, 1000)
					if err != nil {
						errors <- err
					} else {
						results <- result
					}
				}
			}(i)
		}

		wg.Wait()
		close(results)
		close(errors)

		duration := time.Since(startTime)

		// 统计结果
		var successCount, errorCount int
		for result := range results {
			assert.GreaterOrEqual(t, result, 1)
			assert.LessOrEqual(t, result, 1000)
			successCount++
		}

		for err := range errors {
			t.Logf("并发抽奖错误: %v", err)
			errorCount++
		}

		totalOperations := goroutines * drawsPerGoroutine
		successRate := float64(successCount) / float64(totalOperations) * 100
		throughput := float64(successCount) / duration.Seconds()

		t.Logf("并发性能统计:")
		t.Logf("  总操作数: %d", totalOperations)
		t.Logf("  成功数: %d", successCount)
		t.Logf("  失败数: %d", errorCount)
		t.Logf("  成功率: %.2f%%", successRate)
		t.Logf("  总耗时: %v", duration)
		t.Logf("  吞吐量: %.2f ops/sec", throughput)

		// 验证至少有90%的成功率
		assert.GreaterOrEqual(t, successRate, 90.0, "并发抽奖成功率应该至少为90%")
	})

	t.Run("高并发奖品池抽奖正确性", func(t *testing.T) {
		prizes := []Prize{
			{ID: "prize_1", Name: "一等奖", Probability: 0.01, Value: 1000},
			{ID: "prize_2", Name: "二等奖", Probability: 0.05, Value: 500},
			{ID: "prize_3", Name: "三等奖", Probability: 0.1, Value: 100},
			{ID: "prize_4", Name: "谢谢参与", Probability: 0.84, Value: 0},
		}

		const (
			goroutines        = 50
			drawsPerGoroutine = 20
		)

		var wg sync.WaitGroup
		results := make(chan *Prize, goroutines*drawsPerGoroutine)
		errors := make(chan error, goroutines*drawsPerGoroutine)

		startTime := time.Now()

		for i := range goroutines {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ctx := context.Background()

				for j := range drawsPerGoroutine {
					lockKey := fmt.Sprintf("concurrent_prize_test_%d_%d", id, j)
					result, err := engine.DrawFromPrizes(ctx, lockKey, prizes)
					if err != nil {
						errors <- err
					} else {
						results <- result
					}
				}
			}(i)
		}

		wg.Wait()
		close(results)
		close(errors)

		duration := time.Since(startTime)

		// 统计结果
		var successCount, errorCount int
		prizeStats := make(map[string]int)

		for result := range results {
			assert.Contains(t, []string{"prize_1", "prize_2", "prize_3", "prize_4"}, result.ID)
			prizeStats[result.ID]++
			successCount++
		}

		for err := range errors {
			t.Logf("并发奖品抽奖错误: %v", err)
			errorCount++
		}

		totalOperations := goroutines * drawsPerGoroutine
		successRate := float64(successCount) / float64(totalOperations) * 100
		throughput := float64(successCount) / duration.Seconds()

		t.Logf("并发奖品抽奖性能统计:")
		t.Logf("  总操作数: %d", totalOperations)
		t.Logf("  成功数: %d", successCount)
		t.Logf("  失败数: %d", errorCount)
		t.Logf("  成功率: %.2f%%", successRate)
		t.Logf("  总耗时: %v", duration)
		t.Logf("  吞吐量: %.2f ops/sec", throughput)
		t.Logf("  奖品分布: %+v", prizeStats)

		// 验证至少有90%的成功率
		assert.GreaterOrEqual(t, successRate, 90.0, "并发奖品抽奖成功率应该至少为90%")

		// 验证奖品分布合理性（谢谢参与应该是最多的）
		if successCount > 0 {
			assert.Greater(t, prizeStats["prize_4"], prizeStats["prize_1"], "谢谢参与应该比一等奖更多")
		}
	})
}

// ===================================================================

func TestPerformanceMetrics(t *testing.T) {
	metrics := &PerformanceMetrics{}
	metrics.Reset()

	t.Run("初始状态", func(t *testing.T) {
		assert.Equal(t, int64(0), metrics.TotalDraws)
		assert.Equal(t, int64(0), metrics.SuccessfulDraws)
		assert.Equal(t, int64(0), metrics.FailedDraws)
		assert.Equal(t, 0.0, metrics.GetSuccessRate())
		assert.Equal(t, time.Duration(0), metrics.GetAverageLockTime())
		assert.Equal(t, 0.0, metrics.GetThroughput())
	})

	t.Run("成功率计算", func(t *testing.T) {
		metrics.Reset()
		metrics.TotalDraws = 100
		metrics.SuccessfulDraws = 85
		metrics.FailedDraws = 15

		assert.Equal(t, 85.0, metrics.GetSuccessRate())
	})

	t.Run("平均锁时间计算", func(t *testing.T) {
		metrics.Reset()
		metrics.LockAcquisitions = 10
		metrics.LockAcquisitionTime = int64(10 * time.Millisecond)

		expected := time.Duration(int64(10*time.Millisecond) / 10)
		assert.Equal(t, expected, metrics.GetAverageLockTime())
	})
}

func TestPerformanceMonitor(t *testing.T) {
	monitor := NewPerformanceMonitor()

	t.Run("启用和禁用", func(t *testing.T) {
		assert.True(t, monitor.IsEnabled())

		monitor.Disable()
		assert.False(t, monitor.IsEnabled())

		monitor.Enable()
		assert.True(t, monitor.IsEnabled())
	})

	t.Run("记录抽奖操作", func(t *testing.T) {
		monitor.ResetMetrics()

		// 记录成功的抽奖
		monitor.RecordDraw(true, 100*time.Millisecond)
		monitor.RecordDraw(true, 200*time.Millisecond)
		monitor.RecordDraw(false, 50*time.Millisecond)

		metrics := monitor.GetMetrics()
		assert.Equal(t, int64(3), metrics.TotalDraws)
		assert.Equal(t, int64(2), metrics.SuccessfulDraws)
		assert.Equal(t, int64(1), metrics.FailedDraws)
		assert.Greater(t, metrics.AverageDrawTime, int64(0))
	})

	t.Run("记录锁操作", func(t *testing.T) {
		monitor.ResetMetrics()

		// 记录锁获取
		monitor.RecordLockAcquisition(true, 10*time.Millisecond)
		monitor.RecordLockAcquisition(true, 20*time.Millisecond)
		monitor.RecordLockAcquisition(false, 5*time.Millisecond)

		// 记录锁释放
		monitor.RecordLockRelease()
		monitor.RecordLockRelease()

		metrics := monitor.GetMetrics()
		assert.Equal(t, int64(2), metrics.LockAcquisitions)
		assert.Equal(t, int64(1), metrics.LockFailures)
		assert.Equal(t, int64(2), metrics.LockReleases)
		assert.Greater(t, metrics.LockAcquisitionTime, int64(0))
	})

	t.Run("禁用时不记录", func(t *testing.T) {
		monitor.ResetMetrics()
		monitor.Disable()

		monitor.RecordDraw(true, 100*time.Millisecond)
		monitor.RecordLockAcquisition(true, 10*time.Millisecond)

		metrics := monitor.GetMetrics()
		assert.Equal(t, int64(0), metrics.TotalDraws)
		assert.Equal(t, int64(0), metrics.LockAcquisitions)
	})
}

func TestOptimizedRedisConfig(t *testing.T) {
	t.Run("默认配置", func(t *testing.T) {
		config := DefaultRedisConfig()

		assert.Equal(t, 50, config.PoolSize)
		assert.Equal(t, 10, config.MinIdleConns)
		assert.Equal(t, 3, config.MaxRetries)
		assert.Equal(t, 5*time.Second, config.DialTimeout)
		assert.Equal(t, 3*time.Second, config.ReadTimeout)
		assert.Equal(t, 3*time.Second, config.WriteTimeout)
	})

	t.Run("创建优化的Redis客户端", func(t *testing.T) {
		config := DefaultRedisConfig()
		config.Addr = "localhost:6379"
		client := NewRedisClientFromConfig(config)

		assert.NotNil(t, client)
		defer client.Close()
	})
}

func TestEnhancedLotteryEngine(t *testing.T) {
	rdb := NewRedisClientFromConfig(DefaultRedisConfig())
	rdb.Options().DB = 3 // 使用测试数据库

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过测试")
	}

	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	config := NewDefaultConfigManager()
	engine := NewLotteryEngineWithConfig(rdb, config)

	t.Run("带监控的范围抽奖", func(t *testing.T) {
		ctx := context.Background()

		result, err := engine.DrawInRangeWithMonitoring(ctx, "enhanced_range_test", 1, 100)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 1)
		assert.LessOrEqual(t, result, 100)

		// 检查性能指标
		metrics := engine.GetPerformanceMetrics()
		assert.Equal(t, int64(1), metrics.TotalDraws)
		assert.Equal(t, int64(1), metrics.SuccessfulDraws)
		assert.Equal(t, int64(0), metrics.FailedDraws)
		assert.Greater(t, metrics.AverageDrawTime, int64(0))
	})

	t.Run("优化的范围抽奖", func(t *testing.T) {
		ctx := context.Background()

		result, err := engine.DrawInRangeOptimized(ctx, "optimized_range_test", 1, 100)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result, 1)
		assert.LessOrEqual(t, result, 100)

		// 检查性能指标
		metrics := engine.GetPerformanceMetrics()
		assert.Greater(t, metrics.TotalDraws, int64(0))
		assert.Greater(t, metrics.SuccessfulDraws, int64(0))
	})

	t.Run("锁缓存功能", func(t *testing.T) {
		ctx := context.Background()
		lockKey := "cache_test_lock"

		// 先手动获取锁，模拟锁被占用的情况
		lockManager := NewLockManager(rdb, 30*time.Second)
		lockValue := generateLockValue()
		acquired, err := lockManager.AcquireLock(ctx, lockKey, lockValue, 30*time.Second)
		require.NoError(t, err)
		require.True(t, acquired)

		// 现在尝试抽奖应该失败，并且会被缓存
		start := time.Now()
		_, err1 := engine.DrawInRangeOptimized(ctx, lockKey, 1, 10)
		duration1 := time.Since(start)
		assert.Equal(t, ErrLockAcquisitionFailed, err1)

		// 立即再次尝试相同的锁键应该快速失败（由于锁缓存）
		start = time.Now()
		_, err2 := engine.DrawInRangeOptimized(ctx, lockKey, 1, 10)
		duration2 := time.Since(start)

		assert.Equal(t, ErrLockAcquisitionFailed, err2)
		assert.Less(t, duration2, duration1/2) // 第二次应该更快（使用缓存）

		// 释放手动获取的锁
		released, err := lockManager.ReleaseLock(ctx, lockKey, lockValue)
		require.NoError(t, err)
		require.True(t, released)
	})
}

func TestEnhancedBatchLockManager(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   3, // 使用测试数据库
	})

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过测试")
	}

	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	batchManager := NewBatchLockManager(rdb, 30*time.Second, 3, 100*time.Millisecond, 1*time.Second)

	t.Run("批量获取和释放锁", func(t *testing.T) {
		ctx := context.Background()
		lockKeys := []string{"batch_lock_1", "batch_lock_2", "batch_lock_3"}
		lockValues := []string{generateLockValue(), generateLockValue(), generateLockValue()}

		// 批量获取锁
		acquired, err := batchManager.AcquireMultipleLocksWithMonitoring(ctx, lockKeys, lockValues, 30*time.Second)
		require.NoError(t, err)
		assert.Len(t, acquired, 3)

		// 所有锁都应该成功获取
		for i, result := range acquired {
			assert.True(t, result, "锁 %d 应该成功获取", i)
		}

		// 批量释放锁
		released, err := batchManager.ReleaseMultipleLocksWithMonitoring(ctx, lockKeys, lockValues)
		require.NoError(t, err)
		assert.Len(t, released, 3)

		// 所有锁都应该成功释放
		for i, result := range released {
			assert.True(t, result, "锁 %d 应该成功释放", i)
		}

		// 检查性能指标
		metrics := batchManager.GetPerformanceMetrics()
		assert.Equal(t, int64(3), metrics.LockAcquisitions)
		assert.Equal(t, int64(3), metrics.LockReleases)
	})

	t.Run("空锁列表", func(t *testing.T) {
		ctx := context.Background()
		lockKeys := []string{}
		lockValues := []string{}

		acquired, err := batchManager.AcquireMultipleLocksWithMonitoring(ctx, lockKeys, lockValues, 30*time.Second)
		require.NoError(t, err)
		assert.Len(t, acquired, 0)

		released, err := batchManager.ReleaseMultipleLocksWithMonitoring(ctx, lockKeys, lockValues)
		require.NoError(t, err)
		assert.Len(t, released, 0)
	})

	t.Run("参数不匹配", func(t *testing.T) {
		ctx := context.Background()
		lockKeys := []string{"key1", "key2"}
		lockValues := []string{"value1"} // 数量不匹配

		acquired, err := batchManager.AcquireMultipleLocksWithMonitoring(ctx, lockKeys, lockValues, 30*time.Second)
		assert.Equal(t, ErrInvalidParameters, err)
		assert.Nil(t, acquired)

		released, err := batchManager.ReleaseMultipleLocksWithMonitoring(ctx, lockKeys, lockValues)
		assert.Equal(t, ErrInvalidParameters, err)
		assert.Nil(t, released)
	})
}

func TestPerformanceMonitoringControl(t *testing.T) {
	rdb := NewRedisClientFromConfig(DefaultRedisConfig())
	rdb.Options().DB = 3 // 使用测试数据库

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		t.Skip("Redis不可用，跳过测试")
	}

	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	config := NewDefaultConfigManager()
	engine := NewLotteryEngineWithConfig(rdb, config)

	t.Run("性能监控控制", func(t *testing.T) {
		// 重置指标
		engine.ResetPerformanceMetrics()
		metrics := engine.GetPerformanceMetrics()
		assert.Equal(t, int64(0), metrics.TotalDraws)

		// 禁用监控
		engine.DisablePerformanceMonitoring()

		ctx := context.Background()
		_, err := engine.DrawInRangeOptimized(ctx, "monitoring_test", 1, 10)
		require.NoError(t, err)

		// 应该没有记录指标
		metrics = engine.GetPerformanceMetrics()
		assert.Equal(t, int64(0), metrics.TotalDraws)

		// 重新启用监控
		engine.EnablePerformanceMonitoring()

		_, err = engine.DrawInRangeOptimized(ctx, "monitoring_test_2", 1, 10)
		require.NoError(t, err)

		// 现在应该有记录
		metrics = engine.GetPerformanceMetrics()
		assert.Greater(t, metrics.TotalDraws, int64(0))
	})

	t.Run("奖品池抽奖监控", func(t *testing.T) {
		ctx := context.Background()
		prizes := []Prize{
			{ID: "prize_1", Name: "一等奖", Probability: 0.1, Value: 1000},
			{ID: "prize_2", Name: "二等奖", Probability: 0.2, Value: 500},
			{ID: "prize_3", Name: "三等奖", Probability: 0.7, Value: 100},
		}

		prize, err := engine.DrawFromPrizesOptimized(ctx, "optimized_prize_test", prizes)
		require.NoError(t, err)
		assert.NotNil(t, prize)
		assert.Contains(t, []string{"prize_1", "prize_2", "prize_3"}, prize.ID)

		// 检查性能指标
		metrics := engine.GetPerformanceMetrics()
		assert.Greater(t, metrics.TotalDraws, int64(0))
		assert.Greater(t, metrics.SuccessfulDraws, int64(0))
	})

	t.Run("参数验证", func(t *testing.T) {
		ctx := context.Background()

		// 无效范围
		_, err := engine.DrawInRangeOptimized(ctx, "invalid_range", 100, 1)
		assert.Equal(t, ErrInvalidRange, err)

		// 空锁键
		_, err = engine.DrawInRangeOptimized(ctx, "", 1, 100)
		assert.Equal(t, ErrInvalidParameters, err)

		// 无效奖品池
		_, err = engine.DrawFromPrizesOptimized(ctx, "invalid_prizes", []Prize{})
		assert.Equal(t, ErrEmptyPrizePool, err)
	})
}

// BenchmarkOptimizedVsStandardPerformance 优化版本与标准版本的性能对比基准测试
func BenchmarkOptimizedVsStandardPerformance(b *testing.B) {
	rdb := NewRedisClientFromConfig(DefaultRedisConfig())
	rdb.Options().DB = 3 // 使用测试数据库

	// 测试Redis连接
	_, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		b.Skip("Redis不可用，跳过基准测试")
	}

	defer func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
	}()

	config := NewDefaultConfigManager()
	standardEngine := NewLotteryEngineWithConfigAndLogger(rdb, config, NewSilentLogger())
	optimizedEngine := NewLotteryEngineWithConfig(rdb, config)

	b.Run("标准引擎_范围抽奖", func(b *testing.B) {
		ctx := context.Background()
		b.ResetTimer()
		for b.Loop() {
			lockKey := "standard_range_" + generateLockValue()
			_, err := standardEngine.DrawInRange(ctx, lockKey, 1, 1000)
			if err != nil && err != ErrLockAcquisitionFailed {
				b.Fatal(err)
			}
		}
	})

	b.Run("增强引擎_范围抽奖", func(b *testing.B) {
		ctx := context.Background()
		b.ResetTimer()
		for b.Loop() {
			lockKey := "enhanced_range_" + generateLockValue()
			_, err := optimizedEngine.DrawInRangeOptimized(ctx, lockKey, 1, 1000)
			if err != nil && err != ErrLockAcquisitionFailed {
				b.Fatal(err)
			}
		}
	})

	b.Run("标准引擎_奖品池抽奖", func(b *testing.B) {
		prizes := []Prize{
			{ID: "prize_1", Name: "一等奖", Probability: 0.01, Value: 1000},
			{ID: "prize_2", Name: "二等奖", Probability: 0.05, Value: 500},
			{ID: "prize_3", Name: "三等奖", Probability: 0.1, Value: 100},
			{ID: "prize_4", Name: "谢谢参与", Probability: 0.84, Value: 0},
		}

		ctx := context.Background()
		b.ResetTimer()
		for b.Loop() {
			lockKey := "standard_prize_" + generateLockValue()
			_, err := standardEngine.DrawFromPrizes(ctx, lockKey, prizes)
			if err != nil && err != ErrLockAcquisitionFailed {
				b.Fatal(err)
			}
		}
	})

	b.Run("增强引擎_奖品池抽奖", func(b *testing.B) {
		prizes := []Prize{
			{ID: "prize_1", Name: "一等奖", Probability: 0.01, Value: 1000},
			{ID: "prize_2", Name: "二等奖", Probability: 0.05, Value: 500},
			{ID: "prize_3", Name: "三等奖", Probability: 0.1, Value: 100},
			{ID: "prize_4", Name: "谢谢参与", Probability: 0.84, Value: 0},
		}

		ctx := context.Background()
		b.ResetTimer()
		for b.Loop() {
			lockKey := "enhanced_prize_" + generateLockValue()
			_, err := optimizedEngine.DrawFromPrizesOptimized(ctx, lockKey, prizes)
			if err != nil && err != ErrLockAcquisitionFailed {
				b.Fatal(err)
			}
		}
	})
}
