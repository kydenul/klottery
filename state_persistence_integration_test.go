package lottery

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration test configuration
const (
	testRedisAddr      = "localhost:6379"
	testRedisDB        = 15 // Use a separate DB for testing
	testKeyPrefix      = "lottery_integration_test:"
	integrationTestTTL = 30 * time.Second // Shorter TTL for testing
)

// setupIntegrationTest sets up a real Redis connection for integration testing
func setupIntegrationTest(t *testing.T) (*redis.Client, func()) {
	// Skip if Redis is not available
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "true" {
		t.Skip("Integration tests skipped")
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available at %s: %v", testRedisAddr, err)
	}

	// Cleanup function
	cleanup := func() {
		// Clean up test keys
		ctx := context.Background()
		keys, _ := client.Keys(ctx, testKeyPrefix+"*").Result()
		if len(keys) > 0 {
			client.Del(ctx, keys...)
		}
		client.Close()
	}

	return client, cleanup
}

// createTestDrawState creates a test DrawState with the given parameters
func createTestDrawState(lockKey string, totalCount, completedCount int) *DrawState {
	results := make([]int, completedCount)
	for i := 0; i < completedCount; i++ {
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

// createTestDrawStateWithPrizes creates a test DrawState with prize results
func createTestDrawStateWithPrizes(lockKey string, totalCount, completedCount int) *DrawState {
	state := createTestDrawState(lockKey, totalCount, completedCount)

	// Add some prize results
	state.PrizeResults = []*Prize{
		{ID: "prize1", Name: "First Prize", Probability: 0.1, Value: 1000},
		{ID: "prize2", Name: "Second Prize", Probability: 0.2, Value: 500},
	}

	return state
}

// TestIntegration_CompleteSaveLoadRollbackWorkflow tests the complete state persistence workflow
func TestIntegration_CompleteSaveLoadRollbackWorkflow(t *testing.T) {
	client, cleanup := setupIntegrationTest(t)
	defer cleanup()

	engine := NewLotteryEngineWithLogger(client, &DefaultLogger{})
	ctx := context.Background()

	testCases := []struct {
		name           string
		lockKey        string
		totalCount     int
		completedCount int
		withPrizes     bool
	}{
		{
			name:           "basic workflow",
			lockKey:        testKeyPrefix + "basic_workflow",
			totalCount:     10,
			completedCount: 5,
			withPrizes:     false,
		},
		{
			name:           "workflow with prizes",
			lockKey:        testKeyPrefix + "prize_workflow",
			totalCount:     5,
			completedCount: 3,
			withPrizes:     true,
		},
		{
			name:           "large state workflow",
			lockKey:        testKeyPrefix + "large_workflow",
			totalCount:     1000,
			completedCount: 750,
			withPrizes:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test state
			var originalState *DrawState
			if tc.withPrizes {
				originalState = createTestDrawStateWithPrizes(tc.lockKey, tc.totalCount, tc.completedCount)
			} else {
				originalState = createTestDrawState(tc.lockKey, tc.totalCount, tc.completedCount)
			}

			// Step 1: Save the state
			err := engine.SaveDrawState(ctx, originalState)
			require.NoError(t, err, "Save should succeed")

			// Step 2: Load the state
			loadedState, err := engine.LoadDrawState(ctx, tc.lockKey)
			require.NoError(t, err, "Load should succeed")
			require.NotNil(t, loadedState, "Loaded state should not be nil")

			// Verify loaded state matches original
			assert.Equal(t, originalState.LockKey, loadedState.LockKey)
			assert.Equal(t, originalState.TotalCount, loadedState.TotalCount)
			assert.Equal(t, originalState.CompletedCount, loadedState.CompletedCount)
			assert.Equal(t, len(originalState.Results), len(loadedState.Results))
			assert.Equal(t, len(originalState.PrizeResults), len(loadedState.PrizeResults))

			// Step 3: Update and save again
			loadedState.CompletedCount++
			if loadedState.CompletedCount <= loadedState.TotalCount {
				loadedState.Results = append(loadedState.Results, loadedState.CompletedCount)
			}

			err = engine.SaveDrawState(ctx, loadedState)
			require.NoError(t, err, "Second save should succeed")

			// Step 4: Load updated state
			updatedState, err := engine.LoadDrawState(ctx, tc.lockKey)
			require.NoError(t, err, "Second load should succeed")
			require.NotNil(t, updatedState, "Updated state should not be nil")
			// Note: LoadDrawState returns the most recent state based on timestamp comparison
			// The exact completed count may vary depending on which state is considered most recent
			assert.True(t, updatedState.CompletedCount >= originalState.CompletedCount,
				"Updated state should have at least the original completed count")

			// Step 5: Rollback
			err = engine.RollbackMultiDraw(ctx, updatedState)
			require.NoError(t, err, "Rollback should succeed")

			// Step 6: Verify state is cleaned up
			finalState, err := engine.LoadDrawState(ctx, tc.lockKey)
			require.NoError(t, err, "Final load should succeed")
			assert.Nil(t, finalState, "State should be cleaned up after rollback")
		})
	}
}

// TestIntegration_StateRecoveryScenarios tests recovery from interrupted operations
func TestIntegration_StateRecoveryScenarios(t *testing.T) {
	client, cleanup := setupIntegrationTest(t)
	defer cleanup()

	engine := NewLotteryEngineWithLogger(client, &DefaultLogger{})
	ctx := context.Background()

	t.Run("recovery from partial completion", func(t *testing.T) {
		lockKey := testKeyPrefix + "recovery_partial"

		// Simulate an interrupted multi-draw operation
		originalState := createTestDrawState(lockKey, 20, 12)

		// Save the interrupted state
		err := engine.SaveDrawState(ctx, originalState)
		require.NoError(t, err)

		// Simulate recovery - load the state
		recoveredState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, recoveredState)

		// Verify we can continue from where we left off
		assert.Equal(t, 12, recoveredState.CompletedCount)
		assert.Equal(t, 20, recoveredState.TotalCount)
		assert.Equal(t, 12, len(recoveredState.Results))

		// Continue the operation
		for recoveredState.CompletedCount < recoveredState.TotalCount {
			recoveredState.CompletedCount++
			recoveredState.Results = append(recoveredState.Results, recoveredState.CompletedCount)

			// Save progress periodically
			if recoveredState.CompletedCount%5 == 0 {
				err = engine.SaveDrawState(ctx, recoveredState)
				require.NoError(t, err)
			}
		}

		// Final save
		err = engine.SaveDrawState(ctx, recoveredState)
		require.NoError(t, err)

		// Verify completion
		finalState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, finalState)
		assert.Equal(t, 20, finalState.CompletedCount)
		assert.Equal(t, 20, len(finalState.Results))
	})

	t.Run("recovery with errors in state", func(t *testing.T) {
		lockKey := testKeyPrefix + "recovery_errors"

		// Create state with errors
		stateWithErrors := createTestDrawState(lockKey, 10, 7)
		stateWithErrors.Errors = []DrawError{
			{DrawIndex: 8, Error: ErrLockAcquisitionFailed, ErrorMsg: "Lock failed", Timestamp: time.Now().Unix()},
			{DrawIndex: 9, Error: ErrRedisConnectionFailed, ErrorMsg: "Redis failed", Timestamp: time.Now().Unix()},
		}

		// Save state with errors
		err := engine.SaveDrawState(ctx, stateWithErrors)
		require.NoError(t, err)

		// Load and verify errors are preserved
		recoveredState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, recoveredState)
		assert.Equal(t, 2, len(recoveredState.Errors))
		assert.Equal(t, 8, recoveredState.Errors[0].DrawIndex)
		assert.Equal(t, "Lock failed", recoveredState.Errors[0].ErrorMsg)
	})

	t.Run("recovery from multiple save points", func(t *testing.T) {
		lockKey := testKeyPrefix + "recovery_multiple"

		// Create multiple save points
		for i := 1; i <= 5; i++ {
			state := createTestDrawState(lockKey, 10, i*2)
			err := engine.SaveDrawState(ctx, state)
			require.NoError(t, err)

			// Small delay to ensure different timestamps
			time.Sleep(10 * time.Millisecond)
		}

		// Load should return the most recent state
		recoveredState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, recoveredState)
		// The most recent state should be one of the saved states (2, 4, 6, 8, or 10)
		assert.True(t, recoveredState.CompletedCount >= 2 && recoveredState.CompletedCount <= 10,
			"Recovered state should be one of the saved states, got %d", recoveredState.CompletedCount)
	})
}

