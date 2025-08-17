package lottery

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redismock/v8"
)

// BenchmarkSerializeDrawState benchmarks the serialization of DrawState
func BenchmarkSerializeDrawState(b *testing.B) {
	testCases := []struct {
		name           string
		totalCount     int
		completedCount int
		prizeCount     int
	}{
		{"Small_10_draws", 10, 5, 2},
		{"Medium_100_draws", 100, 50, 10},
		{"Large_1000_draws", 1000, 500, 50},
		{"XLarge_10000_draws", 10000, 5000, 100},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			// Create test DrawState
			state := createBenchmarkDrawState(tc.totalCount, tc.completedCount, tc.prizeCount)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := serializeDrawState(state)
				if err != nil {
					b.Fatalf("Serialization failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkDeserializeDrawState benchmarks the deserialization of DrawState
func BenchmarkDeserializeDrawState(b *testing.B) {
	testCases := []struct {
		name           string
		totalCount     int
		completedCount int
		prizeCount     int
	}{
		{"Small_10_draws", 10, 5, 2},
		{"Medium_100_draws", 100, 50, 10},
		{"Large_1000_draws", 1000, 500, 50},
		{"XLarge_10000_draws", 10000, 5000, 100},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			// Create test DrawState and serialize it
			state := createBenchmarkDrawState(tc.totalCount, tc.completedCount, tc.prizeCount)
			data, err := serializeDrawState(state)
			if err != nil {
				b.Fatalf("Failed to serialize test data: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := deserializeDrawState(data)
				if err != nil {
					b.Fatalf("Deserialization failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkSaveState benchmarks the saveState operation with mock Redis
func BenchmarkSaveState(b *testing.B) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	testCases := []struct {
		name           string
		totalCount     int
		completedCount int
		prizeCount     int
	}{
		{"Small_10_draws", 10, 5, 2},
		{"Medium_100_draws", 100, 50, 10},
		{"Large_1000_draws", 1000, 500, 50},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			state := createBenchmarkDrawState(tc.totalCount, tc.completedCount, tc.prizeCount)

			// Setup mock expectations
			for i := 0; i < b.N; i++ {
				mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("lottery:state:benchmark_%d:%d", i, time.Now().UnixNano())
				err := spm.saveState(ctx, key, state, DefaultStateTTL)
				if err != nil {
					b.Fatalf("Save failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkLoadState benchmarks the loadState operation with mock Redis
func BenchmarkLoadState(b *testing.B) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	testCases := []struct {
		name           string
		totalCount     int
		completedCount int
		prizeCount     int
	}{
		{"Small_10_draws", 10, 5, 2},
		{"Medium_100_draws", 100, 50, 10},
		{"Large_1000_draws", 1000, 500, 50},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			state := createBenchmarkDrawState(tc.totalCount, tc.completedCount, tc.prizeCount)
			data, err := serializeDrawState(state)
			if err != nil {
				b.Fatalf("Failed to serialize test data: %v", err)
			}

			// Setup mock expectations
			for i := 0; i < b.N; i++ {
				mock.ExpectGet(fmt.Sprintf("lottery:state:benchmark_%d", i)).SetVal(string(data))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("lottery:state:benchmark_%d", i)
				_, err := spm.loadState(ctx, key)
				if err != nil {
					b.Fatalf("Load failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkRetryLogic benchmarks the retry mechanism with simulated failures
func BenchmarkRetryLogic(b *testing.B) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 2, 1*time.Millisecond) // Fast retries for benchmark
	ctx := context.Background()

	state := createBenchmarkDrawState(100, 50, 10)

	b.Run("NoRetry_Success", func(b *testing.B) {
		// Setup mock expectations for immediate success
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("lottery:state:benchmark_success_%d", i)
			err := spm.saveState(ctx, key, state, DefaultStateTTL)
			if err != nil {
				b.Fatalf("Save failed: %v", err)
			}
		}
	})

	b.Run("OneRetry_Success", func(b *testing.B) {
		// Setup mock expectations for failure then success
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("lottery:state:benchmark_retry_%d", i)
			err := spm.saveState(ctx, key, state, DefaultStateTTL)
			if err != nil {
				b.Fatalf("Save failed: %v", err)
			}
		}
	})
}

// BenchmarkConcurrentOperations benchmarks concurrent state operations
func BenchmarkConcurrentOperations(b *testing.B) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	state := createBenchmarkDrawState(100, 50, 10)

	b.Run("ConcurrentSaves", func(b *testing.B) {
		// Setup mock expectations
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("lottery:state:concurrent_%d_%d", b.N, i)
				err := spm.saveState(ctx, key, state, DefaultStateTTL)
				if err != nil {
					b.Fatalf("Concurrent save failed: %v", err)
				}
				i++
			}
		})
	})
}

// BenchmarkErrorHandlingOverhead benchmarks the performance impact of enhanced error handling
func BenchmarkErrorHandlingOverhead(b *testing.B) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	testCases := []struct {
		name           string
		totalCount     int
		completedCount int
		prizeCount     int
	}{
		{"Small_Enhanced", 10, 5, 2},
		{"Medium_Enhanced", 100, 50, 10},
		{"Large_Enhanced", 1000, 500, 50},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			state := createBenchmarkDrawState(tc.totalCount, tc.completedCount, tc.prizeCount)

			// Setup mock expectations for successful operations
			for i := 0; i < b.N; i++ {
				mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("lottery:state:enhanced_%s_%d", tc.name, i)
				err := spm.saveState(ctx, key, state, DefaultStateTTL)
				if err != nil {
					b.Fatalf("Enhanced save failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkRetryMechanismPerformance benchmarks the performance of the retry mechanism
func BenchmarkRetryMechanismPerformance(b *testing.B) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 2, 1*time.Microsecond) // Very fast retries for benchmark
	ctx := context.Background()

	state := createBenchmarkDrawState(100, 50, 10)

	b.Run("NoRetryNeeded", func(b *testing.B) {
		// All operations succeed immediately
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("lottery:state:no_retry_%d", i)
			err := spm.saveState(ctx, key, state, DefaultStateTTL)
			if err != nil {
				b.Fatalf("No retry save failed: %v", err)
			}
		}
	})

	b.Run("OneRetryNeeded", func(b *testing.B) {
		// First attempt fails, second succeeds
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("lottery:state:one_retry_%d", i)
			err := spm.saveState(ctx, key, state, DefaultStateTTL)
			if err != nil {
				b.Fatalf("One retry save failed: %v", err)
			}
		}
	})

	b.Run("TwoRetriesNeeded", func(b *testing.B) {
		// First two attempts fail, third succeeds
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("i/o timeout"))
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("lottery:state:two_retries_%d", i)
			err := spm.saveState(ctx, key, state, DefaultStateTTL)
			if err != nil {
				b.Fatalf("Two retries save failed: %v", err)
			}
		}
	})
}

// BenchmarkSerializationPerformanceBySize benchmarks serialization performance across different data sizes
func BenchmarkSerializationPerformanceBySize(b *testing.B) {
	testCases := []struct {
		name           string
		totalCount     int
		completedCount int
		prizeCount     int
		expectedSize   string
	}{
		{"Tiny_1_draw", 1, 1, 1, "~100B"},
		{"Small_10_draws", 10, 10, 5, "~1KB"},
		{"Medium_100_draws", 100, 100, 20, "~10KB"},
		{"Large_1000_draws", 1000, 1000, 50, "~100KB"},
		{"XLarge_5000_draws", 5000, 5000, 100, "~500KB"},
		{"XXLarge_10000_draws", 10000, 10000, 200, "~1MB"},
	}

	for _, tc := range testCases {
		b.Run(fmt.Sprintf("Serialize_%s", tc.name), func(b *testing.B) {
			state := createBenchmarkDrawState(tc.totalCount, tc.completedCount, tc.prizeCount)

			// Measure actual size for reference
			data, err := serializeDrawState(state)
			if err != nil {
				b.Fatalf("Failed to serialize test data: %v", err)
			}

			b.Logf("Actual serialized size: %d bytes (%s expected)", len(data), tc.expectedSize)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := serializeDrawState(state)
				if err != nil {
					b.Fatalf("Serialization failed: %v", err)
				}
			}

			// Report bytes per operation for throughput analysis
			b.SetBytes(int64(len(data)))
		})

		b.Run(fmt.Sprintf("Deserialize_%s", tc.name), func(b *testing.B) {
			state := createBenchmarkDrawState(tc.totalCount, tc.completedCount, tc.prizeCount)
			data, err := serializeDrawState(state)
			if err != nil {
				b.Fatalf("Failed to serialize test data: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := deserializeDrawState(data)
				if err != nil {
					b.Fatalf("Deserialization failed: %v", err)
				}
			}

			// Report bytes per operation for throughput analysis
			b.SetBytes(int64(len(data)))
		})
	}
}

// BenchmarkContextTimeoutHandling benchmarks performance under context timeout scenarios
func BenchmarkContextTimeoutHandling(b *testing.B) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 1, 1*time.Millisecond)

	state := createBenchmarkDrawState(100, 50, 10)

	b.Run("ShortTimeout_Success", func(b *testing.B) {
		// Operations complete within timeout
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			key := fmt.Sprintf("lottery:state:short_timeout_%d", i)
			err := spm.saveState(ctx, key, state, DefaultStateTTL)
			cancel()
			if err != nil {
				b.Fatalf("Short timeout save failed: %v", err)
			}
		}
	})

	b.Run("LongTimeout_WithRetry", func(b *testing.B) {
		// First attempt fails, second succeeds within timeout
		for i := 0; i < b.N; i++ {
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))
			mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			key := fmt.Sprintf("lottery:state:long_timeout_%d", i)
			err := spm.saveState(ctx, key, state, DefaultStateTTL)
			cancel()
			if err != nil {
				b.Fatalf("Long timeout save failed: %v", err)
			}
		}
	})
}

// createBenchmarkDrawState creates a DrawState for benchmarking with specified parameters
func createBenchmarkDrawState(totalCount, completedCount, prizeCount int) *DrawState {
	// Create results
	results := make([]int, completedCount)
	for i := 0; i < completedCount; i++ {
		results[i] = i + 1
	}

	// Create prize results
	prizeResults := make([]*Prize, prizeCount)
	for i := 0; i < prizeCount; i++ {
		prizeResults[i] = &Prize{
			ID:          fmt.Sprintf("prize_%d", i),
			Name:        fmt.Sprintf("Prize %d", i),
			Probability: 0.1,
			Value:       (i + 1) * 100,
		}
	}

	// Create some errors for realism
	var errors []DrawError
	if completedCount < totalCount {
		errorCount := min(3, totalCount-completedCount)
		for i := 0; i < errorCount; i++ {
			errors = append(errors, DrawError{
				DrawIndex: completedCount + i + 1,
				Error:     ErrLockAcquisitionFailed,
				ErrorMsg:  "Simulated error for benchmark",
				Timestamp: time.Now().Unix(),
			})
		}
	}

	return &DrawState{
		LockKey:        fmt.Sprintf("benchmark_lock_%d_%d", totalCount, completedCount),
		TotalCount:     totalCount,
		CompletedCount: completedCount,
		Results:        results,
		PrizeResults:   prizeResults,
		Errors:         errors,
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
