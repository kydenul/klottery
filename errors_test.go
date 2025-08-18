package lottery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLotteryError(t *testing.T) {
	t.Run("basic_error", func(t *testing.T) {
		err := NewError(ErrCodeInvalidParameters, "test error message")

		assert.Equal(t, ErrCodeInvalidParameters, err.Code)
		assert.Equal(t, "test error message", err.Message)
		assert.Equal(t, SeverityMedium, err.Severity)
		assert.False(t, err.Retryable)
		assert.Contains(t, err.Error(), "LOTTERY_2000")
		assert.Contains(t, err.Error(), "test error message")
	})

	t.Run("retryable_error", func(t *testing.T) {
		err := NewRetryableError(ErrCodeRedisConnection, "connection failed")

		assert.True(t, err.Retryable)
		assert.Equal(t, ErrCodeRedisConnection, err.Code)
	})

	t.Run("critical_error", func(t *testing.T) {
		err := NewCriticalError(ErrCodeSystem, "system failure")

		assert.Equal(t, SeverityCritical, err.Severity)
		assert.NotEmpty(t, err.StackTrace)
	})

	t.Run("error_with_details", func(t *testing.T) {
		err := NewError(ErrCodeInvalidRange, "invalid range").
			WithDetails("min=10, max=5").
			WithRequestID("req-123").
			WithUserID("user-456").
			WithOperation("DrawInRange").
			WithMetadata("attempt", 3)

		assert.Equal(t, "invalid range", err.Message)
		assert.Equal(t, "min=10, max=5", err.Details)
		assert.Equal(t, "req-123", err.RequestID)
		assert.Equal(t, "user-456", err.UserID)
		assert.Equal(t, "DrawInRange", err.Operation)
		assert.Equal(t, 3, err.Metadata["attempt"])

		errorStr := err.Error()
		assert.Contains(t, errorStr, "invalid range")
		assert.Contains(t, errorStr, "min=10, max=5")
	})

	t.Run("error_with_cause", func(t *testing.T) {
		originalErr := errors.New("original error")
		err := NewError(ErrCodeSystem, "wrapped error").WithCause(originalErr)

		assert.Equal(t, originalErr, err.Unwrap())
		assert.True(t, errors.Is(err, originalErr))
	})

	t.Run("error_comparison", func(t *testing.T) {
		err1 := NewError(ErrCodeInvalidParameters, "error 1")
		err2 := NewError(ErrCodeInvalidParameters, "error 2")
		err3 := NewError(ErrCodeInvalidRange, "error 3")

		assert.True(t, errors.Is(err1, err2))
		assert.False(t, errors.Is(err1, err3))
	})
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       *LotteryError
		code      ErrorCode
		retryable bool
		severity  ErrorSeverity
	}{
		{"system_error", ErrSystemError, ErrCodeSystem, false, SeverityCritical},
		{"redis_connection", ErrRedisConnectionFailed, ErrCodeRedisConnection, true, SeverityMedium},
		{"invalid_parameters", ErrInvalidParameters, ErrCodeInvalidParameters, false, SeverityMedium},
		{"lock_acquisition_failed", ErrLockAcquisitionFailed, ErrCodeLockAcquisitionFailed, true, SeverityMedium},
		{"unauthorized", ErrUnauthorized, ErrCodeUnauthorized, false, SeverityMedium},
		{"rate_limit_exceeded", ErrRateLimitExceeded, ErrCodeRateLimitExceeded, true, SeverityMedium},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.code, tt.err.Code)
			assert.Equal(t, tt.retryable, tt.err.Retryable)
			assert.Equal(t, tt.severity, tt.err.Severity)
		})
	}
}

