package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/kydenul/lottery"
)

// 错误处理和最佳实践示例
func main() {
	fmt.Println("=== 线程安全抽奖系统 - 错误处理最佳实践 ===")

	// 初始化Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// 1. 参数验证最佳实践
	fmt.Println("\n--- 参数验证最佳实践 ---")
	parameterValidationExample(ctx, rdb)

	// 2. 超时和取消处理
	fmt.Println("\n--- 超时和取消处理 ---")
	timeoutAndCancellationExample(ctx, rdb)

	// 3. Redis连接错误处理
	fmt.Println("\n--- Redis连接错误处理 ---")
	redisConnectionErrorExample(ctx, rdb)

	// 4. 并发安全性验证
	fmt.Println("\n--- 并发安全性验证 ---")
	concurrencySafetyExample(ctx, rdb)

	// 5. 错误恢复策略
	fmt.Println("\n--- 错误恢复策略 ---")
	errorRecoveryStrategyExample(ctx, rdb)

	// 6. 生产环境最佳实践
	fmt.Println("\n--- 生产环境最佳实践 ---")
	productionBestPracticesExample(ctx, rdb)

	fmt.Println("\n=== 错误处理最佳实践演示完成 ===")
}

// 参数验证最佳实践
func parameterValidationExample(ctx context.Context, rdb *redis.Client) {
	engine := lottery.NewLotteryEngine(rdb)

	fmt.Println("测试各种无效参数:")

	// 测试无效范围
	_, err := engine.DrawInRange(ctx, "param_test", 100, 1) // max < min
	if err != nil {
		fmt.Printf("✓ 正确捕获无效范围错误: %v\n", err)
	}

	// 测试空锁键
	_, err = engine.DrawInRange(ctx, "", 1, 100)
	if err != nil {
		fmt.Printf("✓ 正确捕获空锁键错误: %v\n", err)
	}

	// 测试无效计数
	_, err = engine.DrawMultipleInRange(ctx, "param_test", 1, 100, 0)
	if err != nil {
		fmt.Printf("✓ 正确捕获无效计数错误: %v\n", err)
	}

	// 测试无效奖品池
	invalidPrizes := []lottery.Prize{
		{ID: "invalid", Name: "无效奖品", Probability: 1.5, Value: 100}, // 概率 > 1
	}
	_, err = engine.DrawFromPrizes(ctx, "param_test", invalidPrizes)
	if err != nil {
		fmt.Printf("✓ 正确捕获无效奖品池错误: %v\n", err)
	}

	// 测试概率总和不为1的奖品池
	invalidSumPrizes := []lottery.Prize{
		{ID: "prize1", Name: "奖品1", Probability: 0.3, Value: 100},
		{ID: "prize2", Name: "奖品2", Probability: 0.3, Value: 50}, // 总和 = 0.6 ≠ 1
	}
	_, err = engine.DrawFromPrizes(ctx, "param_test", invalidSumPrizes)
	if err != nil {
		fmt.Printf("✓ 正确捕获概率总和错误: %v\n", err)
	}
}

// 超时和取消处理
func timeoutAndCancellationExample(ctx context.Context, rdb *redis.Client) {
	engine := lottery.NewLotteryEngine(rdb)

	// 测试超时处理
	fmt.Println("测试超时处理:")
	timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	// 等待确保context超时
	time.Sleep(5 * time.Millisecond)

	result, err := engine.DrawMultipleInRangeWithRecovery(timeoutCtx, "timeout_test", 1, 100, 10)
	if err != nil {
		fmt.Printf("✓ 正确处理超时: %v\n", err)
		if result != nil && result.PartialSuccess {
			fmt.Printf("  部分完成: %d/%d\n", result.Completed, result.TotalRequested)
		}
	}

	// 测试取消处理
	fmt.Println("测试取消处理:")
	cancelCtx, cancelFunc := context.WithCancel(ctx)

	// 启动一个goroutine来取消context
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancelFunc()
	}()

	result, err = engine.DrawMultipleInRangeWithRecovery(cancelCtx, "cancel_test", 1, 100, 20)
	if err != nil {
		fmt.Printf("✓ 正确处理取消: %v\n", err)
		if result != nil && result.PartialSuccess {
			fmt.Printf("  部分完成: %d/%d\n", result.Completed, result.TotalRequested)
		}
	}
}

