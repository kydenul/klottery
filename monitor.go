package lottery

import (
	"sync"
	"sync/atomic"
	"time"
)

// PerformanceMetrics 性能指标收集器
type PerformanceMetrics struct {
	// 抽奖操作统计
	TotalDraws      int64 `json:"total_draws"`      // 总抽奖次数
	SuccessfulDraws int64 `json:"successful_draws"` // 成功抽奖次数
	FailedDraws     int64 `json:"failed_draws"`     // 失败抽奖次数

	// 锁操作统计
	LockAcquisitions    int64 `json:"lock_acquisitions"`     // 锁获取次数
	LockAcquisitionTime int64 `json:"lock_acquisition_time"` // 锁获取总时间(纳秒)
	LockReleases        int64 `json:"lock_releases"`         // 锁释放次数
	LockFailures        int64 `json:"lock_failures"`         // 锁获取失败次数

	// 性能统计
	AverageDrawTime int64 `json:"average_draw_time"` // 平均抽奖时间(纳秒)
	TotalDrawTime   int64 `json:"total_draw_time"`   // 总抽奖时间(纳秒)

	// Redis统计
	RedisErrors int64 `json:"redis_errors"` // Redis错误数

	// 时间戳
	StartTime      int64 `json:"start_time"`       // 开始时间
	LastUpdateTime int64 `json:"last_update_time"` // 最后更新时间
}

// GetSuccessRate 获取成功率
func (pm *PerformanceMetrics) GetSuccessRate() float64 {
	total := atomic.LoadInt64(&pm.TotalDraws)
	if total == 0 {
		return 0.0
	}
	successful := atomic.LoadInt64(&pm.SuccessfulDraws)
	return float64(successful) / float64(total) * 100.0
}

// GetAverageLockTime 获取平均锁获取时间
func (pm *PerformanceMetrics) GetAverageLockTime() time.Duration {
	acquisitions := atomic.LoadInt64(&pm.LockAcquisitions)
	if acquisitions == 0 {
		return 0
	}
	totalTime := atomic.LoadInt64(&pm.LockAcquisitionTime)
	return time.Duration(totalTime / acquisitions)
}

// GetThroughput 获取吞吐量(每秒操作数)
func (pm *PerformanceMetrics) GetThroughput() float64 {
	startTime := atomic.LoadInt64(&pm.StartTime)
	lastUpdate := atomic.LoadInt64(&pm.LastUpdateTime)
	if startTime == 0 || lastUpdate <= startTime {
		return 0.0
	}

	duration := time.Duration(lastUpdate - startTime)
	totalDraws := atomic.LoadInt64(&pm.TotalDraws)

	return float64(totalDraws) / duration.Seconds()
}

// Reset 重置性能指标
func (pm *PerformanceMetrics) Reset() {
	atomic.StoreInt64(&pm.TotalDraws, 0)
	atomic.StoreInt64(&pm.SuccessfulDraws, 0)
	atomic.StoreInt64(&pm.FailedDraws, 0)
	atomic.StoreInt64(&pm.LockAcquisitions, 0)
	atomic.StoreInt64(&pm.LockAcquisitionTime, 0)
	atomic.StoreInt64(&pm.LockReleases, 0)
	atomic.StoreInt64(&pm.LockFailures, 0)
	atomic.StoreInt64(&pm.AverageDrawTime, 0)
	atomic.StoreInt64(&pm.TotalDrawTime, 0)
	atomic.StoreInt64(&pm.RedisErrors, 0)
	atomic.StoreInt64(&pm.StartTime, time.Now().UnixNano())
	atomic.StoreInt64(&pm.LastUpdateTime, time.Now().UnixNano())
}

// ================================================================================

// PerformanceMonitor 性能监控器
type PerformanceMonitor struct {
	metrics *PerformanceMetrics
	mu      sync.RWMutex
	enabled bool
}

// NewPerformanceMonitor 创建新的性能监控器
func NewPerformanceMonitor() *PerformanceMonitor {
	pm := &PerformanceMonitor{
		metrics: &PerformanceMetrics{},
		enabled: true,
	}
	pm.metrics.Reset()
	return pm
}

