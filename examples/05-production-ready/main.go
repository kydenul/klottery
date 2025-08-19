package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/kydenul/lottery"
)

// 生产环境就绪的抽奖系统示例
func main() {
	fmt.Println("=== 生产环境就绪的抽奖系统示例 ===")

	// 1. 创建生产级 Redis 客户端配置
	redisClient := redis.NewClient(&redis.Options{
		Addr:         getRedisAddr(),
		Password:     getRedisPassword(),
		DB:           getRedisDB(),
		PoolSize:     20,               // 连接池大小
		MinIdleConns: 10,               // 最小空闲连接
		MaxRetries:   5,                // 最大重试次数
		DialTimeout:  10 * time.Second, // 连接超时
		ReadTimeout:  5 * time.Second,  // 读超时
		WriteTimeout: 5 * time.Second,  // 写超时
		PoolTimeout:  6 * time.Second,  // 连接池超时
	})
	defer redisClient.Close()

	// 测试 Redis 连接
	ctx := context.Background()
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}
	fmt.Println("✓ Redis 连接成功")

	// 2. 创建生产级配置管理器
	configManager := lottery.NewDefaultConfigManager()
	config := configManager.GetConfig()

	// 设置生产环境配置
	config.Engine.LockTimeout = 30 * time.Second
	config.Engine.RetryAttempts = 5
	config.Engine.RetryInterval = 200 * time.Millisecond
	config.Engine.LockCacheTTL = 2 * time.Second

	// 启用熔断器
	config.CircuitBreaker.Enabled = true
	config.CircuitBreaker.Name = "lottery-circuit-breaker"
	config.CircuitBreaker.MaxRequests = 100
	config.CircuitBreaker.Interval = 60 * time.Second
	config.CircuitBreaker.Timeout = 30 * time.Second
	config.CircuitBreaker.MinRequests = 10
	config.CircuitBreaker.FailureRatio = 0.5
	config.CircuitBreaker.OnStateChange = true

	fmt.Printf("✓ 配置加载成功，环境: %s\n", getEnvironment())

	// 3. 创建抽奖引擎
	engine := lottery.NewLotteryEngineWithConfig(redisClient, configManager)

	// 设置生产级日志记录器
	logger := &ProductionLogger{env: getEnvironment()}
	engine.SetLogger(logger)

	fmt.Println("✓ 抽奖引擎创建成功")

	// 4. 运行生产环境示例
	runProductionExamples(ctx, engine, config)

	// 5. 启动健康检查和监控
	startHealthCheck(ctx, engine, redisClient)

	// 6. 优雅关闭
	gracefulShutdown(redisClient, engine)
}

