package lottery

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redismock/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnhancedErrorMessages tests that error messages contain sufficient context
func TestEnhancedErrorMessages(t *testing.T) {
	db, _ := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	t.Run("SaveState_EmptyKey", func(t *testing.T) {
		state := createSimpleTestDrawState("test", 5, 3)
		err := spm.saveState(ctx, "", state, DefaultStateTTL)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key provided")
		assert.Contains(t, err.Error(), "saveState operation")
	})

	t.Run("SaveState_NilState", func(t *testing.T) {
		err := spm.saveState(ctx, "test_key", nil, DefaultStateTTL)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil DrawState provided")
		assert.Contains(t, err.Error(), "saveState operation")
	})

	t.Run("LoadState_EmptyKey", func(t *testing.T) {
		_, err := spm.loadState(ctx, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key provided")
		assert.Contains(t, err.Error(), "loadState operation")
	})

	t.Run("DeleteState_EmptyKey", func(t *testing.T) {
		err := spm.deleteState(ctx, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty key provided")
		assert.Contains(t, err.Error(), "deleteState operation")
	})

	t.Run("FindStateKeys_EmptyLockKey", func(t *testing.T) {
		_, err := spm.findStateKeys(ctx, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty lockKey provided")
		assert.Contains(t, err.Error(), "findStateKeys operation")
	})
}

// TestRetryLogic tests the retry mechanism for Redis operations
func TestRetryLogic(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 2, 1*time.Millisecond)
	ctx := context.Background()

	state := createSimpleTestDrawState("retry_test", 5, 3)

	t.Run("SaveState_SuccessAfterRetry", func(t *testing.T) {
		key := "test_retry_save"

		// First attempt fails with retriable error, second succeeds
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetVal("OK")

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		assert.NoError(t, err)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("SaveState_NonRetriableError", func(t *testing.T) {
		key := "test_non_retriable"

		// Non-retriable error should not be retried
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("invalid command"))

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation failed after 3 attempts")

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("SaveState_MaxRetriesExceeded", func(t *testing.T) {
		key := "test_max_retries"

		// All attempts fail with retriable error
		for i := 0; i <= 2; i++ { // 3 total attempts (initial + 2 retries)
			mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection refused"))
		}

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation failed after 3 attempts")

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("LoadState_SuccessAfterRetry", func(t *testing.T) {
		key := "test_retry_load"
		data, _ := serializeDrawState(state)

		// First attempt fails with retriable error, second succeeds
		mock.ExpectGet(key).SetErr(fmt.Errorf("i/o timeout"))
		mock.ExpectGet(key).SetVal(string(data))

		loadedState, err := spm.loadState(ctx, key)
		assert.NoError(t, err)
		assert.NotNil(t, loadedState)
		assert.Equal(t, state.LockKey, loadedState.LockKey)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DeleteState_SuccessAfterRetry", func(t *testing.T) {
		key := "test_retry_delete"

		// First attempt fails with retriable error, second succeeds
		mock.ExpectDel(key).SetErr(fmt.Errorf("network is unreachable"))
		mock.ExpectDel(key).SetVal(1)

		err := spm.deleteState(ctx, key)
		assert.NoError(t, err)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("FindStateKeys_SuccessAfterRetry", func(t *testing.T) {
		lockKey := "test_retry_find"
		pattern := fmt.Sprintf("%s%s:*", StateKeyPrefix, lockKey)

		// First attempt fails with retriable error, second succeeds
		mock.ExpectKeys(pattern).SetErr(fmt.Errorf("temporary failure"))
		mock.ExpectKeys(pattern).SetVal([]string{"key1", "key2"})

		keys, err := spm.findStateKeys(ctx, lockKey)
		assert.NoError(t, err)
		assert.Len(t, keys, 2)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestIsRetriableRedisError tests the retry error detection logic
func TestIsRetriableRedisError(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		retriable bool
	}{
		{"Nil error", nil, false},
		{"Connection refused", fmt.Errorf("connection refused"), true},
		{"Connection reset", fmt.Errorf("connection reset by peer"), true},
		{"Timeout", fmt.Errorf("operation timeout"), true},
		{"Network unreachable", fmt.Errorf("network is unreachable"), true},
		{"Temporary failure", fmt.Errorf("temporary failure in name resolution"), true},
		{"Server closed", fmt.Errorf("server closed the connection"), true},
		{"Broken pipe", fmt.Errorf("broken pipe"), true},
		{"I/O timeout", fmt.Errorf("i/o timeout"), true},
		{"Invalid command", fmt.Errorf("ERR unknown command"), false},
		{"Wrong type", fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value"), false},
		{"Syntax error", fmt.Errorf("ERR syntax error"), false},
		{"Out of memory", fmt.Errorf("OOM command not allowed when used memory > 'maxmemory'"), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isRetriableRedisError(tc.err)
			assert.Equal(t, tc.retriable, result, "Error: %v", tc.err)
		})
	}
}

// TestContextCancellation tests behavior when context is cancelled
func TestContextCancellation(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 2, 10*time.Millisecond)

	state := createSimpleTestDrawState("context_test", 5, 3)

	t.Run("SaveState_ContextCancelledDuringRetry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		key := "test_context_cancel"

		// First attempt fails, then context gets cancelled
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))

		// Cancel context after first failure
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context cancelled during retry")
	})

	t.Run("LoadState_ContextCancelledDuringRetry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		key := "test_context_cancel_load"

		// First attempt fails, then context gets cancelled
		mock.ExpectGet(key).SetErr(fmt.Errorf("connection timeout"))

		// Cancel context after first failure
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()

		_, err := spm.loadState(ctx, key)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context cancelled during retry")
	})
}

// TestLargeStateSerialization tests handling of large DrawState objects
func TestLargeStateSerialization(t *testing.T) {
	t.Run("MaxSizeState", func(t *testing.T) {
		// Create a state that approaches but doesn't exceed the limit
		largeResults := make([]int, 100000) // Large but reasonable
		for i := range largeResults {
			largeResults[i] = i
		}

		largePrizes := make([]*Prize, 1000)
		for i := range largePrizes {
			largePrizes[i] = &Prize{
				ID:          fmt.Sprintf("prize_%d", i),
				Name:        fmt.Sprintf("Very Long Prize Name %d with lots of description text", i),
				Probability: 0.001,
				Value:       i * 100,
			}
		}

		largeState := &DrawState{
			LockKey:        "large_state_test",
			TotalCount:     100000,
			CompletedCount: 100000,
			Results:        largeResults,
			PrizeResults:   largePrizes,
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// This should succeed
		data, err := serializeDrawState(largeState)
		assert.NoError(t, err)
		assert.True(t, len(data) > 0)
		assert.True(t, len(data) <= MaxSerializationSize, "Serialized size should not exceed limit")

		// Verify round-trip
		deserializedState, err := deserializeDrawState(data)
		assert.NoError(t, err)
		assert.Equal(t, largeState.LockKey, deserializedState.LockKey)
		assert.Equal(t, largeState.TotalCount, deserializedState.TotalCount)
	})

	t.Run("ExcessivelySizeState", func(t *testing.T) {
		// Create a state that would exceed the serialization limit
		// We'll create an artificially large state by adding many large prize names
		hugePrizes := make([]*Prize, 50000)
		for i := range hugePrizes {
			// Create very long prize names to inflate the serialization size
			longName := strings.Repeat(fmt.Sprintf("Very Long Prize Name %d ", i), 50)
			hugePrizes[i] = &Prize{
				ID:          fmt.Sprintf("prize_%d", i),
				Name:        longName,
				Probability: 0.001,
				Value:       i * 100,
			}
		}

		hugeState := &DrawState{
			LockKey:        "huge_state_test",
			TotalCount:     50000,
			CompletedCount: 50000,
			Results:        make([]int, 50000),
			PrizeResults:   hugePrizes,
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// This should fail due to size limit
		_, err := serializeDrawState(hugeState)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
		assert.Contains(t, err.Error(), "huge_state_test")
	})
}

// TestLoadStateSizeLimit tests that loading oversized data is handled properly
func TestLoadStateSizeLimit(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	t.Run("LoadOversizedData", func(t *testing.T) {
		key := "test_oversized_load"

		// Create oversized data (larger than MaxSerializationSize)
		oversizedData := make([]byte, MaxSerializationSize+1000)
		for i := range oversizedData {
			oversizedData[i] = 'x'
		}

		mock.ExpectGet(key).SetVal(string(oversizedData))

		_, err := spm.loadState(ctx, key)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
		assert.Contains(t, err.Error(), key)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestConnectionTimeoutScenarios tests various timeout scenarios
func TestConnectionTimeoutScenarios(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 1, 1*time.Millisecond) // Fast retry for testing
	ctx := context.Background()

	state := createSimpleTestDrawState("timeout_test", 5, 3)

	t.Run("SaveState_ConnectionTimeout", func(t *testing.T) {
		key := "test_connection_timeout"

		// Simulate connection timeout (retriable)
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("dial tcp: i/o timeout"))
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetVal("OK")

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		assert.NoError(t, err)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("LoadState_ReadTimeout", func(t *testing.T) {
		key := "test_read_timeout"
		data, _ := serializeDrawState(state)

		// Simulate read timeout (retriable)
		mock.ExpectGet(key).SetErr(fmt.Errorf("read tcp: i/o timeout"))
		mock.ExpectGet(key).SetVal(string(data))

		loadedState, err := spm.loadState(ctx, key)
		assert.NoError(t, err)
		assert.NotNil(t, loadedState)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DeleteState_WriteTimeout", func(t *testing.T) {
		key := "test_write_timeout"

		// Simulate write timeout (retriable)
		mock.ExpectDel(key).SetErr(fmt.Errorf("write tcp: i/o timeout"))
		mock.ExpectDel(key).SetVal(1)

		err := spm.deleteState(ctx, key)
		assert.NoError(t, err)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestCorruptedDataHandling tests handling of corrupted data scenarios
func TestCorruptedDataHandling(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	testCases := []struct {
		name string
		data string
	}{
		{"InvalidJSON", `{"invalid": json}`},
		{"PartialJSON", `{"lock_key": "test", "total_count"`},
		{"EmptyJSON", `{}`},
		{"NonJSONData", `this is not json at all`},
		{"BinaryData", string([]byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE})},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			key := fmt.Sprintf("test_corrupted_%s", strings.ToLower(tc.name))

			mock.ExpectGet(key).SetVal(tc.data)

			_, err := spm.loadState(ctx, key)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "deserialization failed")
			assert.Contains(t, err.Error(), key)

			// Verify all expectations were met
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestEnhancedRetryLogicEdgeCases tests advanced retry scenarios
func TestEnhancedRetryLogicEdgeCases(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 3, 1*time.Millisecond)
	ctx := context.Background()

	state := createSimpleTestDrawState("enhanced_retry_test", 10, 5)

	t.Run("ExponentialBackoffTiming", func(t *testing.T) {
		key := "test_exponential_backoff"

		// All attempts fail to test backoff timing
		for i := 0; i <= 3; i++ { // 4 total attempts (initial + 3 retries)
			mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection refused"))
		}

		start := time.Now()
		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		elapsed := time.Since(start)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation failed after 4 attempts")

		// With exponential backoff: 1ms + 2ms + 4ms = 7ms minimum
		// Allow some tolerance for test execution overhead
		assert.True(t, elapsed >= 6*time.Millisecond, "Expected at least 6ms for exponential backoff, got %v", elapsed)

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("MaxDelayCapTest", func(t *testing.T) {
		// Test with high retry count to verify delay capping
		spmHighRetry := NewStatePersistenceManagerWithRetry(db, logger, 10, 1*time.Second)
		key := "test_max_delay_cap"

		// Only set up a few expectations since we're testing delay capping
		for i := 0; i < 3; i++ {
			mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))
		}

		start := time.Now()
		err := spmHighRetry.saveState(ctx, key, state, DefaultStateTTL)
		elapsed := time.Since(start)

		require.Error(t, err)

		// Even with high retry count, delay should be capped at 5 seconds per retry
		// With 2 retries, maximum should be around 10 seconds + overhead
		assert.True(t, elapsed < 12*time.Second, "Delay should be capped, got %v", elapsed)

		// Verify expectations were met (may not be all due to early termination)
		mock.ClearExpect()
	})

	t.Run("MixedRetriableNonRetriableErrors", func(t *testing.T) {
		key := "test_mixed_errors"

		// First: retriable error (should retry)
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))
		// Second: non-retriable error (should not retry further)
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("ERR unknown command"))

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation failed after 4 attempts")
		assert.Contains(t, err.Error(), "ERR unknown command")

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestEnhancedErrorContextInformation tests that error messages contain comprehensive context
func TestEnhancedErrorContextInformation(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	t.Run("SaveState_SerializationError_Context", func(t *testing.T) {
		// Create an invalid state that will fail validation
		invalidState := &DrawState{
			LockKey:        "test_serialization_error",
			TotalCount:     -1, // Invalid: negative count
			CompletedCount: 5,
			Results:        []int{1, 2, 3, 4, 5},
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		err := spm.saveState(ctx, "test_key", invalidState, DefaultStateTTL)
		require.Error(t, err)

		// Check that error contains comprehensive context
		assert.Contains(t, err.Error(), "serialization failed")
		assert.Contains(t, err.Error(), "lockKey=test_serialization_error")
		assert.Contains(t, err.Error(), "totalCount=-1")
		assert.Contains(t, err.Error(), "completedCount=5")
		assert.Contains(t, err.Error(), "results=5")
		assert.Contains(t, err.Error(), "serialization_time=")
	})

	t.Run("LoadState_RedisError_Context", func(t *testing.T) {
		key := "test_redis_error_context"

		mock.ExpectGet(key).SetErr(fmt.Errorf("READONLY You can't write against a read only replica"))

		_, err := spm.loadState(ctx, key)
		require.Error(t, err)

		// Check that error contains comprehensive context
		assert.Contains(t, err.Error(), "Redis load operation failed")
		assert.Contains(t, err.Error(), key)
		assert.Contains(t, err.Error(), "load_time=")
		assert.Contains(t, err.Error(), "READONLY")

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("LoadState_DeserializationError_Context", func(t *testing.T) {
		key := "test_deserialization_error_context"
		invalidJSON := `{"lock_key": "test", "total_count": "invalid_number"}`

		mock.ExpectGet(key).SetVal(invalidJSON)

		_, err := spm.loadState(ctx, key)
		require.Error(t, err)

		// Check that error contains comprehensive context
		assert.Contains(t, err.Error(), "deserialization failed")
		assert.Contains(t, err.Error(), key)
		assert.Contains(t, err.Error(), fmt.Sprintf("size=%d bytes", len(invalidJSON)))
		assert.Contains(t, err.Error(), "load_time=")
		assert.Contains(t, err.Error(), "deserialization_time=")

		// Verify all expectations were met
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestAdvancedTimeoutScenarios tests complex timeout and cancellation scenarios
func TestAdvancedTimeoutScenarios(t *testing.T) {
	db, mock := redismock.NewClientMock()
	defer db.Close()

	logger := &DefaultLogger{}
	spm := NewStatePersistenceManagerWithRetry(db, logger, 2, 10*time.Millisecond)

	state := createSimpleTestDrawState("timeout_advanced_test", 10, 5)

	t.Run("ContextCancelledDuringExponentialBackoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		key := "test_cancel_during_backoff"

		// First attempt fails
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))

		// Cancel context during backoff delay
		go func() {
			time.Sleep(5 * time.Millisecond) // Cancel during backoff
			cancel()
		}()

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context cancelled during retry")
		assert.Contains(t, err.Error(), "attempt 1/3")
	})

	t.Run("ContextTimeoutDuringOperation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()

		key := "test_timeout_during_op"

		// Mock a slow operation that will exceed context timeout
		mock.Regexp().ExpectSet(key, `.*`, DefaultStateTTL).SetErr(fmt.Errorf("connection timeout"))

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context cancelled during retry")
	})

	t.Run("VeryShortContextTimeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond) // Extremely short
		defer cancel()

		key := "test_very_short_timeout"

		err := spm.saveState(ctx, key, state, DefaultStateTTL)
		require.Error(t, err)
		// Should fail immediately due to context timeout
		assert.Contains(t, err.Error(), "context")
	})
}

// TestLargeStateSerializationEdgeCases tests edge cases with large state objects
func TestLargeStateSerializationEdgeCases(t *testing.T) {
	t.Run("StateAtExactSizeLimit", func(t *testing.T) {
		// Create a state that serializes to exactly the maximum allowed size
		// This is tricky to achieve exactly, so we'll create a large state and check it's close
		largeResults := make([]int, 50000)
		for i := range largeResults {
			largeResults[i] = i
		}

		// Create prizes with controlled size
		largePrizes := make([]*Prize, 5000)
		for i := range largePrizes {
			// Use a fixed-length name to control serialization size
			largePrizes[i] = &Prize{
				ID:          fmt.Sprintf("prize_%05d", i),
				Name:        fmt.Sprintf("Prize Name %05d", i), // Fixed length
				Probability: 0.001,
				Value:       i * 100,
			}
		}

		largeState := &DrawState{
			LockKey:        "exact_size_limit_test",
			TotalCount:     50000,
			CompletedCount: 50000,
			Results:        largeResults,
			PrizeResults:   largePrizes,
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		data, err := serializeDrawState(largeState)
		if err != nil {
			// If it fails due to size, that's expected behavior
			if strings.Contains(err.Error(), "exceeds maximum allowed size") {
				t.Logf("State correctly rejected for size: %v", err)
				return
			}
			t.Fatalf("Unexpected serialization error: %v", err)
		}

		t.Logf("Large state serialized successfully: %d bytes (limit: %d)", len(data), MaxSerializationSize)
		assert.True(t, len(data) <= MaxSerializationSize)

		// Verify round-trip
		deserializedState, err := deserializeDrawState(data)
		assert.NoError(t, err)
		assert.Equal(t, largeState.LockKey, deserializedState.LockKey)
		assert.Equal(t, largeState.TotalCount, deserializedState.TotalCount)
	})

	t.Run("StateWithManyErrors", func(t *testing.T) {
		// Create a state with many error entries to test serialization limits
		manyErrors := make([]DrawError, 10000)
		for i := range manyErrors {
			manyErrors[i] = DrawError{
				DrawIndex: i + 1,
				Error:     ErrLockAcquisitionFailed,
				ErrorMsg:  fmt.Sprintf("Detailed error message for draw %d with additional context information", i+1),
				Timestamp: time.Now().Unix(),
			}
		}

		stateWithManyErrors := &DrawState{
			LockKey:        "many_errors_test",
			TotalCount:     10000,
			CompletedCount: 0,
			Results:        []int{},
			PrizeResults:   []*Prize{},
			Errors:         manyErrors,
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		data, err := serializeDrawState(stateWithManyErrors)
		if err != nil {
			if strings.Contains(err.Error(), "exceeds maximum allowed size") {
				t.Logf("State with many errors correctly rejected for size: %v", err)
				return
			}
			t.Fatalf("Unexpected serialization error: %v", err)
		}

		t.Logf("State with many errors serialized: %d bytes", len(data))
		assert.True(t, len(data) <= MaxSerializationSize)

		// Verify round-trip
		deserializedState, err := deserializeDrawState(data)
		assert.NoError(t, err)
		assert.Equal(t, len(manyErrors), len(deserializedState.Errors))
	})
}

// createSimpleTestDrawState creates a simple DrawState for testing (renamed to avoid conflicts)
func createSimpleTestDrawState(lockKey string, totalCount, completedCount int) *DrawState {
	results := make([]int, completedCount)
	for i := range completedCount {
		results[i] = i + 1
	}

	return &DrawState{
		LockKey:        lockKey,
		TotalCount:     totalCount,
		CompletedCount: completedCount,
		Results:        results,
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}
}