// Redis连接错误处理
func redisConnectionErrorExample(ctx context.Context, rdb *redis.Client) {
	// 创建一个连接到不存在Redis服务器的客户端
	badRdb := redis.NewClient(&redis.Options{
		Addr: "localhost:9999", // 不存在的端口
	})
	defer badRdb.Close()

	engine := lottery.NewLotteryEngine(badRdb)

	fmt.Println("测试Redis连接错误:")
	_, err := engine.DrawInRange(ctx, "connection_test", 1, 100)
	if err != nil {
		fmt.Printf("✓ 正确处理Redis连接错误: %v\n", err)
	}

	// 测试连接恢复
	fmt.Println("测试连接恢复:")
	goodEngine := lottery.NewLotteryEngine(rdb)
	result, err := goodEngine.DrawInRange(ctx, "recovery_test", 1, 100)
	if err != nil {
		fmt.Printf("❌ 连接恢复失败: %v\n", err)
	} else {
		fmt.Printf("✓ 连接恢复成功，抽奖结果: %d\n", result)
	}
}

// 并发安全性验证
func concurrencySafetyExample(ctx context.Context, rdb *redis.Client) {
	engine := lottery.NewLotteryEngine(rdb)

	fmt.Println("测试并发安全性 (10个并发抽奖):")

	// 创建通道来收集结果
	results := make(chan int, 10)
	errors := make(chan error, 10)

	// 启动10个并发抽奖
	for i := 0; i < 10; i++ {
		go func(id int) {
			lockKey := fmt.Sprintf("concurrent_test_%d", id)
			result, err := engine.DrawInRange(ctx, lockKey, 1, 100)
			if err != nil {
				errors <- err
			} else {
				results <- result
			}
		}(i)
	}

	// 收集结果
	successCount := 0
	errorCount := 0

	for i := 0; i < 10; i++ {
		select {
		case result := <-results:
			successCount++
			fmt.Printf("  并发抽奖 #%d 成功: %d\n", successCount, result)
		case err := <-errors:
			errorCount++
			fmt.Printf("  并发抽奖错误: %v\n", err)
		case <-time.After(5 * time.Second):
			fmt.Printf("  超时等待并发结果\n")
			return
		}
	}

	fmt.Printf("✓ 并发测试完成: 成功 %d 次, 失败 %d 次\n", successCount, errorCount)
}

// 错误恢复策略
func errorRecoveryStrategyExample(ctx context.Context, rdb *redis.Client) {
	engine := lottery.NewLotteryEngine(rdb)

	fmt.Println("演示错误恢复策略:")

	// 策略1: 重试机制
	fmt.Println("1. 重试机制演示:")
	retryResult := performWithRetry(ctx, engine, "retry_demo", 3)
	if retryResult {
		fmt.Println("✓ 重试成功")
	} else {
		fmt.Println("❌ 重试失败")
	}

	// 策略2: 降级处理
	fmt.Println("2. 降级处理演示:")
	fallbackResult := performWithFallback(ctx, engine, "fallback_demo")
	fmt.Printf("✓ 降级处理结果: %d\n", fallbackResult)

	// 策略3: 断路器模式
	fmt.Println("3. 断路器模式演示:")
	circuitBreakerDemo(ctx, engine)
}

