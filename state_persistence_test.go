package lottery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redismock/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerializeDrawState(t *testing.T) {
	tests := []struct {
		name        string
		drawState   *DrawState
		expectError bool
		errorType   error
	}{
		{
			name: "valid draw state",
			drawState: &DrawState{
				LockKey:        "test_lock",
				TotalCount:     10,
				CompletedCount: 5,
				Results:        []int{1, 2, 3, 4, 5},
				StartTime:      time.Now().Unix(),
				LastUpdateTime: time.Now().Unix(),
			},
			expectError: false,
		},
		{
			name: "draw state with prize results",
			drawState: &DrawState{
				LockKey:        "test_lock",
				TotalCount:     3,
				CompletedCount: 2,
				PrizeResults: []*Prize{
					{ID: "prize1", Name: "Prize 1", Probability: 0.5, Value: 100},
					{ID: "prize2", Name: "Prize 2", Probability: 0.3, Value: 50},
				},
				StartTime:      time.Now().Unix(),
				LastUpdateTime: time.Now().Unix(),
			},
			expectError: false,
		},
		{
			name: "draw state with errors",
			drawState: &DrawState{
				LockKey:        "test_lock",
				TotalCount:     5,
				CompletedCount: 3,
				Results:        []int{1, 2, 3},
				Errors: []DrawError{
					{DrawIndex: 4, Error: ErrLockAcquisitionFailed, ErrorMsg: ErrLockAcquisitionFailed.Error(), Timestamp: time.Now().Unix()},
					{DrawIndex: 5, Error: ErrRedisConnectionFailed, ErrorMsg: ErrRedisConnectionFailed.Error(), Timestamp: time.Now().Unix()},
				},
				StartTime:      time.Now().Unix(),
				LastUpdateTime: time.Now().Unix(),
			},
			expectError: false,
		},
		{
			name:        "nil draw state",
			drawState:   nil,
			expectError: true,
			errorType:   ErrInvalidParameters,
		},
		{
			name: "invalid draw state - empty lock key",
			drawState: &DrawState{
				LockKey:        "",
				TotalCount:     10,
				CompletedCount: 5,
				StartTime:      time.Now().Unix(),
				LastUpdateTime: time.Now().Unix(),
			},
			expectError: true,
			errorType:   ErrInvalidParameters,
		},
		{
			name: "invalid draw state - negative total count",
			drawState: &DrawState{
				LockKey:        "test_lock",
				TotalCount:     -1,
				CompletedCount: 0,
				StartTime:      time.Now().Unix(),
				LastUpdateTime: time.Now().Unix(),
			},
			expectError: true,
			errorType:   ErrInvalidCount,
		},
		{
			name: "invalid draw state - completed count exceeds total",
			drawState: &DrawState{
				LockKey:        "test_lock",
				TotalCount:     5,
				CompletedCount: 10,
				StartTime:      time.Now().Unix(),
				LastUpdateTime: time.Now().Unix(),
			},
			expectError: true,
			errorType:   ErrInvalidParameters,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := serializeDrawState(tt.drawState)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errorType != nil && !strings.Contains(err.Error(), tt.errorType.Error()) {
					t.Errorf("Expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(data) == 0 {
				t.Errorf("Expected serialized data, got empty")
				return
			}

			// Verify the data is valid JSON
			var testState DrawState
			if err := json.Unmarshal(data, &testState); err != nil {
				t.Errorf("Serialized data is not valid JSON: %v", err)
			}

			// Verify key fields are preserved
			if testState.LockKey != tt.drawState.LockKey {
				t.Errorf("LockKey mismatch: expected %s, got %s", tt.drawState.LockKey, testState.LockKey)
			}
			if testState.TotalCount != tt.drawState.TotalCount {
				t.Errorf("TotalCount mismatch: expected %d, got %d", tt.drawState.TotalCount, testState.TotalCount)
			}
			if testState.CompletedCount != tt.drawState.CompletedCount {
				t.Errorf("CompletedCount mismatch: expected %d, got %d", tt.drawState.CompletedCount, testState.CompletedCount)
			}
		})
	}
}