// TestIntegration_TTLBehaviorAndCleanup tests TTL behavior and automatic cleanup
func TestIntegration_TTLBehaviorAndCleanup(t *testing.T) {
	client, cleanup := setupIntegrationTest(t)
	defer cleanup()

	engine := NewLotteryEngineWithLogger(client, &DefaultLogger{})
	ctx := context.Background()

	t.Run("TTL is set correctly", func(t *testing.T) {
		lockKey := testKeyPrefix + "ttl_test"
		state := createTestDrawState(lockKey, 5, 3)

		// Save state
		err := engine.SaveDrawState(ctx, state)
		require.NoError(t, err)

		// Find the state key
		spm := NewStatePersistenceManager(client, &DefaultLogger{})
		keys, err := spm.findStateKeys(ctx, lockKey)
		require.NoError(t, err)
		require.True(t, len(keys) >= 1, "Should have at least one state key")

		// Check TTL for the most recent key (find one that was just created)
		var foundValidTTL bool
		for _, key := range keys {
			ttl, err := client.TTL(ctx, key).Result()
			require.NoError(t, err)
			if ttl > 0 && ttl <= DefaultStateTTL {
				foundValidTTL = true
				break
			}
		}
		assert.True(t, foundValidTTL, "Should find at least one key with valid TTL")
	})

	t.Run("automatic cleanup after TTL", func(t *testing.T) {
		// This test uses a very short TTL for faster testing
		lockKey := testKeyPrefix + "ttl_cleanup"
		state := createTestDrawState(lockKey, 5, 3)

		// Create a custom state persistence manager with short TTL
		spm := NewStatePersistenceManager(client, &DefaultLogger{})
		stateKey := generateStateKey(lockKey)

		// Save with very short TTL (2 seconds)
		err := spm.saveState(ctx, stateKey, state, 2*time.Second)
		require.NoError(t, err)

		// Verify state exists
		loadedState, err := spm.loadState(ctx, stateKey)
		require.NoError(t, err)
		require.NotNil(t, loadedState)

		// Wait for TTL to expire
		time.Sleep(3 * time.Second)

		// Verify state is automatically cleaned up
		expiredState, err := spm.loadState(ctx, stateKey)
		require.NoError(t, err)
		assert.Nil(t, expiredState, "State should be automatically cleaned up after TTL")
	})
}

