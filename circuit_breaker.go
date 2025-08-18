package lottery

import (
	"context"
	"time"

	"github.com/sony/gobreaker"
)

// CircuitBreakerEngine 带熔断器的抽奖引擎
type CircuitBreakerEngine struct {
	engine LotteryDrawer

	breaker *gobreaker.CircuitBreaker
	logger  Logger
	config  *CircuitBreakerConfig
}

// NewCircuitBreakerEngine 创建带熔断器的抽奖引擎
func NewCircuitBreakerEngine(engine LotteryDrawer, config *CircuitBreakerConfig, logger Logger) *CircuitBreakerEngine {
	if !config.Enabled {
		// 如果熔断器未启用，返回一个透传的包装器
		return &CircuitBreakerEngine{
			engine: engine,
			logger: logger,
			config: config,
		}
	}

	settings := gobreaker.Settings{
		Name:        config.Name,
		MaxRequests: config.MaxRequests,
		Interval:    config.Interval,
		Timeout:     config.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// 当请求数达到最小要求且失败率超过阈值时触发熔断
			return counts.Requests >= config.MinRequests &&
				float64(counts.TotalFailures)/float64(counts.Requests) >= config.FailureRatio
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			if config.OnStateChange && logger != nil {
				logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
			}
		},
	}

	return &CircuitBreakerEngine{
		engine:  engine,
		breaker: gobreaker.NewCircuitBreaker(settings),
		logger:  logger,
		config:  config,
	}
}

// executeWithBreaker 使用熔断器执行操作
func (c *CircuitBreakerEngine) executeWithBreaker(operation func() (any, error)) (any, error) {
	if c.breaker == nil {
		// 熔断器未启用，直接执行
		return operation()
	}

	result, err := c.breaker.Execute(operation)
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

// DrawInRange 在指定范围内抽奖
func (c *CircuitBreakerEngine) DrawInRange(ctx context.Context, lockKey string, min, max int) (int, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawInRange(ctx, lockKey, min, max)
	})
	if err != nil {
		return 0, err
	}

	return result.(int), nil
}

// DrawMultipleInRange 在指定范围内进行多次抽奖
func (c *CircuitBreakerEngine) DrawMultipleInRange(ctx context.Context, lockKey string, min, max, count int) ([]int, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawMultipleInRange(ctx, lockKey, min, max, count)
	})
	if err != nil {
		return nil, err
	}

	return result.([]int), nil
}

// DrawFromPrizes 从奖品池中抽奖
func (c *CircuitBreakerEngine) DrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize) (*Prize, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawFromPrizes(ctx, lockKey, prizes)
	})
	if err != nil {
		return nil, err
	}

	return result.(*Prize), nil
}

// DrawMultipleFromPrizes 从奖品池中进行多次抽奖
func (c *CircuitBreakerEngine) DrawMultipleFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int) ([]*Prize, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawMultipleFromPrizes(ctx, lockKey, prizes, count)
	})
	if err != nil {
		return nil, err
	}

	return result.([]*Prize), nil
}

