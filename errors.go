package lottery

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"time"
)

// ErrorCode 错误代码类型
type ErrorCode string

// 错误代码常量
const (
	// 系统级错误 (1000-1999)
	ErrCodeSystem             ErrorCode = "LOTTERY_1000"
	ErrCodeRedisConnection    ErrorCode = "LOTTERY_1001"
	ErrCodeRedisTimeout       ErrorCode = "LOTTERY_1002"
	ErrCodeRedisCluster       ErrorCode = "LOTTERY_1003"
	ErrCodeConfigInvalid      ErrorCode = "LOTTERY_1004"
	ErrCodeServiceUnavailable ErrorCode = "LOTTERY_1005"

	// 业务级错误 (2000-2999)
	ErrCodeInvalidParameters    ErrorCode = "LOTTERY_2000"
	ErrCodeInvalidRange         ErrorCode = "LOTTERY_2001"
	ErrCodeInvalidCount         ErrorCode = "LOTTERY_2002"
	ErrCodeInvalidProbability   ErrorCode = "LOTTERY_2003"
	ErrCodeEmptyPrizePool       ErrorCode = "LOTTERY_2004"
	ErrCodeInvalidPrize         ErrorCode = "LOTTERY_2005"
	ErrCodeDrawStateCorrupted   ErrorCode = "LOTTERY_2006"
	ErrCodeInvalidPrizeID       ErrorCode = "LOTTERY_2007"
	ErrCodeInvalidPrizeName     ErrorCode = "LOTTERY_2008"
	ErrCodeNegativePrizeValue   ErrorCode = "LOTTERY_2009"
	ErrCodeInvalidLockTimeout   ErrorCode = "LOTTERY_2010"
	ErrCodeInvalidRetryAttempts ErrorCode = "LOTTERY_2011"
	ErrCodeInvalidRetryInterval ErrorCode = "LOTTERY_2012"
	ErrCodeDrawInterrupted      ErrorCode = "LOTTERY_2013"
	ErrCodePartialDrawFailure   ErrorCode = "LOTTERY_2014"
	ErrCodeInvalidLockCacheTTL  ErrorCode = "LOTTERY_2015"

	// 锁相关错误 (3000-3999)
	ErrCodeLockAcquisitionFailed ErrorCode = "LOTTERY_3000"
	ErrCodeLockTimeout           ErrorCode = "LOTTERY_3001"
	ErrCodeLockReleaseFailure    ErrorCode = "LOTTERY_3002"
	ErrCodeLockAlreadyHeld       ErrorCode = "LOTTERY_3003"

	// 安全相关错误 (4000-4999)
	ErrCodeUnauthorized     ErrorCode = "LOTTERY_4000"
	ErrCodeForbidden        ErrorCode = "LOTTERY_4001"
	ErrCodeTokenExpired     ErrorCode = "LOTTERY_4002"
	ErrCodeTokenInvalid     ErrorCode = "LOTTERY_4003"
	ErrCodeEncryptionFailed ErrorCode = "LOTTERY_4004"
	ErrCodeDecryptionFailed ErrorCode = "LOTTERY_4005"

	// 限流相关错误 (5000-5999)
	ErrCodeRateLimitExceeded  ErrorCode = "LOTTERY_5000"
	ErrCodeConcurrencyLimit   ErrorCode = "LOTTERY_5001"
	ErrCodeCircuitBreakerOpen ErrorCode = "LOTTERY_5002"

	// 状态相关错误 (6000-6999)
	ErrCodeStateNotFound         ErrorCode = "LOTTERY_6000"
	ErrCodeStateSaveFailure      ErrorCode = "LOTTERY_6001"
	ErrCodeStateLoadFailure      ErrorCode = "LOTTERY_6002"
	ErrCodeStateCorrupted        ErrorCode = "LOTTERY_6003"
	ErrCodeSerializationFailed   ErrorCode = "LOTTERY_6004"
	ErrCodeDeserializationFailed ErrorCode = "LOTTERY_6005"
)

// ErrorSeverity 错误严重程度
type ErrorSeverity string

const (
	SeverityCritical ErrorSeverity = "critical"
	SeverityHigh     ErrorSeverity = "high"
	SeverityMedium   ErrorSeverity = "medium"
	SeverityLow      ErrorSeverity = "low"
	SeverityInfo     ErrorSeverity = "info"
)