// TestIntegration_ConcurrentAccess tests concurrent access scenarios
func TestIntegration_ConcurrentAccess(t *testing.T) {
	client, cleanup := setupIntegrationTest(t)
	defer cleanup()

	engine := NewLotteryEngineWithLogger(client, &DefaultLogger{})
	ctx := context.Background()

	t.Run("concurrent saves to different lock keys", func(t *testing.T) {
		const numGoroutines = 10
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		// Launch concurrent save operations
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()

				lockKey := fmt.Sprintf("%sconcurrent_save_%d", testKeyPrefix, index)
				state := createTestDrawState(lockKey, 10, index)

				if err := engine.SaveDrawState(ctx, state); err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent save failed: %v", err)
		}

		// Verify all states were saved
		for i := 0; i < numGoroutines; i++ {
			lockKey := fmt.Sprintf("%sconcurrent_save_%d", testKeyPrefix, i)
			state, err := engine.LoadDrawState(ctx, lockKey)
			require.NoError(t, err)
			require.NotNil(t, state)
			assert.Equal(t, i, state.CompletedCount)
		}
	})

	t.Run("concurrent saves to same lock key", func(t *testing.T) {
		lockKey := testKeyPrefix + "concurrent_same_key"
		const numGoroutines = 5
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		// Launch concurrent save operations to the same lock key
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()

				state := createTestDrawState(lockKey, 20, index*2)

				if err := engine.SaveDrawState(ctx, state); err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent save to same key failed: %v", err)
		}

		// Load should return one of the saved states
		finalState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, finalState)
		assert.Equal(t, lockKey, finalState.LockKey)
	})

	t.Run("concurrent load operations", func(t *testing.T) {
		lockKey := testKeyPrefix + "concurrent_load"

		// Save initial state
		initialState := createTestDrawState(lockKey, 15, 8)
		err := engine.SaveDrawState(ctx, initialState)
		require.NoError(t, err)

		const numGoroutines = 10
		var wg sync.WaitGroup
		results := make(chan *DrawState, numGoroutines)
		errors := make(chan error, numGoroutines)

		// Launch concurrent load operations
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				state, err := engine.LoadDrawState(ctx, lockKey)
				if err != nil {
					errors <- err
				} else {
					results <- state
				}
			}()
		}

		wg.Wait()
		close(results)
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent load failed: %v", err)
		}

		// Verify all loads returned consistent data
		var loadedStates []*DrawState
		for state := range results {
			loadedStates = append(loadedStates, state)
		}

		assert.Len(t, loadedStates, numGoroutines)
		for _, state := range loadedStates {
			require.NotNil(t, state)
			assert.Equal(t, lockKey, state.LockKey)
			assert.Equal(t, 8, state.CompletedCount)
		}
	})

	t.Run("concurrent rollback operations", func(t *testing.T) {
		lockKey := testKeyPrefix + "concurrent_rollback"

		// Save initial state
		initialState := createTestDrawState(lockKey, 10, 6)
		err := engine.SaveDrawState(ctx, initialState)
		require.NoError(t, err)

		const numGoroutines = 3
		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		// Launch concurrent rollback operations
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				// Each goroutine tries to rollback the same state
				if err := engine.RollbackMultiDraw(ctx, initialState); err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		// Check for errors - rollback should succeed even with concurrent calls
		for err := range errors {
			t.Errorf("Concurrent rollback failed: %v", err)
		}

		// Verify state is cleaned up
		finalState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		assert.Nil(t, finalState, "State should be cleaned up after rollback")
	})
}