func TestDefaultErrorHandler(t *testing.T) {
	logger := &DefaultLogger{}
	handler := NewDefaultErrorHandler(logger)

	t.Run("handle_lottery_error", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), "request_id", "req-123")
		ctx = context.WithValue(ctx, "user_id", "user-456")

		originalErr := NewError(ErrCodeInvalidParameters, "test error")
		handledErr := handler.HandleError(ctx, originalErr)

		var lotteryErr *LotteryError
		require.True(t, errors.As(handledErr, &lotteryErr))
		assert.Equal(t, "req-123", lotteryErr.RequestID)
		assert.Equal(t, "user-456", lotteryErr.UserID)
	})

	t.Run("handle_regular_error", func(t *testing.T) {
		ctx := context.Background()
		originalErr := errors.New("regular error")
		handledErr := handler.HandleError(ctx, originalErr)

		var lotteryErr *LotteryError
		require.True(t, errors.As(handledErr, &lotteryErr))
		assert.Equal(t, ErrCodeSystem, lotteryErr.Code)
		assert.Equal(t, originalErr, lotteryErr.Unwrap())
	})

	t.Run("should_retry", func(t *testing.T) {
		retryableErr := NewRetryableError(ErrCodeRedisConnection, "connection failed")
		nonRetryableErr := NewError(ErrCodeInvalidParameters, "invalid params")
		regularErr := errors.New("connection timeout")

		assert.True(t, handler.ShouldRetry(retryableErr))
		assert.False(t, handler.ShouldRetry(nonRetryableErr))
		assert.True(t, handler.ShouldRetry(regularErr)) // 包含 "timeout"
	})

	t.Run("get_retry_delay", func(t *testing.T) {
		err := NewRetryableError(ErrCodeRedisConnection, "connection failed")

		// 测试多次以考虑抖动的影响
		var delays []time.Duration
		for i := 1; i <= 3; i++ {
			delays = append(delays, handler.GetRetryDelay(i, err))
		}

		// 延迟应该在合理范围内（考虑抖动）
		assert.True(t, delays[0] >= 75*time.Millisecond)  // 100ms - 25% jitter
		assert.True(t, delays[0] <= 125*time.Millisecond) // 100ms + 25% jitter
		assert.True(t, delays[2] <= 30*time.Second)

		// 基础延迟应该遵循指数退避（不考虑抖动的基础值）
		baseDelay1 := 100 * time.Millisecond
		baseDelay2 := 200 * time.Millisecond
		baseDelay3 := 400 * time.Millisecond

		// 验证延迟在预期范围内（考虑±25%抖动）
		assert.True(t, delays[0] >= time.Duration(float64(baseDelay1)*0.75))
		assert.True(t, delays[0] <= time.Duration(float64(baseDelay1)*1.25))
		assert.True(t, delays[1] >= time.Duration(float64(baseDelay2)*0.75))
		assert.True(t, delays[1] <= time.Duration(float64(baseDelay2)*1.25))
		assert.True(t, delays[2] >= time.Duration(float64(baseDelay3)*0.75))
		assert.True(t, delays[2] <= time.Duration(float64(baseDelay3)*1.25))
	})
}

func TestIsRetriableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retriable bool
	}{
		{"nil_error", nil, false},
		{"connection_refused", errors.New("connection refused"), true},
		{"connection_reset", errors.New("connection reset by peer"), true},
		{"timeout", errors.New("operation timeout"), true},
		{"network_unreachable", errors.New("network is unreachable"), true},
		{"temporary_failure", errors.New("temporary failure"), true},
		{"server_closed", errors.New("server closed"), true},
		{"broken_pipe", errors.New("broken pipe"), true},
		{"io_timeout", errors.New("i/o timeout"), true},
		{"dial_tcp", errors.New("dial tcp: connection failed"), true},
		{"read_tcp", errors.New("read tcp: connection reset"), true},
		{"write_tcp", errors.New("write tcp: broken pipe"), true},
		{"redis_pool_timeout", errors.New("redis: connection pool timeout"), true},
		{"redis_client_closed", errors.New("redis: client is closed"), true},
		{"context_deadline", errors.New("context deadline exceeded"), true},
		{"invalid_command", errors.New("ERR unknown command"), false},
		{"wrong_type", errors.New("WRONGTYPE Operation against wrong type"), false},
		{"syntax_error", errors.New("ERR syntax error"), false},
		{"out_of_memory", errors.New("OOM command not allowed"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryableError(tt.err)
			assert.Equal(t, tt.retriable, result)
		})
	}
}