// 生产环境最佳实践
func productionBestPracticesExample(ctx context.Context, rdb *redis.Client) {
	fmt.Println("生产环境最佳实践:")

	// 1. 使用连接池
	fmt.Println("1. Redis连接池配置:")
	productionRdb := redis.NewClient(&redis.Options{
		Addr:         "localhost:6379",
		PoolSize:     10,              // 连接池大小
		MinIdleConns: 5,               // 最小空闲连接
		MaxRetries:   3,               // 最大重试次数
		DialTimeout:  5 * time.Second, // 连接超时
		ReadTimeout:  3 * time.Second, // 读超时
		WriteTimeout: 3 * time.Second, // 写超时
		PoolTimeout:  4 * time.Second, // 连接池超时
	})
	defer productionRdb.Close()

	// 2. 使用生产级配置
	fmt.Println("2. 生产级抽奖引擎配置:")
	config, err := lottery.NewLotteryConfig(
		30*time.Second,       // 锁超时
		5,                    // 重试次数
		100*time.Millisecond, // 重试间隔
	)
	if err != nil {
		fmt.Printf("❌ 配置创建失败: %v\n", err)
		return
	}

	// 3. 使用静默日志记录器 (生产环境)
	silentLogger := lottery.NewSilentLogger()
	engine := lottery.NewLotteryEngineWithConfigAndLogger(productionRdb, config, silentLogger)

	// 4. 健康检查
	fmt.Println("3. 健康检查:")
	if healthCheck(ctx, productionRdb) {
		fmt.Println("✓ 系统健康")
	} else {
		fmt.Println("❌ 系统异常")
	}

	// 5. 监控和指标
	fmt.Println("4. 执行监控抽奖:")
	startTime := time.Now()
	result, err := engine.DrawInRange(ctx, "production_demo", 1, 1000)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ 抽奖失败: %v (耗时: %v)\n", err, duration)
	} else {
		fmt.Printf("✓ 抽奖成功: %d (耗时: %v)\n", result, duration)
	}

	fmt.Println("5. 生产环境建议:")
	fmt.Println("   - 设置适当的超时时间")
	fmt.Println("   - 配置连接池参数")
	fmt.Println("   - 实施监控和告警")
	fmt.Println("   - 定期备份Redis数据")
	fmt.Println("   - 使用Redis集群提高可用性")
}

// 重试机制实现
func performWithRetry(ctx context.Context, engine *lottery.LotteryEngine, lockKey string, maxRetries int) bool {
	for i := 0; i < maxRetries; i++ {
		_, err := engine.DrawInRange(ctx, lockKey, 1, 100)
		if err == nil {
			return true
		}
		fmt.Printf("  重试 %d/%d 失败: %v\n", i+1, maxRetries, err)
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// 降级处理实现
func performWithFallback(ctx context.Context, engine *lottery.LotteryEngine, lockKey string) int {
	result, err := engine.DrawInRange(ctx, lockKey, 1, 100)
	if err != nil {
		fmt.Printf("  主要方法失败: %v, 使用降级方案\n", err)
		// 降级到本地随机数生成
		return 42 // 固定降级值
	}
	return result
}

// 断路器模式演示
func circuitBreakerDemo(ctx context.Context, engine *lottery.LotteryEngine) {
	failureCount := 0
	threshold := 3

	for i := 0; i < 5; i++ {
		if failureCount >= threshold {
			fmt.Printf("  断路器开启，跳过请求 %d\n", i+1)
			continue
		}

		_, err := engine.DrawInRange(ctx, "circuit_test", 1, 100)
		if err != nil {
			failureCount++
			fmt.Printf("  请求 %d 失败 (失败计数: %d)\n", i+1, failureCount)
		} else {
			failureCount = 0 // 重置失败计数
			fmt.Printf("  请求 %d 成功\n", i+1)
		}
	}
}

// 健康检查实现
func healthCheck(ctx context.Context, rdb *redis.Client) bool {
	// 检查Redis连接
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Printf("Redis健康检查失败: %v", err)
		return false
	}

	// 检查Redis内存使用
	info, err := rdb.Info(ctx, "memory").Result()
	if err != nil {
		log.Printf("获取Redis内存信息失败: %v", err)
		return false
	}

	// 简单检查 (实际生产环境需要更详细的检查)
	if len(info) == 0 {
		return false
	}

	return true
}