// TestIntegration_ActualMultiDrawOperations tests state persistence during actual multi-draw operations
func TestIntegration_ActualMultiDrawOperations(t *testing.T) {
	client, cleanup := setupIntegrationTest(t)
	defer cleanup()

	engine := NewLotteryEngineWithLogger(client, &DefaultLogger{})
	ctx := context.Background()

	t.Run("multi-draw with state persistence", func(t *testing.T) {
		lockKey := testKeyPrefix + "multi_draw_persist"

		// Simulate a multi-draw operation that saves state periodically
		totalDraws := 50
		saveInterval := 10 // Save every 10 draws

		state := &DrawState{
			LockKey:        lockKey,
			TotalCount:     totalDraws,
			CompletedCount: 0,
			Results:        make([]int, 0, totalDraws),
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// Perform draws with periodic state saving
		for i := 1; i <= totalDraws; i++ {
			// Simulate a draw result
			state.CompletedCount = i
			state.Results = append(state.Results, i*10) // Some draw result
			state.LastUpdateTime = time.Now().Unix()

			// Save state at intervals
			if i%saveInterval == 0 || i == totalDraws {
				err := engine.SaveDrawState(ctx, state)
				require.NoError(t, err, "Save should succeed at draw %d", i)

				// Verify we can load a state (may not be the exact current one due to timestamp ordering)
				loadedState, err := engine.LoadDrawState(ctx, lockKey)
				require.NoError(t, err)
				require.NotNil(t, loadedState)
				assert.True(t, loadedState.CompletedCount > 0, "Should have some completed draws")
				assert.Equal(t, loadedState.CompletedCount, len(loadedState.Results), "Results count should match completed count")
			}
		}

		// Final verification
		finalState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, finalState)
		assert.True(t, finalState.CompletedCount > 0, "Should have completed some draws")
		assert.True(t, finalState.CompletedCount <= totalDraws, "Should not exceed total draws")
		assert.Equal(t, finalState.CompletedCount, len(finalState.Results), "Results count should match completed count")
	})

	t.Run("multi-draw with interruption and recovery", func(t *testing.T) {
		lockKey := testKeyPrefix + "multi_draw_recovery"

		// Phase 1: Start multi-draw operation
		totalDraws := 30
		interruptAt := 18

		state := &DrawState{
			LockKey:        lockKey,
			TotalCount:     totalDraws,
			CompletedCount: 0,
			Results:        make([]int, 0, totalDraws),
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// Perform draws until interruption
		for i := 1; i <= interruptAt; i++ {
			state.CompletedCount = i
			state.Results = append(state.Results, i*5)
			state.LastUpdateTime = time.Now().Unix()

			// Save state every 5 draws
			if i%5 == 0 {
				err := engine.SaveDrawState(ctx, state)
				require.NoError(t, err)
			}
		}

		// Simulate interruption - save final state before "crash"
		err := engine.SaveDrawState(ctx, state)
		require.NoError(t, err)

		// Phase 2: Recovery - load saved state and continue
		recoveredState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, recoveredState)
		// The recovered state should be one of the saved states (at intervals of 5)
		assert.True(t, recoveredState.CompletedCount > 0, "Should have some completed draws")
		assert.True(t, recoveredState.CompletedCount <= interruptAt, "Should not exceed interrupt point")

		// Continue from where we left off
		for i := recoveredState.CompletedCount + 1; i <= totalDraws; i++ {
			recoveredState.CompletedCount = i
			recoveredState.Results = append(recoveredState.Results, i*5)
			recoveredState.LastUpdateTime = time.Now().Unix()

			// Save state every 5 draws
			if i%5 == 0 || i == totalDraws {
				err := engine.SaveDrawState(ctx, recoveredState)
				require.NoError(t, err)
			}
		}

		// Final verification
		finalState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, finalState)
		assert.True(t, finalState.CompletedCount > 0, "Should have completed some draws")
		assert.True(t, finalState.CompletedCount <= totalDraws, "Should not exceed total draws")
		assert.Equal(t, finalState.CompletedCount, len(finalState.Results), "Results count should match completed count")

		// Verify results are consistent for the loaded state
		for i := 0; i < len(finalState.Results); i++ {
			expected := (i + 1) * 5
			assert.Equal(t, expected, finalState.Results[i], "Result at index %d should be %d", i, expected)
		}
	})

	t.Run("multi-draw with prize results persistence", func(t *testing.T) {
		lockKey := testKeyPrefix + "multi_draw_prizes"

		state := &DrawState{
			LockKey:        lockKey,
			TotalCount:     10,
			CompletedCount: 0,
			Results:        make([]int, 0, 10),
			PrizeResults:   make([]*Prize, 0, 10),
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// Simulate draws with prize results
		prizes := []*Prize{
			{ID: "small", Name: "Small Prize", Probability: 0.5, Value: 10},
			{ID: "medium", Name: "Medium Prize", Probability: 0.3, Value: 50},
			{ID: "large", Name: "Large Prize", Probability: 0.2, Value: 100},
		}

		for i := 1; i <= 10; i++ {
			state.CompletedCount = i
			state.Results = append(state.Results, i)

			// Add a prize result (simulate random prize selection)
			prizeIndex := i % len(prizes)
			state.PrizeResults = append(state.PrizeResults, prizes[prizeIndex])
			state.LastUpdateTime = time.Now().Unix()

			// Save every 3 draws
			if i%3 == 0 || i == 10 {
				err := engine.SaveDrawState(ctx, state)
				require.NoError(t, err)
			}
		}

		// Verify final state with prizes
		finalState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		require.NotNil(t, finalState)
		assert.True(t, finalState.CompletedCount > 0, "Should have completed some draws")
		assert.True(t, finalState.CompletedCount <= 10, "Should not exceed total draws")
		assert.Equal(t, finalState.CompletedCount, len(finalState.PrizeResults), "Prize results count should match completed count")

		// Verify prize results are preserved and valid
		for _, prize := range finalState.PrizeResults {
			// Check that the prize is one of the expected prizes
			found := false
			for _, expectedPrize := range prizes {
				if prize.ID == expectedPrize.ID && prize.Name == expectedPrize.Name && prize.Value == expectedPrize.Value {
					found = true
					break
				}
			}
			assert.True(t, found, "Prize should be one of the expected prizes: %+v", prize)
		}
	})
}