func TestErrorRecovery(t *testing.T) {
	logger := &DefaultLogger{}
	handler := NewDefaultErrorHandler(logger)
	recovery := NewErrorRecovery(handler, 2, logger)

	t.Run("successful_operation", func(t *testing.T) {
		ctx := context.Background()
		callCount := 0

		err := recovery.ExecuteWithRetry(ctx, func() error {
			callCount++
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, 1, callCount)
	})

	t.Run("retry_then_success", func(t *testing.T) {
		ctx := context.Background()
		callCount := 0

		err := recovery.ExecuteWithRetry(ctx, func() error {
			callCount++
			if callCount < 3 {
				return NewRetryableError(ErrCodeRedisConnection, "connection failed")
			}
			return nil
		})

		assert.NoError(t, err)
		assert.Equal(t, 3, callCount)
	})

	t.Run("non_retryable_error", func(t *testing.T) {
		ctx := context.Background()
		callCount := 0

		err := recovery.ExecuteWithRetry(ctx, func() error {
			callCount++
			return NewError(ErrCodeInvalidParameters, "invalid params")
		})

		assert.Error(t, err)
		assert.Equal(t, 1, callCount) // 不应该重试
	})

	t.Run("max_retries_exceeded", func(t *testing.T) {
		ctx := context.Background()
		callCount := 0

		err := recovery.ExecuteWithRetry(ctx, func() error {
			callCount++
			return NewRetryableError(ErrCodeRedisConnection, "connection failed")
		})

		assert.Error(t, err)
		assert.Equal(t, 3, callCount) // 1 + 2 retries
		assert.Contains(t, err.Error(), "operation failed after 3 attempts")
	})

	t.Run("context_cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // 立即取消

		callCount := 0
		err := recovery.ExecuteWithRetry(ctx, func() error {
			callCount++
			return NewRetryableError(ErrCodeRedisConnection, "connection failed")
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "operation cancelled")
	})

	t.Run("context_cancelled_during_retry", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		// 创建一个会重试的错误处理器，但延迟很长
		slowHandler := &DefaultErrorHandler{
			logger:        logger,
			maxRetries:    3,
			baseDelay:     100 * time.Millisecond, // 比上下文超时长
			maxDelay:      30 * time.Second,
			backoffFactor: 2.0,
		}
		slowRecovery := NewErrorRecovery(slowHandler, 2, logger)

		callCount := 0
		err := slowRecovery.ExecuteWithRetry(ctx, func() error {
			callCount++
			return NewRetryableError(ErrCodeRedisConnection, "connection failed")
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")
	})
}

// 基准测试
func BenchmarkLotteryError_Creation(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := NewError(ErrCodeInvalidParameters, "test error").
			WithDetails("test details").
			WithRequestID("req-123").
			WithUserID("user-456").
			WithOperation("test-op").
			WithMetadata("key", "value")
		_ = err
	}
}

func BenchmarkErrorHandler_HandleError(b *testing.B) {
	logger := &DefaultLogger{}
	handler := NewDefaultErrorHandler(logger)
	ctx := context.Background()
	err := NewError(ErrCodeInvalidParameters, "test error")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handledErr := handler.HandleError(ctx, err)
		_ = handledErr
	}
}

func BenchmarkIsRetriableError(b *testing.B) {
	err := errors.New("connection timeout")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result := IsRetryableError(err)
		_ = result
	}
}

func BenchmarkErrorRecovery_ExecuteWithRetry(b *testing.B) {
	logger := &DefaultLogger{}
	handler := NewDefaultErrorHandler(logger)
	recovery := NewErrorRecovery(handler, 2, logger)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := recovery.ExecuteWithRetry(ctx, func() error {
			return nil // 总是成功
		})
		if err != nil {
			b.Fatalf("Unexpected error: %v", err)
		}
	}
}