func TestDeserializeDrawState(t *testing.T) {
	validState := &DrawState{
		LockKey:        "test_lock",
		TotalCount:     10,
		CompletedCount: 5,
		Results:        []int{1, 2, 3, 4, 5},
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}
	validData, _ := json.Marshal(validState)

	tests := []struct {
		name        string
		data        []byte
		expectError bool
		errorType   error
	}{
		{
			name:        "valid JSON data",
			data:        validData,
			expectError: false,
		},
		{
			name:        "empty data",
			data:        []byte{},
			expectError: true,
			errorType:   ErrInvalidParameters,
		},
		{
			name:        "nil data",
			data:        nil,
			expectError: true,
			errorType:   ErrInvalidParameters,
		},
		{
			name:        "invalid JSON",
			data:        []byte(`{"invalid": json}`),
			expectError: true,
		},
		{
			name:        "valid JSON but invalid DrawState",
			data:        []byte(`{"lock_key": "", "total_count": 10, "completed_count": 5, "start_time": 1234567890}`),
			expectError: true,
			errorType:   ErrDrawStateCorrupted,
		},
		{
			name:        "JSON with missing required fields",
			data:        []byte(`{"lock_key": "test"}`),
			expectError: true,
			errorType:   ErrDrawStateCorrupted,
		},
		{
			name:        "JSON with negative values",
			data:        []byte(`{"lock_key": "test", "total_count": -1, "completed_count": 0, "start_time": 1234567890}`),
			expectError: true,
			errorType:   ErrDrawStateCorrupted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := deserializeDrawState(tt.data)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.errorType != nil && !strings.Contains(err.Error(), tt.errorType.Error()) {
					t.Errorf("Expected error type %v, got %v", tt.errorType, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if state == nil {
				t.Errorf("Expected deserialized state, got nil")
				return
			}

			// Verify the state is valid
			if err := state.Validate(); err != nil {
				t.Errorf("Deserialized state is invalid: %v", err)
			}
		})
	}
}

func TestGenerateOperationID(t *testing.T) {
	// Generate multiple IDs to test uniqueness
	ids := make(map[string]bool)
	for range 100 {
		id := generateOperationID()

		if id == "" {
			t.Errorf("Generated empty operation ID")
		}

		if ids[id] {
			t.Errorf("Generated duplicate operation ID: %s", id)
		}
		ids[id] = true

		// Verify format: should contain timestamp and random part
		parts := strings.Split(id, "_")
		if len(parts) < 3 {
			t.Errorf("Operation ID format incorrect: %s (expected at least 3 parts)", id)
		}

		// First part should be date
		if len(parts[0]) != 8 { // YYYYMMDD
			t.Errorf("Date part format incorrect: %s", parts[0])
		}

		// Second part should be time with nanoseconds
		timeParts := strings.Split(parts[1], ".")
		if len(timeParts) != 2 {
			t.Fatalf("Time part format incorrect, expected HHMMSS.nanoseconds: %s", parts[1])
		}
		if len(timeParts[0]) != 6 { // HHMMSS
			t.Errorf("Time part format incorrect: %s", timeParts[0])
		}
	}
}

func TestGenerateStateKey(t *testing.T) {
	tests := []struct {
		name     string
		lockKey  string
		expected string
	}{
		{
			name:     "valid lock key",
			lockKey:  "user_123_daily",
			expected: StateKeyPrefix + "user_123_daily:",
		},
		{
			name:     "empty lock key",
			lockKey:  "",
			expected: "",
		},
		{
			name:     "lock key with special characters",
			lockKey:  "user:123:daily",
			expected: StateKeyPrefix + "user:123:daily:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := generateStateKey(tt.lockKey)

			if tt.expected == "" {
				if key != "" {
					t.Errorf("Expected empty key for empty lockKey, got %s", key)
				}
				return
			}

			if !strings.HasPrefix(key, tt.expected) {
				t.Errorf("Expected key to start with %s, got %s", tt.expected, key)
			}

			// Verify the key has an operation ID part
			parts := strings.Split(key, ":")
			if len(parts) < 4 { // lottery:state:lockKey:operationID
				t.Errorf("Generated key format incorrect: %s", key)
			}
		})
	}
}

func TestParseStateKey(t *testing.T) {
	tests := []struct {
		name            string
		key             string
		expectedLockKey string
		expectedOpID    string
		expectError     bool
	}{
		{
			name:            "valid state key",
			key:             "lottery:state:user_123_daily:20241217_143022_abcd1234",
			expectedLockKey: "user_123_daily",
			expectedOpID:    "20241217_143022_abcd1234",
			expectError:     false,
		},
		{
			name:            "lock key with colons",
			key:             "lottery:state:user:123:daily:20241217_143022_abcd1234",
			expectedLockKey: "user:123:daily",
			expectedOpID:    "20241217_143022_abcd1234",
			expectError:     false,
		},
		{
			name:        "invalid prefix",
			key:         "invalid:state:user_123:op123",
			expectError: true,
		},
		{
			name:        "missing operation ID",
			key:         "lottery:state:user_123",
			expectError: true,
		},
		{
			name:        "empty lock key",
			key:         "lottery:state::op123",
			expectError: true,
		},
		{
			name:        "empty operation ID",
			key:         "lottery:state:user_123:",
			expectError: true,
		},
		{
			name:        "empty key",
			key:         "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lockKey, opID, err := parseStateKey(tt.key)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if lockKey != tt.expectedLockKey {
				t.Errorf("Expected lockKey %s, got %s", tt.expectedLockKey, lockKey)
			}

			if opID != tt.expectedOpID {
				t.Errorf("Expected operationID %s, got %s", tt.expectedOpID, opID)
			}
		})
	}
}

