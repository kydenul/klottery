package main

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/kydenul/lottery"
)

func main() {
	// 初始化Redis客户端
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	// 创建抽奖引擎
	engine := lottery.NewLotteryEngine(rdb)
	ctx := context.Background()

	fmt.Println("=== 增强的连抽错误处理示例 ===")

	// 示例1：带错误恢复的连抽
	fmt.Println("\n1. 带错误恢复的范围连抽:")
	result, err := engine.DrawMultipleInRange(ctx, "enhanced_demo_1", 1, 100, 10, nil)
	if err != nil {
		if result != nil && result.PartialSuccess {
			successRate := float64(result.Completed) / float64(result.TotalRequested) * 100
			fmt.Printf("部分成功: 完成 %d/%d 次抽奖 (成功率: %.1f%%)\n",
				result.Completed, result.TotalRequested, successRate)
			fmt.Printf("结果: %v\n", result.Results)
		} else {
			fmt.Printf("抽奖失败: %v\n", err)
		}
	} else {
		fmt.Printf("全部成功: %v\n", result.Results)
	}

	// 示例2：带进度回调的优化连抽
	fmt.Println("\n2. 带进度回调的优化连抽:")

	progressCallback := func(completed, total int, currentResult any) {
		progress := float64(completed) / float64(total) * 100
		fmt.Printf("进度: %.1f%% (%d/%d) - 当前结果: %v\n", progress, completed, total, currentResult)
	}

	optimizedResult, err := engine.DrawMultipleInRange(ctx, "enhanced_demo_2", 1, 50, 5, progressCallback)
	if err != nil {
		fmt.Printf("优化连抽出错: %v\n", err)
	} else {
		fmt.Printf("优化连抽完成: %v\n", optimizedResult.Results)
	}

	// 示例3：奖品池连抽与错误处理
	fmt.Println("\n3. 奖品池连抽与错误处理:")

	prizes := []lottery.Prize{
		{ID: "gold", Name: "金奖", Probability: 0.1, Value: 1000},
		{ID: "silver", Name: "银奖", Probability: 0.2, Value: 500},
		{ID: "bronze", Name: "铜奖", Probability: 0.3, Value: 100},
		{ID: "consolation", Name: "安慰奖", Probability: 0.4, Value: 10},
	}

	prizeResult, err := engine.DrawMultipleFromPrizes(ctx, "enhanced_demo_3", prizes, 8, nil)
	if err != nil {
		if prizeResult != nil && prizeResult.PartialSuccess {
			fmt.Printf("奖品连抽部分成功: 完成 %d/%d 次\n", prizeResult.Completed, prizeResult.TotalRequested)
			for i, prize := range prizeResult.PrizeResults {
				fmt.Printf("  第%d次: %s (价值: %d)\n", i+1, prize.Name, prize.Value)
			}
		} else {
			fmt.Printf("奖品连抽失败: %v\n", err)
		}
	} else {
		fmt.Printf("奖品连抽全部成功:\n")
		for i, prize := range prizeResult.PrizeResults {
			fmt.Printf("  第%d次: %s (价值: %d)\n", i+1, prize.Name, prize.Value)
		}
	}

	// 示例4：演示错误详情
	fmt.Println("\n4. 错误详情示例:")

	// 创建一个会超时的context来模拟错误
	timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	time.Sleep(2 * time.Millisecond) // 确保context已经超时

	errorResult, err := engine.DrawMultipleInRange(timeoutCtx, "enhanced_demo_4", 1, 100, 20, nil)
	if err != nil {
		fmt.Printf("预期的错误: %v\n", err)
		if errorResult != nil {
			fmt.Printf("错误详情:\n")
			for _, errDetail := range errorResult.ErrorDetails {
				fmt.Printf("  抽奖 #%d: %v (时间: %d)\n", errDetail.DrawIndex, errDetail.Error, errDetail.Timestamp)
			}
		}
	}

	// 示例5：状态保存和恢复（演示接口）
	fmt.Println("\n5. 状态保存和恢复演示:")

	drawState := &lottery.DrawState{
		LockKey:        "demo_state",
		TotalCount:     10,
		CompletedCount: 6,
		Results:        []int{15, 42, 73, 28, 91, 56},
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}

	progress := float64(drawState.CompletedCount) / float64(drawState.TotalCount) * 100
	fmt.Printf("保存状态: 进度 %.1f%% (%d/%d)\n", progress, drawState.CompletedCount, drawState.TotalCount)

	err = engine.SaveDrawState(ctx, drawState)
	if err != nil {
		fmt.Printf("保存状态失败: %v\n", err)
	} else {
		fmt.Printf("状态保存成功\n")
	}

	// 尝试加载状态
	loadedState, err := engine.LoadDrawState(ctx, "demo_state")
	if err != nil {
		fmt.Printf("加载状态失败: %v\n", err)
	} else if loadedState == nil {
		fmt.Printf("未找到保存的状态\n")
	} else {
		loadedProgress := float64(loadedState.CompletedCount) / float64(loadedState.TotalCount) * 100
		fmt.Printf("加载状态成功: 进度 %.1f%%\n", loadedProgress)
	}

	// 示例6：恢复中断的抽奖
	fmt.Println("\n6. 恢复中断的抽奖演示:")

	resumeResult, err := engine.ResumeMultiDrawInRange(ctx, "resume_demo", 1, 100, 15)
	if err != nil {
		fmt.Printf("恢复抽奖失败: %v\n", err)
	} else {
		fmt.Printf("恢复抽奖成功: 完成 %d/%d 次\n", resumeResult.Completed, resumeResult.TotalRequested)
		if len(resumeResult.Results) > 0 {
			fmt.Printf("结果: %v\n", resumeResult.Results)
		}
	}

	fmt.Println("\n=== 增强功能演示完成 ===")
}