// runProductionExamples 运行生产环境示例
func runProductionExamples(ctx context.Context, engine *lottery.LotteryEngine, config *lottery.Config) {
	fmt.Println("\n--- 生产环境抽奖示例 ---")

	// 示例1: 带错误恢复的范围抽奖
	fmt.Println("\n1. 带错误恢复的范围抽奖")
	result, err := performWithRetry(ctx, func() (any, error) {
		return engine.DrawInRange(ctx, "prod:user:123", 1, 1000)
	}, 3)

	if err != nil {
		fmt.Printf("   ❌ 抽奖失败: %v\n", err)
	} else {
		fmt.Printf("   ✓ 抽奖成功: %d\n", result.(int))
	}

	// 示例2: 高并发奖品池抽奖
	fmt.Println("\n2. 高并发奖品池抽奖")
	prizes := []lottery.Prize{
		{ID: "legendary", Name: "传说奖品", Probability: 0.01, Value: 10000},
		{ID: "epic", Name: "史诗奖品", Probability: 0.05, Value: 5000},
		{ID: "rare", Name: "稀有奖品", Probability: 0.2, Value: 1000},
		{ID: "common", Name: "普通奖品", Probability: 0.74, Value: 100},
	}

	// 模拟高并发场景
	concurrentDraws := 10
	results := make(chan *lottery.Prize, concurrentDraws)
	errors := make(chan error, concurrentDraws)

	for i := range concurrentDraws {
		go func(index int) {
			prize, err := engine.DrawFromPrizes(ctx, fmt.Sprintf("prod:activity:concurrent_%d", index), prizes)
			if err != nil {
				errors <- err
			} else {
				results <- prize
			}
		}(i)
	}

	// 收集结果
	successCount := 0
	errorCount := 0
	for range concurrentDraws {
		select {
		case prize := <-results:
			successCount++
			fmt.Printf("   ✓ 并发抽奖 %d: %s (价值: %d)\n", successCount, prize.Name, prize.Value)
		case err := <-errors:
			errorCount++
			fmt.Printf("   ❌ 并发抽奖失败 %d: %v\n", errorCount, err)
		case <-time.After(10 * time.Second):
			fmt.Printf("   ⏰ 并发抽奖超时\n")
			return
		}
	}

	fmt.Printf("   📊 并发抽奖统计: 成功 %d, 失败 %d\n", successCount, errorCount)

	// 示例3: 带状态恢复的批量抽奖
	fmt.Println("\n3. 带状态恢复的批量抽奖")

	// 定义进度回调
	progressCallback := func(completed, total int, currentResult any) {
		if completed%5 == 0 || completed == total {
			progress := float64(completed) / float64(total) * 100
			fmt.Printf("   进度: %.1f%% (%d/%d)\n", progress, completed, total)
		}
	}

	batchResult, err := engine.DrawMultipleInRange(ctx, "prod:batch:recovery", 1, 100, 20, progressCallback)
	if err != nil {
		if batchResult != nil && batchResult.PartialSuccess {
			fmt.Printf("   ⚠️ 批量抽奖部分成功: 总数 %d, 成功 %d, 失败 %d\n",
				batchResult.TotalRequested, batchResult.Completed, batchResult.Failed)
		} else {
			fmt.Printf("   ❌ 批量抽奖失败: %v\n", err)
		}
	} else {
		fmt.Printf("   ✓ 批量抽奖完成: 总数 %d, 成功 %d\n",
			batchResult.TotalRequested, batchResult.Completed)

		if len(batchResult.Results) > 0 {
			fmt.Printf("   前5个结果: %v\n", batchResult.Results[:min(5, len(batchResult.Results))])
		}
	}

	// 示例4: 熔断器状态监控
	fmt.Println("\n4. 熔断器状态监控")
	state := engine.GetCircuitBreakerState()
	counts := engine.GetCircuitBreakerCounts()

	fmt.Printf("   熔断器状态: %s\n", state)
	fmt.Printf("   请求统计: 总数 %d, 成功 %d, 失败 %d\n",
		counts.Requests, counts.TotalSuccesses, counts.TotalFailures)

	if counts.Requests > 0 {
		successRate := float64(counts.TotalSuccesses) / float64(counts.Requests) * 100
		fmt.Printf("   成功率: %.2f%%\n", successRate)
	}

	// 示例5: 性能监控
	fmt.Println("\n5. 性能监控")
	metrics := engine.PerformanceMetrics()
	fmt.Printf("   总抽奖次数: %d\n", metrics.TotalDraws)
	fmt.Printf("   成功次数: %d\n", metrics.SuccessfulDraws)
	fmt.Printf("   失败次数: %d\n", metrics.FailedDraws)
	if metrics.TotalDraws > 0 {
		successRate := float64(metrics.SuccessfulDraws) / float64(metrics.TotalDraws) * 100
		fmt.Printf("   成功率: %.2f%%\n", successRate)
	}

	// 示例6: 配置信息展示
	fmt.Println("\n6. 当前配置信息")
	currentConfig := engine.GetConfig()
	fmt.Printf("   锁超时: %v\n", currentConfig.Engine.LockTimeout)
	fmt.Printf("   重试次数: %d\n", currentConfig.Engine.RetryAttempts)
	fmt.Printf("   重试间隔: %v\n", currentConfig.Engine.RetryInterval)
	fmt.Printf("   熔断器启用: %t\n", currentConfig.CircuitBreaker.Enabled)
}

