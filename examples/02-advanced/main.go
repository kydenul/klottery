package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/kydenul/lottery"
)

// 高级功能使用示例
func main() {
	fmt.Println("=== 线程安全抽奖系统 - 高级功能示例 ===")

	// 初始化Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	ctx := context.Background()

	// 1. 自定义配置示例
	fmt.Println("\n--- 自定义配置示例 ---")
	customConfigExample(ctx, rdb)

	// 2. 错误恢复功能示例
	fmt.Println("\n--- 错误恢复功能示例 ---")
	errorRecoveryExample(ctx, rdb)

	// 3. 性能优化功能示例
	fmt.Println("\n--- 性能优化功能示例 ---")
	performanceOptimizationExample(ctx, rdb)

	// 4. 自定义日志记录示例
	fmt.Println("\n--- 自定义日志记录示例 ---")
	customLoggerExample(ctx, rdb)

	// 5. 运行时配置更新示例
	fmt.Println("\n--- 运行时配置更新示例 ---")
	runtimeConfigExample(ctx, rdb)

	fmt.Println("\n=== 高级功能演示完成 ===")
}

// 自定义配置示例
func customConfigExample(ctx context.Context, rdb *redis.Client) {
	// 创建自定义配置
	config, err := lottery.NewLotteryConfig(
		10*time.Second,       // 锁超时时间
		5,                    // 重试次数
		200*time.Millisecond, // 重试间隔
	)
	if err != nil {
		fmt.Printf("❌ 配置创建失败: %v\n", err)
		return
	}

	// 使用自定义配置创建引擎
	engine := lottery.NewLotteryEngineWithConfig(rdb, config)
	fmt.Printf("✓ 自定义配置引擎创建成功 (锁超时: %v, 重试: %d次)\n",
		config.LockTimeout, config.RetryAttempts)

	// 测试抽奖
	result, err := engine.DrawInRange(ctx, "custom_config_demo", 1, 100)
	if err != nil {
		fmt.Printf("❌ 抽奖失败: %v\n", err)
		return
	}
	fmt.Printf("✓ 抽奖结果: %d\n", result)
}

// 错误恢复功能示例
func errorRecoveryExample(ctx context.Context, rdb *redis.Client) {
	engine := lottery.NewLotteryEngine(rdb)

	// 使用带错误恢复的连抽功能
	result, err := engine.DrawMultipleInRangeWithRecovery(ctx, "recovery_demo", 1, 50, 10)

	if err != nil {
		if result != nil && result.PartialSuccess {
			fmt.Printf("⚠️  部分成功: 完成 %d/%d 次抽奖 (成功率: %.1f%%)\n",
				result.Completed, result.TotalRequested, result.SuccessRate())
			fmt.Printf("   成功结果: %v\n", result.Results)

			// 显示错误详情
			if len(result.ErrorDetails) > 0 {
				fmt.Printf("   错误详情:\n")
				for _, errDetail := range result.ErrorDetails {
					fmt.Printf("     抽奖 #%d: %v\n", errDetail.DrawIndex, errDetail.Error)
				}
			}
		} else {
			fmt.Printf("❌ 抽奖完全失败: %v\n", err)
		}
	} else {
		fmt.Printf("✓ 全部成功: %v\n", result.Results)
	}

	// 奖品池错误恢复示例
	prizes := []lottery.Prize{
		{ID: "rare", Name: "稀有奖品", Probability: 0.1, Value: 500},
		{ID: "common", Name: "普通奖品", Probability: 0.6, Value: 50},
		{ID: "nothing", Name: "谢谢参与", Probability: 0.3, Value: 0},
	}

	prizeResult, err := engine.DrawMultipleFromPrizesWithRecovery(ctx, "prize_recovery_demo", prizes, 8)
	if err != nil {
		if prizeResult != nil && prizeResult.PartialSuccess {
			fmt.Printf("⚠️  奖品抽奖部分成功: %d/%d 次\n",
				prizeResult.Completed, prizeResult.TotalRequested)
			for i, prize := range prizeResult.PrizeResults {
				fmt.Printf("   第%d次: %s (价值: %d)\n", i+1, prize.Name, prize.Value)
			}
		}
	} else {
		fmt.Printf("✓ 奖品抽奖全部成功: %d次\n", len(prizeResult.PrizeResults))
	}
}

