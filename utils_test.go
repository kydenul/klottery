package lottery

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRangeUtils(t *testing.T) {
	tests := []struct {
		name        string
		min         int
		max         int
		expectError bool
	}{
		{"valid_range", 1, 100, false},
		{"equal_values", 5, 5, false},
		{"invalid_range", 100, 1, true},
		{"negative_range", -10, -5, false},
		{"mixed_range", -5, 10, false},
		{"zero_range", 0, 0, false},
		{"large_range", 1, 1000000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRange(tt.min, tt.max)
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, ErrInvalidRange, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCountUtils(t *testing.T) {
	tests := []struct {
		name        string
		count       int
		expectError bool
	}{
		{"valid_count", 1, false},
		{"valid_large_count", 1000, false},
		{"zero_count", 0, true},
		{"negative_count", -1, true},
		{"negative_large_count", -100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCount(tt.count)
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, ErrInvalidCount, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGenerateLockValueUtils(t *testing.T) {
	t.Run("generates_non_empty_value", func(t *testing.T) {
		value := generateLockValue()
		assert.NotEmpty(t, value)
	})

	t.Run("generates_unique_values", func(t *testing.T) {
		values := make(map[string]bool)

		// Generate multiple values and check for uniqueness
		for i := 0; i < 100; i++ {
			value := generateLockValue()
			assert.False(t, values[value], "Generated duplicate lock value: %s", value)
			values[value] = true
		}
	})

	t.Run("generates_hex_string", func(t *testing.T) {
		value := generateLockValue()

		// Should be 32 characters (16 bytes * 2 hex chars per byte)
		if !strings.HasPrefix(value, "lock_") {
			assert.Len(t, value, 32)

			// Should only contain hex characters
			for _, char := range value {
				assert.True(t,
					(char >= '0' && char <= '9') || (char >= 'a' && char <= 'f'),
					"Invalid hex character: %c", char)
			}
		}
	})

	t.Run("fallback_format", func(t *testing.T) {
		// This test is harder to trigger since crypto/rand rarely fails
		// But we can at least verify the format would be correct
		value := generateLockValue()

		// Either hex format or timestamp fallback format
		if strings.HasPrefix(value, "lock_") {
			assert.True(t, len(value) > 5, "Fallback format should have timestamp")
		} else {
			assert.Len(t, value, 32, "Hex format should be 32 characters")
		}
	})
}

func TestCalculateOptimalBatchSizeUtils(t *testing.T) {
	tests := []struct {
		name          string
		totalCount    int
		expectedBatch int
	}{
		{"very_small_count", 5, 1},
		{"small_count", 10, 1},
		{"moderate_count_low", 50, 10},
		{"moderate_count_high", 100, 10},
		{"large_count_low", 500, 50},
		{"large_count_high", 1000, 50},
		{"very_large_count", 5000, 100},
		{"huge_count", 100000, 100},
		{"edge_case_11", 11, 10},
		{"edge_case_101", 101, 50},
		{"edge_case_1001", 1001, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize := calculateOptimalBatchSize(tt.totalCount)
			assert.Equal(t, tt.expectedBatch, batchSize)
		})
	}
}

func TestCalculateOptimalBatchSize_Properties(t *testing.T) {
	t.Run("batch_size_never_exceeds_total", func(t *testing.T) {
		for totalCount := 1; totalCount <= 1000; totalCount++ {
			batchSize := calculateOptimalBatchSize(totalCount)
			assert.LessOrEqual(t, batchSize, totalCount,
				"Batch size %d should not exceed total count %d", batchSize, totalCount)
		}
	})

	t.Run("batch_size_always_positive", func(t *testing.T) {
		for totalCount := 1; totalCount <= 1000; totalCount++ {
			batchSize := calculateOptimalBatchSize(totalCount)
			assert.Greater(t, batchSize, 0,
				"Batch size should be positive for total count %d", totalCount)
		}
	})

	t.Run("batch_size_increases_with_total", func(t *testing.T) {
		// Test that batch size generally increases with total count
		prevBatch := calculateOptimalBatchSize(1)

		checkPoints := []int{10, 50, 100, 500, 1000, 5000}
		for _, totalCount := range checkPoints {
			currentBatch := calculateOptimalBatchSize(totalCount)
			assert.GreaterOrEqual(t, currentBatch, prevBatch,
				"Batch size should not decrease as total count increases")
			prevBatch = currentBatch
		}
	})
}

func BenchmarkValidateRange(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ValidateRange(1, 100)
	}
}

func BenchmarkValidateCount(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ValidateCount(10)
	}
}

func BenchmarkGenerateLockValue(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = generateLockValue()
	}
}

func BenchmarkCalculateOptimalBatchSize(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = calculateOptimalBatchSize(1000)
	}
}

func TestUtilsEdgeCases(t *testing.T) {
	t.Run("validate_range_edge_cases", func(t *testing.T) {
		// Test with maximum int values
		err := ValidateRange(0, int(^uint(0)>>1)) // Max int
		assert.NoError(t, err)

		// Test with minimum int values
		minInt := -int(^uint(0)>>1) - 1
		err = ValidateRange(minInt, 0)
		assert.NoError(t, err)

		// Test invalid case with extreme values
		err = ValidateRange(100, -100)
		assert.Error(t, err)
	})

	t.Run("validate_count_edge_cases", func(t *testing.T) {
		// Test with maximum reasonable count
		err := ValidateCount(1000000)
		assert.NoError(t, err)

		// Test boundary values
		err = ValidateCount(1)
		assert.NoError(t, err)

		err = ValidateCount(0)
		assert.Error(t, err)

		err = ValidateCount(-1)
		assert.Error(t, err)
	})

	t.Run("batch_size_edge_cases", func(t *testing.T) {
		// Test boundary values for batch size calculation
		assert.Equal(t, 1, calculateOptimalBatchSize(1))
		assert.Equal(t, 1, calculateOptimalBatchSize(10))
		assert.Equal(t, 10, calculateOptimalBatchSize(11))
		assert.Equal(t, 10, calculateOptimalBatchSize(100))
		assert.Equal(t, 50, calculateOptimalBatchSize(101))
		assert.Equal(t, 50, calculateOptimalBatchSize(1000))
		assert.Equal(t, 100, calculateOptimalBatchSize(1001))
	})
}

// ================================================================================
// Logger Tests
// ================================================================================

func TestSilentLogger(t *testing.T) {
	logger := NewSilentLogger()

	// 测试所有方法都不会panic或产生错误
	t.Run("Info方法", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SilentLogger.Info() caused panic: %v", r)
			}
		}()
		logger.Info("test info message")
	})

	t.Run("Error方法", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SilentLogger.Error() caused panic: %v", r)
			}
		}()
		logger.Error("test error message")
	})

	t.Run("Debug方法", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SilentLogger.Debug() caused panic: %v", r)
			}
		}()
		logger.Debug("test debug message")
	})

	t.Run("多次调用", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			logger.Info("info %d", i)
			logger.Error("error %d", i)
			logger.Debug("debug %d", i)
		}
	})

	t.Run("空消息", func(t *testing.T) {
		logger.Info("")
		logger.Error("")
		logger.Debug("")
	})

	t.Run("长消息", func(t *testing.T) {
		longMessage := strings.Repeat("a", 10000)
		logger.Info(longMessage)
		logger.Error(longMessage)
		logger.Debug(longMessage)
	})
}