// startHealthCheck 启动健康检查
func startHealthCheck(ctx context.Context, engine *lottery.LotteryEngine, redisClient *redis.Client) {
	fmt.Println("\n--- 健康检查 ---")

	// Redis 健康检查
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		fmt.Printf("❌ Redis 健康检查失败: %v\n", err)
	} else {
		fmt.Println("✓ Redis 健康检查通过")
	}

	// 熔断器健康检查
	healthCheck := engine.CircuitBreakerHealthCheck()
	if healthy, ok := healthCheck["healthy"].(bool); ok && healthy {
		fmt.Println("✓ 熔断器健康检查通过")
	} else {
		fmt.Printf("⚠️ 熔断器健康检查异常: %+v\n", healthCheck)
	}

	// 简单的功能测试
	testResult, err := engine.DrawInRange(ctx, "health_check", 1, 10)
	if err != nil {
		fmt.Printf("❌ 功能健康检查失败: %v\n", err)
	} else {
		fmt.Printf("✓ 功能健康检查通过: %d\n", testResult)
	}
}

// performWithRetry 执行重试逻辑
func performWithRetry(ctx context.Context, operation func() (any, error), maxRetries int) (any, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		result, err := operation()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if i < maxRetries-1 {
			// 指数退避
			backoff := time.Duration(i+1) * 100 * time.Millisecond
			time.Sleep(backoff)
		}
	}

	return nil, lastErr
}

// ProductionLogger 生产环境日志记录器
type ProductionLogger struct {
	env string
}

func (l *ProductionLogger) Info(msg string, args ...any) {
	log.Printf("[%s] [INFO] "+msg, append([]any{l.env}, args...)...)
}

func (l *ProductionLogger) Error(msg string, args ...any) {
	log.Printf("[%s] [ERROR] "+msg, append([]any{l.env}, args...)...)
}

func (l *ProductionLogger) Debug(msg string, args ...any) {
	// 生产环境可以选择性地记录调试日志
	if l.env == "development" {
		log.Printf("[%s] [DEBUG] "+msg, append([]any{l.env}, args...)...)
	}
}

// 环境配置获取函数
func getEnvironment() string {
	env := os.Getenv("LOTTERY_ENV")
	if env == "" {
		env = "development"
	}
	return env
}

func getRedisAddr() string {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	return addr
}

func getRedisPassword() string {
	return os.Getenv("REDIS_PASSWORD")
}

func getRedisDB() int {
	// 简化处理，实际生产环境可能需要更复杂的配置解析
	return 0
}

// gracefulShutdown 优雅关闭
func gracefulShutdown(redisClient *redis.Client, engine *lottery.LotteryEngine) {
	fmt.Println("\n--- 等待关闭信号 ---")

	// 创建信号通道
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 等待信号
	sig := <-sigChan
	fmt.Printf("\n收到信号: %v，开始优雅关闭...\n", sig)

	// 创建关闭超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 显示最终统计信息
	fmt.Println("\n--- 最终统计信息 ---")
	metrics := engine.PerformanceMetrics()
	fmt.Printf("总抽奖次数: %d\n", metrics.TotalDraws)
	fmt.Printf("成功次数: %d\n", metrics.SuccessfulDraws)
	fmt.Printf("失败次数: %d\n", metrics.FailedDraws)

	// 关闭 Redis 连接
	if err := redisClient.Close(); err != nil {
		fmt.Printf("关闭 Redis 连接失败: %v\n", err)
	} else {
		fmt.Println("✓ Redis 连接已关闭")
	}

	// 等待其他清理工作完成
	select {
	case <-ctx.Done():
		fmt.Println("⏰ 关闭超时")
	default:
		fmt.Println("✓ 优雅关闭完成")
	}
}