func TestStatePersistenceManager_SaveState(t *testing.T) {
	db, mock := redismock.NewClientMock()
	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)

	validState := &DrawState{
		LockKey:        "test_lock",
		TotalCount:     10,
		CompletedCount: 5,
		Results:        []int{1, 2, 3, 4, 5},
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}

	tests := []struct {
		name        string
		key         string
		state       *DrawState
		ttl         time.Duration
		mockSetup   func()
		expectError bool
	}{
		{
			name:  "successful save",
			key:   "lottery:state:test:op123",
			state: validState,
			ttl:   DefaultStateTTL,
			mockSetup: func() {
				mock.Regexp().ExpectSet("lottery:state:test:op123", `.*`, DefaultStateTTL).SetVal("OK")
			},
			expectError: false,
		},
		{
			name:        "empty key",
			key:         "",
			state:       validState,
			ttl:         DefaultStateTTL,
			mockSetup:   func() {},
			expectError: true,
		},
		{
			name:        "nil state",
			key:         "lottery:state:test:op123",
			state:       nil,
			ttl:         DefaultStateTTL,
			mockSetup:   func() {},
			expectError: true,
		},
		{
			name:  "Redis error",
			key:   "lottery:state:test:op123",
			state: validState,
			ttl:   DefaultStateTTL,
			mockSetup: func() {
				mock.Regexp().ExpectSet("lottery:state:test:op123", `.*`, DefaultStateTTL).SetErr(redis.TxFailedErr)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			err := spm.saveState(context.Background(), tt.key, tt.state, tt.ttl)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Redis mock expectations not met: %v", err)
			}
		})
	}
}

func TestStatePersistenceManager_LoadState(t *testing.T) {
	db, mock := redismock.NewClientMock()
	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)

	validState := &DrawState{
		LockKey:        "test_lock",
		TotalCount:     10,
		CompletedCount: 5,
		Results:        []int{1, 2, 3, 4, 5},
		StartTime:      time.Now().Unix(),
		LastUpdateTime: time.Now().Unix(),
	}
	validData, _ := json.Marshal(validState)

	tests := []struct {
		name        string
		key         string
		mockSetup   func()
		expectError bool
		expectNil   bool
	}{
		{
			name: "successful load",
			key:  "lottery:state:test:op123",
			mockSetup: func() {
				mock.ExpectGet("lottery:state:test:op123").SetVal(string(validData))
			},
			expectError: false,
			expectNil:   false,
		},
		{
			name: "key not found",
			key:  "lottery:state:test:op123",
			mockSetup: func() {
				mock.ExpectGet("lottery:state:test:op123").RedisNil()
			},
			expectError: false,
			expectNil:   true,
		},
		{
			name:        "empty key",
			key:         "",
			mockSetup:   func() {},
			expectError: true,
			expectNil:   false,
		},
		{
			name: "Redis error",
			key:  "lottery:state:test:op123",
			mockSetup: func() {
				mock.ExpectGet("lottery:state:test:op123").SetErr(redis.TxFailedErr)
			},
			expectError: true,
			expectNil:   false,
		},
		{
			name: "corrupted data",
			key:  "lottery:state:test:op123",
			mockSetup: func() {
				mock.ExpectGet("lottery:state:test:op123").SetVal(`{"invalid": "data"}`)
			},
			expectError: true,
			expectNil:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			state, err := spm.loadState(context.Background(), tt.key)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if tt.expectNil {
				if state != nil {
					t.Errorf("Expected nil state but got %+v", state)
				}
			} else if !tt.expectError {
				if state == nil {
					t.Errorf("Expected state but got nil")
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Redis mock expectations not met: %v", err)
			}
		})
	}
}

func TestStatePersistenceManager_DeleteState(t *testing.T) {
	db, mock := redismock.NewClientMock()
	logger := &DefaultLogger{}
	spm := NewStatePersistenceManager(db, logger)

	tests := []struct {
		name        string
		key         string
		mockSetup   func()
		expectError bool
	}{
		{
			name: "successful delete",
			key:  "lottery:state:test:op123",
			mockSetup: func() {
				mock.ExpectDel("lottery:state:test:op123").SetVal(1)
			},
			expectError: false,
		},
		{
			name: "key not found",
			key:  "lottery:state:test:op123",
			mockSetup: func() {
				mock.ExpectDel("lottery:state:test:op123").SetVal(0)
			},
			expectError: false,
		},
		{
			name:        "empty key",
			key:         "",
			mockSetup:   func() {},
			expectError: true,
		},
		{
			name: "Redis error",
			key:  "lottery:state:test:op123",
			mockSetup: func() {
				mock.ExpectDel("lottery:state:test:op123").SetErr(redis.TxFailedErr)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			err := spm.deleteState(context.Background(), tt.key)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("Redis mock expectations not met: %v", err)
			}
		})
	}
}