func TestDefaultLogger(t *testing.T) {
	// 由于DefaultLogger使用标准库的log包，我们主要测试它不会panic
	logger := &DefaultLogger{}

	t.Run("Info方法", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("DefaultLogger.Info() caused panic: %v", r)
			}
		}()
		logger.Info("test info message")
	})

	t.Run("Error方法", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("DefaultLogger.Error() caused panic: %v", r)
			}
		}()
		logger.Error("test error message")
	})

	t.Run("Debug方法", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("DefaultLogger.Debug() caused panic: %v", r)
			}
		}()
		logger.Debug("test debug message")
	})
}

// 性能基准测试
func BenchmarkSilentLogger(b *testing.B) {
	logger := NewSilentLogger()

	b.Run("Info", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("benchmark info message %d", i)
		}
	})

	b.Run("Error", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Error("benchmark error message %d", i)
		}
	})

	b.Run("Debug", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Debug("benchmark debug message %d", i)
		}
	})
}

func BenchmarkDefaultLogger(b *testing.B) {
	logger := &DefaultLogger{}

	b.Run("Info", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("benchmark info message %d", i)
		}
	})

	b.Run("Error", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Error("benchmark error message %d", i)
		}
	})

	b.Run("Debug", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Debug("benchmark debug message %d", i)
		}
	})
}
