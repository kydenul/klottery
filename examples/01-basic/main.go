package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-redis/redis/v8"

	"github.com/kydenul/lottery"
)

// 基础使用示例
func main() {
	fmt.Println("=== 线程安全抽奖系统 - 基础使用示例 ===")

	// 1. 初始化Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // 如果有密码请填写
		DB:       0,  // 使用默认数据库
	})
	defer rdb.Close()

	// 测试Redis连接
	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Redis连接失败: %v", err)
	}
	fmt.Println("✓ Redis连接成功")

	// 2. 创建抽奖引擎
	engine := lottery.NewLotteryEngine(rdb)
	fmt.Println("✓ 抽奖引擎创建成功")

	// 3. 基础范围抽奖示例
	fmt.Println("\n--- 基础范围抽奖 ---")
	basicRangeExample(ctx, engine)

	// 4. 基础奖品池抽奖示例
	fmt.Println("\n--- 基础奖品池抽奖 ---")
	basicPrizeExample(ctx, engine)

	// 5. 连续抽奖示例
	fmt.Println("\n--- 连续抽奖示例 ---")
	multipleDrawExample(ctx, engine)

	fmt.Println("\n=== 基础示例演示完成 ===")
}

// 基础范围抽奖示例
func basicRangeExample(ctx context.Context, engine *lottery.LotteryEngine) {
	// 在1-100范围内抽奖
	result, err := engine.DrawInRange(ctx, "user:basic_demo", 1, 100)
	if err != nil {
		fmt.Printf("❌ 抽奖失败: %v\n", err)
		return
	}
	fmt.Printf("✓ 抽奖结果: %d\n", result)

	// 在不同范围内抽奖
	result2, err := engine.DrawInRange(ctx, "user:basic_demo_2", 10, 50)
	if err != nil {
		fmt.Printf("❌ 抽奖失败: %v\n", err)
		return
	}
	fmt.Printf("✓ 抽奖结果 (10-50): %d\n", result2)
}

// 基础奖品池抽奖示例
func basicPrizeExample(ctx context.Context, engine *lottery.LotteryEngine) {
	// 定义奖品池
	prizes := []lottery.Prize{
		{ID: "first", Name: "一等奖", Probability: 0.05, Value: 1000},
		{ID: "second", Name: "二等奖", Probability: 0.15, Value: 500},
		{ID: "third", Name: "三等奖", Probability: 0.30, Value: 100},
		{ID: "consolation", Name: "谢谢参与", Probability: 0.50, Value: 0},
	}

	// 从奖品池抽奖
	prize, err := engine.DrawFromPrizes(ctx, "activity:basic_demo", prizes)
	if err != nil {
		fmt.Printf("❌ 奖品抽奖失败: %v\n", err)
		return
	}
	fmt.Printf("✓ 中奖结果: %s (价值: %d)\n", prize.Name, prize.Value)
}

// 连续抽奖示例
func multipleDrawExample(ctx context.Context, engine *lottery.LotteryEngine) {
	// 连续范围抽奖
	results, err := engine.DrawMultipleInRange(ctx, "user:multi_demo", 1, 20, 5, nil)
	if err != nil {
		fmt.Printf("❌ 连续抽奖失败: %v\n", err)
		return
	}
	fmt.Printf("✓ 连续抽奖结果 (5次): %v\n", results)

	// 连续奖品抽奖
	prizes := []lottery.Prize{
		{ID: "gold", Name: "金币", Probability: 0.3, Value: 100},
		{ID: "silver", Name: "银币", Probability: 0.4, Value: 50},
		{ID: "bronze", Name: "铜币", Probability: 0.3, Value: 10},
	}

	prizeResults, err := engine.DrawMultipleFromPrizes(ctx, "activity:multi_demo", prizes, 3, nil)
	if err != nil {
		fmt.Printf("❌ 连续奖品抽奖失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 连续奖品抽奖结果 (3次):\n")
	for i, prize := range prizeResults.PrizeResults {
		fmt.Printf("  第%d次: %s (价值: %d)\n", i+1, prize.Name, prize.Value)
	}
}
