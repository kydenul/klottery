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

	// 1. 初始化配置管理器
	configManager := lottery.NewConfigManager()

	// 加载配置
	config, err := configManager.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Printf("✓ 配置加载成功，环境: %s\n", getEnvironment())

	// 2. 创建 Redis 客户端
	redisClient := lottery.NewRedisClientFromConfig(config.Redis)
	defer redisClient.Close()

	// 测试 Redis 连接
	ctx := context.Background()
	_, err = redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Redis connection failed: %v", err)
	}
	fmt.Println("✓ Redis 连接成功")

	// 3. 创建抽奖引擎
	lotteryConfig, err := lottery.NewLotteryConfigFromConfig(config)
	if err != nil {
		log.Fatalf("Failed to create lottery config: %v", err)
	}
	engine := lottery.NewLotteryEngineWithConfig(redisClient, lotteryConfig)

	// 4. 创建带熔断器的引擎
	cbEngine := lottery.NewCircuitBreakerEngine(engine, config.CircuitBreaker, engine.GetLogger())
	fmt.Println("✓ 熔断器引擎创建成功")

	// 5. 创建错误处理器
	errorHandler := lottery.NewDefaultErrorHandler(engine.GetLogger())
	errorRecovery := lottery.NewErrorRecovery(errorHandler, config.Engine.RetryAttempts, engine.GetLogger())

	// 6. 设置配置热更新监听
	err = configManager.WatchConfig(func(newConfig *lottery.Config) {
		fmt.Printf("⚡ 配置已更新: %+v\n", newConfig)
		// 这里可以更新引擎配置
	})
	if err != nil {
		log.Printf("Failed to watch config: %v", err)
	}

	// 7. 运行示例
	runProductionExamples(ctx, cbEngine, errorRecovery, config)

	// 8. 优雅关闭
	gracefulShutdown(redisClient)
}

// runProductionExamples 运行生产环境示例
func runProductionExamples(ctx context.Context, engine lottery.LotteryDrawer, recovery *lottery.ErrorRecovery, config *lottery.Config) {
	fmt.Println("\n--- 生产环境抽奖示例 ---")

	// 示例1: 带错误恢复的范围抽奖
	fmt.Println("\n1. 带错误恢复的范围抽奖")
	err := recovery.ExecuteWithRetry(ctx, func() error {
		result, err := engine.DrawInRange(ctx, "prod:user:123", 1, 1000)
		if err != nil {
			return err
		}
		fmt.Printf("   抽奖结果: %d\n", result)
		return nil
	})
	if err != nil {
		fmt.Printf("   ❌ 抽奖失败: %v\n", err)
	} else {
		fmt.Println("   ✓ 抽奖成功")
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

	for i := 0; i < concurrentDraws; i++ {
		go func(index int) {
			err := recovery.ExecuteWithRetry(ctx, func() error {
				prize, err := engine.DrawFromPrizes(ctx, fmt.Sprintf("prod:activity:concurrent_%d", index), prizes)
				if err != nil {
					return err
				}
				results <- prize
				return nil
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// 收集结果
	successCount := 0
	errorCount := 0
	for i := 0; i < concurrentDraws; i++ {
		select {
		case prize := <-results:
			successCount++
			fmt.Printf("   ✓ 并发抽奖 %d: %s (价值: %d)\n", successCount, prize.Name, prize.Value)
		case err := <-errors:
			errorCount++
			fmt.Printf("   ❌ 并发抽奖失败 %d: %v\n", errorCount, err)
		case <-time.After(5 * time.Second):
			fmt.Printf("   ⏰ 并发抽奖超时\n")
			return
		}
	}

	fmt.Printf("   📊 并发抽奖统计: 成功 %d, 失败 %d\n", successCount, errorCount)

	// 示例3: 带状态恢复的批量抽奖
	fmt.Println("\n3. 带状态恢复的批量抽奖")
	err = recovery.ExecuteWithRetry(ctx, func() error {
		result, err := engine.DrawMultipleInRangeWithRecovery(ctx, "prod:batch:recovery", 1, 100, 20)
		if err != nil {
			return err
		}

		fmt.Printf("   批量抽奖完成: 总数 %d, 成功 %d, 失败 %d\n",
			result.TotalRequested, result.Completed, result.Failed)

		if len(result.Results) > 0 {
			fmt.Printf("   前5个结果: %v\n", result.Results[:min(5, len(result.Results))])
		}

		return nil
	})
	if err != nil {
		fmt.Printf("   ❌ 批量抽奖失败: %v\n", err)
	} else {
		fmt.Println("   ✓ 批量抽奖成功")
	}

	// 示例4: 熔断器状态监控
	fmt.Println("\n4. 熔断器状态监控")
	if cbEngine, ok := engine.(*lottery.CircuitBreakerEngine); ok {
		state := cbEngine.GetCircuitBreakerState()
		counts := cbEngine.GetCircuitBreakerCounts()

		fmt.Printf("   熔断器状态: %s\n", state)
		fmt.Printf("   请求统计: 总数 %d, 成功 %d, 失败 %d\n",
			counts.Requests, counts.TotalSuccesses, counts.TotalFailures)

		if counts.Requests > 0 {
			successRate := float64(counts.TotalSuccesses) / float64(counts.Requests) * 100
			fmt.Printf("   成功率: %.2f%%\n", successRate)
		}
	}

	// 示例5: 配置信息展示
	fmt.Println("\n5. 当前配置信息")
	fmt.Printf("   锁超时: %v\n", config.Engine.LockTimeout)
	fmt.Printf("   重试次数: %d\n", config.Engine.RetryAttempts)
	fmt.Printf("   重试间隔: %v\n", config.Engine.RetryInterval)
	fmt.Printf("   熔断器启用: %t\n", config.CircuitBreaker.Enabled)
}

// getEnvironment 获取当前环境
func getEnvironment() string {
	env := os.Getenv("LOTTERY_ENV")
	if env == "" {
		env = "development"
	}
	return env
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// gracefulShutdown 优雅关闭
func gracefulShutdown(redisClient *redis.Client) {
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