// DrawMultipleInRangeWithRecovery 带恢复机制的多次范围抽奖
func (c *CircuitBreakerEngine) DrawMultipleInRangeWithRecovery(ctx context.Context, lockKey string, min, max, count int) (*MultiDrawResult, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawMultipleInRangeWithRecovery(ctx, lockKey, min, max, count)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// DrawMultipleFromPrizesWithRecovery 带恢复机制的多次奖品抽奖
func (c *CircuitBreakerEngine) DrawMultipleFromPrizesWithRecovery(ctx context.Context, lockKey string, prizes []Prize, count int) (*MultiDrawResult, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawMultipleFromPrizesWithRecovery(ctx, lockKey, prizes, count)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// ResumeMultiDrawInRange 恢复多次范围抽奖
func (c *CircuitBreakerEngine) ResumeMultiDrawInRange(ctx context.Context, lockKey string, min, max, count int) (*MultiDrawResult, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.ResumeMultiDrawInRange(ctx, lockKey, min, max, count)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// ResumeMultiDrawFromPrizes 恢复多次奖品抽奖
func (c *CircuitBreakerEngine) ResumeMultiDrawFromPrizes(ctx context.Context, lockKey string, prizes []Prize, count int) (*MultiDrawResult, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.ResumeMultiDrawFromPrizes(ctx, lockKey, prizes, count)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// RollbackMultiDraw 回滚多次抽奖
func (c *CircuitBreakerEngine) RollbackMultiDraw(ctx context.Context, drawState *DrawState) error {
	_, err := c.executeWithBreaker(func() (any, error) {
		return nil, c.engine.RollbackMultiDraw(ctx, drawState)
	})

	return err
}

// SaveDrawState 保存抽奖状态
func (c *CircuitBreakerEngine) SaveDrawState(ctx context.Context, drawState *DrawState) error {
	_, err := c.executeWithBreaker(func() (any, error) {
		return nil, c.engine.SaveDrawState(ctx, drawState)
	})

	return err
}

// LoadDrawState 加载抽奖状态
func (c *CircuitBreakerEngine) LoadDrawState(ctx context.Context, lockKey string) (*DrawState, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.LoadDrawState(ctx, lockKey)
	})
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	return result.(*DrawState), nil
}

// DrawMultipleInRangeOptimized 优化的多次范围抽奖
func (c *CircuitBreakerEngine) DrawMultipleInRangeOptimized(ctx context.Context, lockKey string, min, max, count int, progressCallback ProgressCallback) (*MultiDrawResult, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawMultipleInRangeOptimized(ctx, lockKey, min, max, count, progressCallback)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// DrawMultipleFromPrizesOptimized 优化的多次奖品抽奖
func (c *CircuitBreakerEngine) DrawMultipleFromPrizesOptimized(ctx context.Context, lockKey string, prizes []Prize, count int, progressCallback ProgressCallback) (*MultiDrawResult, error) {
	result, err := c.executeWithBreaker(func() (any, error) {
		return c.engine.DrawMultipleFromPrizesOptimized(ctx, lockKey, prizes, count, progressCallback)
	})
	if err != nil {
		return nil, err
	}

	return result.(*MultiDrawResult), nil
}

// GetCircuitBreakerState 获取熔断器状态
func (c *CircuitBreakerEngine) GetCircuitBreakerState() string {
	if c.breaker == nil {
		return "disabled"
	}

	switch c.breaker.State() {
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
func (c *CircuitBreakerEngine) GetCircuitBreakerCounts() gobreaker.Counts {
	if c.breaker == nil {
		return gobreaker.Counts{}
	}

	return c.breaker.Counts()
}

// ResetCircuitBreaker 重置熔断器 (重新创建熔断器实例)
func (c *CircuitBreakerEngine) ResetCircuitBreaker() {
	if c.breaker != nil {
		// gobreaker 没有 Reset 方法，我们重新创建一个实例
		c.breaker = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        c.config.Name,
			MaxRequests: c.config.MaxRequests,
			Interval:    c.config.Interval,
			Timeout:     c.config.Timeout,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
				return counts.Requests >= c.config.MinRequests && failureRatio >= c.config.FailureRatio
			},
			OnStateChange: func(name string, from, to gobreaker.State) {
				if c.config.OnStateChange && c.logger != nil {
					c.logger.Info("Circuit breaker '%s' state changed from %s to %s", name, from, to)
				}
			},
		})
		if c.logger != nil {
			c.logger.Info("Circuit breaker '%s' has been reset (recreated)", c.config.Name)
		}
	}
}

// CircuitBreakerHealthCheck 熔断器健康检查
type CircuitBreakerHealthCheck struct {
	engine *CircuitBreakerEngine
}

// NewCircuitBreakerHealthCheck 创建熔断器健康检查
func NewCircuitBreakerHealthCheck(engine *CircuitBreakerEngine) *CircuitBreakerHealthCheck {
	return &CircuitBreakerHealthCheck{
		engine: engine,
	}
}

// Check 执行健康检查
func (h *CircuitBreakerHealthCheck) Check() map[string]any {
	result := map[string]any{
		"circuit_breaker_enabled": h.engine.config.Enabled,
	}

	if h.engine.config.Enabled && h.engine.breaker != nil {
		state := h.engine.GetCircuitBreakerState()
		counts := h.engine.GetCircuitBreakerCounts()

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

// CircuitBreakerMetrics 熔断器指标收集器
type CircuitBreakerMetrics struct {
	engine *CircuitBreakerEngine
}

// NewCircuitBreakerMetrics 创建熔断器指标收集器
func NewCircuitBreakerMetrics(engine *CircuitBreakerEngine) *CircuitBreakerMetrics {
	return &CircuitBreakerMetrics{
		engine: engine,
	}
}

// CollectMetrics 收集指标
func (m *CircuitBreakerMetrics) CollectMetrics() map[string]any {
	metrics := map[string]any{
		"circuit_breaker_enabled": m.engine.config.Enabled,
		"timestamp":               time.Now().Unix(),
	}

	if m.engine.config.Enabled && m.engine.breaker != nil {
		state := m.engine.GetCircuitBreakerState()
		counts := m.engine.GetCircuitBreakerCounts()

		// 状态指标
		metrics["circuit_breaker_state"] = state
		metrics["circuit_breaker_state_numeric"] = m.stateToNumeric(state)

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
		metrics["circuit_breaker_max_requests"] = m.engine.config.MaxRequests
		metrics["circuit_breaker_failure_ratio_threshold"] = m.engine.config.FailureRatio
		metrics["circuit_breaker_min_requests"] = m.engine.config.MinRequests
		metrics["circuit_breaker_interval_seconds"] = m.engine.config.Interval.Seconds()
		metrics["circuit_breaker_timeout_seconds"] = m.engine.config.Timeout.Seconds()
	}

	return metrics
}

// stateToNumeric 将状态转换为数值
func (m *CircuitBreakerMetrics) stateToNumeric(state string) int {
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