// 性能优化功能示例
func performanceOptimizationExample(ctx context.Context, rdb *redis.Client) {
	engine := lottery.NewLotteryEngine(rdb)

	// 定义进度回调函数
	progressCallback := func(completed, total int, currentResult interface{}) {
		progress := float64(completed) / float64(total) * 100
		fmt.Printf("   进度: %.1f%% (%d/%d) - 当前结果: %v\n",
			progress, completed, total, currentResult)
	}

	fmt.Println("开始优化连抽 (带进度显示):")

	// 使用优化的连抽功能
	result, err := engine.DrawMultipleInRangeOptimized(
		ctx, "optimization_demo", 1, 100, 15, progressCallback)
	if err != nil {
		fmt.Printf("❌ 优化连抽失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 优化连抽完成: %v\n", result.Results)
	fmt.Printf("  总计: %d次, 成功: %d次, 失败: %d次\n",
		result.TotalRequested, result.Completed, result.Failed)
}

// 自定义日志记录示例
func customLoggerExample(ctx context.Context, rdb *redis.Client) {
	// 创建自定义日志记录器
	logger := &CustomLogger{prefix: "[抽奖系统]"}

	// 使用自定义日志记录器创建引擎
	engine := lottery.NewLotteryEngineWithLogger(rdb, logger)

	fmt.Println("使用自定义日志记录器:")

	// 执行抽奖操作 (会产生日志输出)
	result, err := engine.DrawInRange(ctx, "custom_logger_demo", 1, 10)
	if err != nil {
		fmt.Printf("❌ 抽奖失败: %v\n", err)
		return
	}
	fmt.Printf("✓ 抽奖结果: %d\n", result)
}

// 运行时配置更新示例
func runtimeConfigExample(ctx context.Context, rdb *redis.Client) {
	engine := lottery.NewLotteryEngine(rdb)

	// 获取当前配置
	currentConfig := engine.GetConfig()
	fmt.Printf("当前配置: 锁超时=%v, 重试次数=%d\n",
		currentConfig.LockTimeout, currentConfig.RetryAttempts)

	// 更新锁超时时间
	err := engine.SetLockTimeout(5 * time.Second)
	if err != nil {
		fmt.Printf("❌ 更新锁超时失败: %v\n", err)
		return
	}
	fmt.Println("✓ 锁超时时间已更新为 5秒")

	// 更新重试次数
	err = engine.SetRetryAttempts(10)
	if err != nil {
		fmt.Printf("❌ 更新重试次数失败: %v\n", err)
		return
	}
	fmt.Println("✓ 重试次数已更新为 10次")

	// 验证配置更新
	updatedConfig := engine.GetConfig()
	fmt.Printf("更新后配置: 锁超时=%v, 重试次数=%d\n",
		updatedConfig.LockTimeout, updatedConfig.RetryAttempts)

	// 测试更新后的配置
	result, err := engine.DrawInRange(ctx, "runtime_config_demo", 1, 100)
	if err != nil {
		fmt.Printf("❌ 抽奖失败: %v\n", err)
		return
	}
	fmt.Printf("✓ 使用新配置抽奖成功: %d\n", result)
}

// 自定义日志记录器实现
type CustomLogger struct {
	prefix string
}

func (l *CustomLogger) Info(msg string, args ...interface{}) {
	log.Printf("%s [INFO] "+msg, append([]interface{}{l.prefix}, args...)...)
}

func (l *CustomLogger) Error(msg string, args ...interface{}) {
	log.Printf("%s [ERROR] "+msg, append([]interface{}{l.prefix}, args...)...)
}

func (l *CustomLogger) Debug(msg string, args ...interface{}) {
	log.Printf("%s [DEBUG] "+msg, append([]interface{}{l.prefix}, args...)...)
}