// Enable 启用性能监控
func (pm *PerformanceMonitor) Enable() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.enabled = true
}

// Disable 禁用性能监控
func (pm *PerformanceMonitor) Disable() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.enabled = false
}

// IsEnabled 检查是否启用了性能监控
func (pm *PerformanceMonitor) IsEnabled() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.enabled
}

// RecordDraw 记录抽奖操作
func (pm *PerformanceMonitor) RecordDraw(success bool, duration time.Duration) {
	if !pm.IsEnabled() {
		return
	}

	atomic.AddInt64(&pm.metrics.TotalDraws, 1)
	atomic.AddInt64(&pm.metrics.TotalDrawTime, int64(duration))

	// 更新抽奖统计
	if success {
		atomic.AddInt64(&pm.metrics.SuccessfulDraws, 1)
	} else {
		atomic.AddInt64(&pm.metrics.FailedDraws, 1)
	}

	// 更新平均抽奖时间
	totalDraws := atomic.LoadInt64(&pm.metrics.TotalDraws)
	totalTime := atomic.LoadInt64(&pm.metrics.TotalDrawTime)
	atomic.StoreInt64(&pm.metrics.AverageDrawTime, totalTime/totalDraws)

	// 更新最后更新时间
	atomic.StoreInt64(&pm.metrics.LastUpdateTime, time.Now().UnixNano())
}

// RecordLockAcquisition 记录锁获取操作
func (pm *PerformanceMonitor) RecordLockAcquisition(success bool, duration time.Duration) {
	if !pm.IsEnabled() {
		return
	}

	if success {
		atomic.AddInt64(&pm.metrics.LockAcquisitions, 1)
		atomic.AddInt64(&pm.metrics.LockAcquisitionTime, int64(duration))
	} else {
		atomic.AddInt64(&pm.metrics.LockFailures, 1)
	}

	atomic.StoreInt64(&pm.metrics.LastUpdateTime, time.Now().UnixNano())
}

// RecordLockRelease 记录锁释放操作
func (pm *PerformanceMonitor) RecordLockRelease() {
	if !pm.IsEnabled() {
		return
	}

	atomic.AddInt64(&pm.metrics.LockReleases, 1)
	atomic.StoreInt64(&pm.metrics.LastUpdateTime, time.Now().UnixNano())
}

// RecordRedisError 记录Redis错误
func (pm *PerformanceMonitor) RecordRedisError() {
	if !pm.IsEnabled() {
		return
	}

	atomic.AddInt64(&pm.metrics.RedisErrors, 1)
	atomic.StoreInt64(&pm.metrics.LastUpdateTime, time.Now().UnixNano())
}

// GetMetrics 获取性能指标的副本
func (pm *PerformanceMonitor) GetMetrics() PerformanceMetrics {
	return PerformanceMetrics{
		TotalDraws:          atomic.LoadInt64(&pm.metrics.TotalDraws),
		SuccessfulDraws:     atomic.LoadInt64(&pm.metrics.SuccessfulDraws),
		FailedDraws:         atomic.LoadInt64(&pm.metrics.FailedDraws),
		LockAcquisitions:    atomic.LoadInt64(&pm.metrics.LockAcquisitions),
		LockAcquisitionTime: atomic.LoadInt64(&pm.metrics.LockAcquisitionTime),
		LockReleases:        atomic.LoadInt64(&pm.metrics.LockReleases),
		LockFailures:        atomic.LoadInt64(&pm.metrics.LockFailures),
		AverageDrawTime:     atomic.LoadInt64(&pm.metrics.AverageDrawTime),
		TotalDrawTime:       atomic.LoadInt64(&pm.metrics.TotalDrawTime),
		RedisErrors:         atomic.LoadInt64(&pm.metrics.RedisErrors),
		StartTime:           atomic.LoadInt64(&pm.metrics.StartTime),
		LastUpdateTime:      atomic.LoadInt64(&pm.metrics.LastUpdateTime),
	}
}

// ResetMetrics 重置性能指标
func (pm *PerformanceMonitor) ResetMetrics() { pm.metrics.Reset() }