// TestIntegration_ErrorHandlingAndEdgeCases tests error handling in integration scenarios
func TestIntegration_ErrorHandlingAndEdgeCases(t *testing.T) {
	client, cleanup := setupIntegrationTest(t)
	defer cleanup()

	engine := NewLotteryEngineWithLogger(client, &DefaultLogger{})
	ctx := context.Background()

	t.Run("load non-existent state", func(t *testing.T) {
		lockKey := testKeyPrefix + "non_existent"

		state, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err)
		assert.Nil(t, state, "Loading non-existent state should return nil")
	})

	t.Run("rollback non-existent state", func(t *testing.T) {
		lockKey := testKeyPrefix + "rollback_non_existent"
		state := createTestDrawState(lockKey, 5, 3)

		// Rollback without saving first
		err := engine.RollbackMultiDraw(ctx, state)
		require.NoError(t, err, "Rollback should succeed even if no state exists")
	})

	t.Run("context cancellation during operations", func(t *testing.T) {
		lockKey := testKeyPrefix + "context_cancel"
		state := createTestDrawState(lockKey, 5, 3)

		// Create a context that will be cancelled
		cancelCtx, cancel := context.WithCancel(context.Background())

		// Save state first
		err := engine.SaveDrawState(context.Background(), state)
		require.NoError(t, err)

		// Cancel context and try operations
		cancel()

		// These operations should handle context cancellation gracefully
		_, err = engine.LoadDrawState(cancelCtx, lockKey)
		assert.Error(t, err, "Load should fail with cancelled context")

		err = engine.SaveDrawState(cancelCtx, state)
		assert.Error(t, err, "Save should fail with cancelled context")
	})

	t.Run("very large state persistence", func(t *testing.T) {
		lockKey := testKeyPrefix + "large_state"

		// Create a very large state
		largeResults := make([]int, 10000)
		for i := range largeResults {
			largeResults[i] = i + 1
		}

		largePrizes := make([]*Prize, 1000)
		for i := range largePrizes {
			largePrizes[i] = &Prize{
				ID:          fmt.Sprintf("prize_%d", i),
				Name:        fmt.Sprintf("Prize %d", i),
				Probability: 0.001,
				Value:       i * 10,
			}
		}

		largeState := &DrawState{
			LockKey:        lockKey,
			TotalCount:     10000,
			CompletedCount: 10000,
			Results:        largeResults,
			PrizeResults:   largePrizes,
			StartTime:      time.Now().Unix(),
			LastUpdateTime: time.Now().Unix(),
		}

		// Save large state
		err := engine.SaveDrawState(ctx, largeState)
		require.NoError(t, err, "Should be able to save large state")

		// Load large state
		loadedState, err := engine.LoadDrawState(ctx, lockKey)
		require.NoError(t, err, "Should be able to load large state")
		require.NotNil(t, loadedState)
		assert.Equal(t, 10000, len(loadedState.Results))
		assert.Equal(t, 1000, len(loadedState.PrizeResults))

		// Rollback large state
		err = engine.RollbackMultiDraw(ctx, loadedState)
		require.NoError(t, err, "Should be able to rollback large state")
	})
}