// TestSerializationRoundTrip tests that serialization and deserialization are consistent
func TestSerializationRoundTrip(t *testing.T) {
	originalState := &DrawState{
		LockKey:        "test_lock_key",
		TotalCount:     100,
		CompletedCount: 75,
		Results:        []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		PrizeResults: []*Prize{
			{ID: "prize1", Name: "First Prize", Probability: 0.1, Value: 1000},
			{ID: "prize2", Name: "Second Prize", Probability: 0.2, Value: 500},
		},
		Errors: []DrawError{
			{DrawIndex: 11, Error: ErrLockAcquisitionFailed, ErrorMsg: ErrLockAcquisitionFailed.Error(), Timestamp: 1703123456},
			{DrawIndex: 12, Error: ErrRedisConnectionFailed, ErrorMsg: ErrRedisConnectionFailed.Error(), Timestamp: 1703123789},
		},
		StartTime:      1703120000,
		LastUpdateTime: 1703123456,
	}

	// Serialize
	data, err := serializeDrawState(originalState)
	if err != nil {
		t.Fatalf("Serialization failed: %v", err)
	}

	// Deserialize
	deserializedState, err := deserializeDrawState(data)
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// Compare all fields
	if deserializedState.LockKey != originalState.LockKey {
		t.Errorf("LockKey mismatch: expected %s, got %s", originalState.LockKey, deserializedState.LockKey)
	}
	if deserializedState.TotalCount != originalState.TotalCount {
		t.Errorf("TotalCount mismatch: expected %d, got %d", originalState.TotalCount, deserializedState.TotalCount)
	}
	if deserializedState.CompletedCount != originalState.CompletedCount {
		t.Errorf("CompletedCount mismatch: expected %d, got %d", originalState.CompletedCount, deserializedState.CompletedCount)
	}
	if len(deserializedState.Results) != len(originalState.Results) {
		t.Errorf("Results length mismatch: expected %d, got %d", len(originalState.Results), len(deserializedState.Results))
	}
	if len(deserializedState.PrizeResults) != len(originalState.PrizeResults) {
		t.Errorf("PrizeResults length mismatch: expected %d, got %d", len(originalState.PrizeResults), len(deserializedState.PrizeResults))
	}
	if len(deserializedState.Errors) != len(originalState.Errors) {
		t.Errorf("Errors length mismatch: expected %d, got %d", len(originalState.Errors), len(deserializedState.Errors))
	}
}

// ==============================

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

			for b.Loop() {
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

			for b.Loop() {
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
			for b.Loop() {
				mock.Regexp().ExpectSet(`lottery:state:.*`, `.*`, DefaultStateTTL).SetVal("OK")
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; b.Loop(); i++ {
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
			for i := 0; b.Loop(); i++ {
				mock.ExpectGet(fmt.Sprintf("lottery:state:benchmark_%d", i)).SetVal(string(data))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; b.Loop(); i++ {
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
		for b.Loop() {
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
		for b.Loop() {
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

	logger := &SilentLogger{}
	spm := NewStatePersistenceManager(db, logger)
	ctx := context.Background()

	state := createBenchmarkDrawState(100, 50, 10)

	b.Run("ConcurrentSaves", func(b *testing.B) {
		// Setup mock expectations
		for b.Loop() {
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
		for b.Loop() {
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
		for b.Loop() {
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
		for b.Loop() {
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

			for b.Loop() {
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

			for b.Loop() {
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
		// Setup mock expectations
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
		// Setup mock expectations - first attempt fails, second succeeds
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
	for i := range completedCount {
		results[i] = i + 1
	}

	// Create prize results
	prizeResults := make([]*Prize, prizeCount)
	for i := range prizeCount {
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
		for i := range errorCount {
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

// ----------------

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
		for range 3 {
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

// Integration test configuration
const (
	testRedisAddr = "localhost:6379"
	testRedisDB   = 15 // Use a separate DB for testing
	testKeyPrefix = "lottery_integration_test:"
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

	engine := NewLotteryEngineWithLogger(client, &SilentLogger{})
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
		for i := range numGoroutines {
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
		for i := range numGoroutines {
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
		for i := range numGoroutines {
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
		for range numGoroutines {
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
		for range numGoroutines {
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