// LotteryError 增强的错误类型
type LotteryError struct {
	Code       ErrorCode      `json:"code"`
	Message    string         `json:"message"`
	Details    string         `json:"details,omitempty"`
	Severity   ErrorSeverity  `json:"severity"`
	Timestamp  time.Time      `json:"timestamp"`
	RequestID  string         `json:"request_id,omitempty"`
	UserID     string         `json:"user_id,omitempty"`
	Operation  string         `json:"operation,omitempty"`
	StackTrace string         `json:"stack_trace,omitempty"`
	Cause      error          `json:"-"`
	Retryable  bool           `json:"retryable"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Error 实现 error 接口
func (e *LotteryError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap 实现 errors.Unwrap 接口
func (e *LotteryError) Unwrap() error {
	return e.Cause
}

// Is 实现 errors.Is 接口
func (e *LotteryError) Is(target error) bool {
	if t, ok := target.(*LotteryError); ok {
		return e.Code == t.Code
	}
	return false
}

// WithCause 添加原因错误
func (e *LotteryError) WithCause(cause error) *LotteryError {
	e.Cause = cause
	return e
}

// WithDetails 添加详细信息
func (e *LotteryError) WithDetails(details string) *LotteryError {
	e.Details = details
	return e
}

// WithRequestID 添加请求ID
func (e *LotteryError) WithRequestID(requestID string) *LotteryError {
	e.RequestID = requestID
	return e
}

// WithUserID 添加用户ID
func (e *LotteryError) WithUserID(userID string) *LotteryError {
	e.UserID = userID
	return e
}

// WithOperation 添加操作信息
func (e *LotteryError) WithOperation(operation string) *LotteryError {
	e.Operation = operation
	return e
}

// WithMetadata 添加元数据
func (e *LotteryError) WithMetadata(key string, value interface{}) *LotteryError {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithStackTrace 添加堆栈跟踪
func (e *LotteryError) WithStackTrace() *LotteryError {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	e.StackTrace = string(buf[:n])
	return e
}

// NewError 创建新的错误
func NewError(code ErrorCode, message string) *LotteryError {
	return &LotteryError{
		Code:      code,
		Message:   message,
		Severity:  SeverityMedium,
		Timestamp: time.Now(),
		Retryable: false,
	}
}

// NewRetryableError 创建可重试的错误
func NewRetryableError(code ErrorCode, message string) *LotteryError {
	return &LotteryError{
		Code:      code,
		Message:   message,
		Severity:  SeverityMedium,
		Timestamp: time.Now(),
		Retryable: true,
	}
}

// NewCriticalError 创建严重错误
func NewCriticalError(code ErrorCode, message string) *LotteryError {
	err := &LotteryError{
		Code:      code,
		Message:   message,
		Severity:  SeverityCritical,
		Timestamp: time.Now(),
		Retryable: false,
	}
	return err.WithStackTrace()
}

// 预定义的错误实例
var (
	// 系统级错误
	ErrSystemError           = NewCriticalError(ErrCodeSystem, "system error occurred")
	ErrRedisConnectionFailed = NewRetryableError(ErrCodeRedisConnection, "Redis connection failed")
	ErrRedisTimeout          = NewRetryableError(ErrCodeRedisTimeout, "Redis operation timeout")
	ErrRedisCluster          = NewRetryableError(ErrCodeRedisCluster, "Redis cluster error")
	ErrConfigInvalid         = NewCriticalError(ErrCodeConfigInvalid, "configuration is invalid")
	ErrServiceUnavailable    = NewRetryableError(ErrCodeServiceUnavailable, "service temporarily unavailable")

	// 业务级错误
	ErrInvalidParameters    = NewError(ErrCodeInvalidParameters, "invalid parameters provided")
	ErrInvalidRange         = NewError(ErrCodeInvalidRange, "invalid range: min must be less than or equal to max")
	ErrInvalidCount         = NewError(ErrCodeInvalidCount, "invalid count: must be greater than 0")
	ErrInvalidProbability   = NewError(ErrCodeInvalidProbability, "invalid probability: probabilities must sum to 1.0")
	ErrEmptyPrizePool       = NewError(ErrCodeEmptyPrizePool, "prize pool cannot be empty")
	ErrInvalidPrize         = NewError(ErrCodeInvalidPrize, "invalid prize configuration")
	ErrDrawStateCorrupted   = NewError(ErrCodeDrawStateCorrupted, "draw state is corrupted")
	ErrInvalidPrizeID       = NewError(ErrCodeInvalidPrizeID, "invalid prize ID: cannot be empty")
	ErrInvalidPrizeName     = NewError(ErrCodeInvalidPrizeName, "invalid prize name: cannot be empty")
	ErrNegativePrizeValue   = NewError(ErrCodeNegativePrizeValue, "invalid prize value: cannot be negative")
	ErrInvalidLockTimeout   = NewError(ErrCodeInvalidLockTimeout, "invalid lock timeout: must be between 1s and 5m")
	ErrInvalidRetryAttempts = NewError(ErrCodeInvalidRetryAttempts, "invalid retry attempts: must be between 0 and 10")
	ErrInvalidRetryInterval = NewError(ErrCodeInvalidRetryInterval, "invalid retry interval: cannot be negative")
	ErrInvalidLockCacheTTL  = NewError(ErrCodeInvalidLockCacheTTL, "invalid lock cache TTL: must be between 1s and 5m")
	ErrDrawInterrupted      = NewError(ErrCodeDrawInterrupted, "draw operation interrupted")
	ErrPartialDrawFailure   = NewError(ErrCodePartialDrawFailure, "partial draw failure: some draws completed successfully, some failed")

	// 锁相关错误
	ErrLockAcquisitionFailed = NewRetryableError(ErrCodeLockAcquisitionFailed, "failed to acquire distributed lock")
	ErrLockTimeout           = NewRetryableError(ErrCodeLockTimeout, "lock acquisition timeout")
	ErrLockReleaseFailure    = NewError(ErrCodeLockReleaseFailure, "failed to release lock")
	ErrLockAlreadyHeld       = NewError(ErrCodeLockAlreadyHeld, "lock is already held")

	// 安全相关错误
	ErrUnauthorized     = NewError(ErrCodeUnauthorized, "unauthorized access")
	ErrForbidden        = NewError(ErrCodeForbidden, "access forbidden")
	ErrTokenExpired     = NewError(ErrCodeTokenExpired, "token has expired")
	ErrTokenInvalid     = NewError(ErrCodeTokenInvalid, "invalid token")
	ErrEncryptionFailed = NewError(ErrCodeEncryptionFailed, "encryption failed")
	ErrDecryptionFailed = NewError(ErrCodeDecryptionFailed, "decryption failed")

	// 限流相关错误
	ErrRateLimitExceeded  = NewRetryableError(ErrCodeRateLimitExceeded, "rate limit exceeded")
	ErrConcurrencyLimit   = NewRetryableError(ErrCodeConcurrencyLimit, "concurrency limit exceeded")
	ErrCircuitBreakerOpen = NewRetryableError(ErrCodeCircuitBreakerOpen, "circuit breaker is open")

	// 状态相关错误
	ErrStateNotFound         = NewError(ErrCodeStateNotFound, "state not found")
	ErrStateSaveFailure      = NewRetryableError(ErrCodeStateSaveFailure, "failed to save state")
	ErrStateLoadFailure      = NewRetryableError(ErrCodeStateLoadFailure, "failed to load state")
	ErrStateCorrupted        = NewError(ErrCodeStateCorrupted, "state data is corrupted")
	ErrSerializationFailed   = NewError(ErrCodeSerializationFailed, "serialization failed")
	ErrDeserializationFailed = NewError(ErrCodeDeserializationFailed, "deserialization failed")
)

// ErrorHandler 错误处理器接口
type ErrorHandler interface {
	HandleError(ctx context.Context, err error) error
	ShouldRetry(err error) bool
	GetRetryDelay(attempt int, err error) time.Duration
}

// DefaultErrorHandler 默认错误处理器
type DefaultErrorHandler struct {
	logger        Logger
	maxRetries    int
	baseDelay     time.Duration
	maxDelay      time.Duration
	backoffFactor float64
}

// NewDefaultErrorHandler 创建默认错误处理器
func NewDefaultErrorHandler(logger Logger) *DefaultErrorHandler {
	return &DefaultErrorHandler{
		logger:        logger,
		maxRetries:    3,
		baseDelay:     100 * time.Millisecond,
		maxDelay:      30 * time.Second,
		backoffFactor: 2.0,
	}
}

// HandleError 处理错误
func (h *DefaultErrorHandler) HandleError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	// 转换为 LotteryError
	var lotteryErr *LotteryError
	if !errors.As(err, &lotteryErr) {
		// 包装普通错误
		lotteryErr = NewError(ErrCodeSystem, err.Error()).WithCause(err)
	}

	// 从上下文中提取信息
	if requestID := ctx.Value("request_id"); requestID != nil {
		if id, ok := requestID.(string); ok {
			lotteryErr.WithRequestID(id)
		}
	}

	if userID := ctx.Value("user_id"); userID != nil {
		if id, ok := userID.(string); ok {
			lotteryErr.WithUserID(id)
		}
	}

	// 记录错误日志
	h.logError(lotteryErr)

	return lotteryErr
}

// ShouldRetry 判断是否应该重试
func (h *DefaultErrorHandler) ShouldRetry(err error) bool {
	var lotteryErr *LotteryError
	if errors.As(err, &lotteryErr) {
		return lotteryErr.Retryable
	}

	// 检查常见的可重试错误
	if IsRetryableError(err) {
		return true
	}

	return false
}

// GetRetryDelay 获取重试延迟
func (h *DefaultErrorHandler) GetRetryDelay(attempt int, err error) time.Duration {
	if attempt <= 0 {
		return h.baseDelay
	}

	// 指数退避算法
	delay := time.Duration(float64(h.baseDelay) * pow(h.backoffFactor, float64(attempt-1)))

	// 添加抖动 (±25%)
	jitter := time.Duration(float64(delay) * 0.25 * (2*rand.Float64() - 1))
	delay += jitter

	// 限制最大延迟
	if delay > h.maxDelay {
		delay = h.maxDelay
	}

	return delay
}

// logError 记录错误日志
func (h *DefaultErrorHandler) logError(err *LotteryError) {
	fields := map[string]interface{}{
		"error_code": err.Code,
		"severity":   err.Severity,
		"timestamp":  err.Timestamp,
		"retryable":  err.Retryable,
	}

	if err.RequestID != "" {
		fields["request_id"] = err.RequestID
	}

	if err.UserID != "" {
		fields["user_id"] = err.UserID
	}

	if err.Operation != "" {
		fields["operation"] = err.Operation
	}

	if err.Metadata != nil {
		for k, v := range err.Metadata {
			fields[k] = v
		}
	}

	switch err.Severity {
	case SeverityCritical:
		h.logger.Error("Critical error occurred: %s", err.Error())
	case SeverityHigh:
		h.logger.Error("High severity error: %s", err.Error())
	case SeverityMedium:
		h.logger.Error("Medium severity error: %s", err.Error())
	case SeverityLow:
		h.logger.Info("Low severity error: %s", err.Error())
	case SeverityInfo:
		h.logger.Info("Info level error: %s", err.Error())
	default:
		h.logger.Error("Unknown severity error: %s", err.Error())
	}
}

// IsRetryableError 检查是否为可重试错误
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"network is unreachable",
		"temporary failure",
		"server closed",
		"broken pipe",
		"i/o timeout",
		"dial tcp",
		"read tcp",
		"write tcp",
		"connection timed out",
		"no route to host",
		"host is down",
		"connection aborted",
		"socket is not connected",
		"operation timed out",
		"redis: connection pool timeout",
		"redis: client is closed",
		"context deadline exceeded",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	return false
}

// pow 计算幂次方 (简单实现)
func pow(base, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

// ErrorRecovery 错误恢复策略
type ErrorRecovery struct {
	handler    ErrorHandler
	maxRetries int
	logger     Logger
}

// NewErrorRecovery 创建错误恢复策略
func NewErrorRecovery(handler ErrorHandler, maxRetries int, logger Logger) *ErrorRecovery {
	return &ErrorRecovery{
		handler:    handler,
		maxRetries: maxRetries,
		logger:     logger,
	}
}

// ExecuteWithRetry 执行带重试的操作
func (r *ErrorRecovery) ExecuteWithRetry(ctx context.Context, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return NewError(ErrCodeSystem, "operation cancelled").WithCause(ctx.Err())
		default:
		}

		// 执行操作
		err := operation()
		if err == nil {
			if attempt > 0 {
				r.logger.Info("Operation succeeded after %d retries", attempt)
			}
			return nil
		}

		// 处理错误
		lastErr = r.handler.HandleError(ctx, err)

		// 检查是否应该重试
		if !r.handler.ShouldRetry(lastErr) {
			r.logger.Debug("Error is not retryable: %v", lastErr)
			break
		}

		// 如果不是最后一次尝试，等待重试
		if attempt < r.maxRetries {
			delay := r.handler.GetRetryDelay(attempt+1, lastErr)
			r.logger.Debug("Retrying operation in %v (attempt %d/%d)", delay, attempt+1, r.maxRetries)

			select {
			case <-ctx.Done():
				return NewError(ErrCodeSystem, "operation cancelled during retry").WithCause(ctx.Err())
			case <-time.After(delay):
				// 继续重试
			}
		}
	}

	return NewError(ErrCodeSystem, fmt.Sprintf("operation failed after %d attempts", r.maxRetries+1)).WithCause(lastErr)
}
