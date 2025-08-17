package lottery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redismock/v8"
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
	for i := 0; i < 100; i++ {
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

		// First two parts should be date and time
		if len(parts[0]) != 8 { // YYYYMMDD
			t.Errorf("Date part format incorrect: %s", parts[0])
		}
		if len(parts[1]) != 6 { // HHMMSS
			t.Errorf("Time part format incorrect: %s", parts[1])
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
