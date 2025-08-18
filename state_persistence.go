package lottery

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	// StateKeyPrefix is the prefix for Redis state persistence keys
	StateKeyPrefix = "lottery:state:"

	// DefaultStateTTL is the default TTL for persisted state (1 hour)
	DefaultStateTTL = 1 * time.Hour

	// OperationIDLength is the length of the operation ID in bytes
	OperationIDLength = 8

	// MaxSerializationSize is the maximum allowed size for serialized DrawState (10MB)
	MaxSerializationSize = 10 * 1024 * 1024
)

// StatePersistenceManager handles Redis operations for state management
type StatePersistenceManager struct {
	redisClient    *redis.Client
	logger         Logger
	retryAttempts  int
	retryBaseDelay time.Duration
}

// NewStatePersistenceManager creates a new state persistence manager
func NewStatePersistenceManager(redisClient *redis.Client, logger Logger) *StatePersistenceManager {
	return &StatePersistenceManager{
		redisClient:    redisClient,
		logger:         logger,
		retryAttempts:  DefaultRetryAttempts,
		retryBaseDelay: DefaultRetryInterval,
	}
}

// NewStatePersistenceManagerWithRetry creates a new state persistence manager with custom retry settings
func NewStatePersistenceManagerWithRetry(redisClient *redis.Client, logger Logger, retryAttempts int, retryDelay time.Duration) *StatePersistenceManager {
	return &StatePersistenceManager{
		redisClient:    redisClient,
		logger:         logger,
		retryAttempts:  retryAttempts,
		retryBaseDelay: retryDelay,
	}
}

// serializeDrawState serializes a DrawState to JSON bytes
func serializeDrawState(drawState *DrawState) ([]byte, error) {
	if drawState == nil {
		return nil, ErrInvalidParameters
	}

	// Validate the draw state before serialization
	if err := drawState.Validate(); err != nil {
		return nil, err
	}

	data, err := json.Marshal(drawState)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize DrawState: %w", err)
	}

	// Check serialization size
	if len(data) > MaxSerializationSize {
		return nil, fmt.Errorf("serialized DrawState size (%d bytes) exceeds maximum allowed size (%d bytes): lockKey=%s, totalCount=%d, completedCount=%d",
			len(data), MaxSerializationSize, drawState.LockKey, drawState.TotalCount, drawState.CompletedCount)
	}

	return data, nil
}

// isRetriableRedisError checks if a Redis error is retriable
func isRetriableRedisError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common retriable Redis errors
	errStr := strings.ToLower(err.Error())
	retriableErrors := []string{
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

	for _, retriableErr := range retriableErrors {
		if strings.Contains(errStr, retriableErr) {
			return true
		}
	}

	return false
}

// executeWithRetry executes a Redis operation with retry logic using exponential backoff
func (spm *StatePersistenceManager) executeWithRetry(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	startTime := time.Now()

	for attempt := 0; attempt <= spm.retryAttempts; attempt++ {
		if attempt > 0 {
			// Calculate exponential backoff delay: baseDelay * 2^(attempt-1)
			backoffMultiplier := 1 << (attempt - 1) // 2^(attempt-1)
			delay := time.Duration(backoffMultiplier) * spm.retryBaseDelay

			// Cap the maximum delay to prevent excessive wait times
			maxDelay := 5 * time.Second
			if delay > maxDelay {
				delay = maxDelay
			}

			spm.logger.Debug("Retrying %s operation (attempt %d/%d) after %v exponential backoff delay, total elapsed: %v",
				operation, attempt, spm.retryAttempts, delay, time.Since(startTime))

			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry for %s operation after %v (attempt %d/%d): %w",
					operation, time.Since(startTime), attempt, spm.retryAttempts+1, ctx.Err())
			case <-time.After(delay):
				// Continue with retry
			}
		}

		attemptStart := time.Now()
		err := fn()
		attemptDuration := time.Since(attemptStart)

		if err == nil {
			if attempt > 0 {
				spm.logger.Info("Successfully completed %s operation after %d retries in %v (total time: %v)",
					operation, attempt, attemptDuration, time.Since(startTime))
			} else {
				spm.logger.Debug("Successfully completed %s operation in %v", operation, attemptDuration)
			}
			return nil
		}

		lastErr = err

		// Check if error is retriable
		if !isRetriableRedisError(err) {
			spm.logger.Debug("Non-retriable error for %s operation (attempt %d, duration: %v): %v",
				operation, attempt+1, attemptDuration, err)
			break
		}

		if attempt < spm.retryAttempts {
			spm.logger.Debug("Retriable error for %s operation (attempt %d/%d, duration: %v): %v",
				operation, attempt+1, spm.retryAttempts+1, attemptDuration, err)
		} else {
			spm.logger.Error("Final retry attempt failed for %s operation (attempt %d/%d, duration: %v): %v",
				operation, attempt+1, spm.retryAttempts+1, attemptDuration, err)
		}
	}

	totalDuration := time.Since(startTime)
	return fmt.Errorf("%s operation failed after %d attempts in %v: %w",
		operation, spm.retryAttempts+1, totalDuration, lastErr)
}

// deserializeDrawState deserializes JSON bytes back to DrawState
func deserializeDrawState(data []byte) (*DrawState, error) {
	if len(data) == 0 {
		return nil, ErrInvalidParameters
	}

	var drawState DrawState
	if err := json.Unmarshal(data, &drawState); err != nil {
		return nil, fmt.Errorf("failed to deserialize DrawState: %w", err)
	}

	// Validate the deserialized state
	if err := drawState.Validate(); err != nil {
		return nil, ErrDrawStateCorrupted
	}

	return &drawState, nil
}

// generateOperationID generates a unique operation ID using timestamp and random bytes
func generateOperationID() string {
	// Use current timestamp as prefix for time-based ordering
	timestamp := time.Now().Format("20060102_150405")

	// Generate random bytes for uniqueness
	randomBytes := make([]byte, OperationIDLength)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("%s_%d", timestamp, time.Now().UnixNano()%1000000)
	}

	randomHex := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%s_%s", timestamp, randomHex)
}

// generateStateKey generates a Redis key for state persistence
func generateStateKey(lockKey string) string {
	if lockKey == "" {
		return ""
	}

	operationID := generateOperationID()
	return fmt.Sprintf("%s%s:%s", StateKeyPrefix, lockKey, operationID)
}

// parseStateKey parses a state key to extract the lock key and operation ID
func parseStateKey(key string) (lockKey string, operationID string, err error) {
	if !strings.HasPrefix(key, StateKeyPrefix) {
		return "", "", fmt.Errorf("invalid state key format: missing prefix")
	}

	// Remove the prefix
	keyWithoutPrefix := strings.TrimPrefix(key, StateKeyPrefix)

	// Find the last colon to separate lockKey from operationID
	lastColonIndex := strings.LastIndex(keyWithoutPrefix, ":")
	if lastColonIndex == -1 {
		return "", "", fmt.Errorf("invalid state key format: missing operation ID separator")
	}

	lockKey = keyWithoutPrefix[:lastColonIndex]
	operationID = keyWithoutPrefix[lastColonIndex+1:]

	if lockKey == "" {
		return "", "", fmt.Errorf("invalid state key format: empty lock key")
	}
	if operationID == "" {
		return "", "", fmt.Errorf("invalid state key format: empty operation ID")
	}

	return lockKey, operationID, nil
}

// saveState saves a DrawState to Redis with TTL
func (spm *StatePersistenceManager) saveState(ctx context.Context, key string, state *DrawState, ttl time.Duration) error {
	if key == "" {
		return fmt.Errorf("invalid parameters: empty key provided for saveState operation")
	}
	if state == nil {
		return fmt.Errorf("invalid parameters: nil DrawState provided for saveState operation")
	}

	spm.logger.Debug("Saving draw state to Redis: key=%s, lockKey=%s, progress=%.1f%%, ttl=%v",
		key, state.LockKey, state.Progress(), ttl)

	// Serialize the state
	serializationStart := time.Now()
	data, err := serializeDrawState(state)
	serializationDuration := time.Since(serializationStart)

	if err != nil {
		spm.logger.Error("Failed to serialize draw state for key=%s, lockKey=%s, serialization_time=%v: %v",
			key, state.LockKey, serializationDuration, err)
		return fmt.Errorf("serialization failed for DrawState (lockKey=%s, totalCount=%d, completedCount=%d, results=%d, prizes=%d, errors=%d, serialization_time=%v): %w",
			state.LockKey, state.TotalCount, state.CompletedCount, len(state.Results), len(state.PrizeResults), len(state.Errors), serializationDuration, err)
	}

	spm.logger.Debug("Serialized draw state in %v: key=%s, size=%d bytes", serializationDuration, key, len(data))

	// Save to Redis with TTL using retry logic
	err = spm.executeWithRetry(ctx, fmt.Sprintf("save[%s]", key), func() error {
		return spm.redisClient.Set(ctx, key, data, ttl).Err()
	})
	if err != nil {
		spm.logger.Error("Failed to save state to Redis after retries: key=%s, lockKey=%s, size=%d bytes, ttl=%v, serialization_time=%v, error=%v",
			key, state.LockKey, len(data), ttl, serializationDuration, err)
		return fmt.Errorf("Redis save operation failed for key=%s (lockKey=%s, size=%d bytes, ttl=%v, serialization_time=%v, progress=%.1f%%): %w",
			key, state.LockKey, len(data), ttl, serializationDuration, state.Progress(), err)
	}

	spm.logger.Debug("Successfully saved draw state: key=%s, lockKey=%s, size=%d bytes, ttl=%v, serialization_time=%v",
		key, state.LockKey, len(data), ttl, serializationDuration)
	return nil
}

// loadState loads a DrawState from Redis
func (spm *StatePersistenceManager) loadState(ctx context.Context, key string) (*DrawState, error) {
	if key == "" {
		return nil, fmt.Errorf("invalid parameters: empty key provided for loadState operation")
	}

	spm.logger.Debug("Loading draw state from Redis: key=%s", key)

	var data []byte
	var err error

	// Get data from Redis with retry logic
	loadStart := time.Now()
	err = spm.executeWithRetry(ctx, fmt.Sprintf("load[%s]", key), func() error {
		data, err = spm.redisClient.Get(ctx, key).Bytes()
		if err == redis.Nil {
			// Key doesn't exist - this is not an error condition, don't retry
			return nil
		}
		return err
	})
	loadDuration := time.Since(loadStart)

	if err != nil {
		spm.logger.Error("Failed to load state from Redis after retries: key=%s, load_time=%v, error=%v", key, loadDuration, err)
		return nil, fmt.Errorf("Redis load operation failed for key=%s (load_time=%v): %w", key, loadDuration, err)
	}

	// Check if key doesn't exist (data will be nil if redis.Nil was returned)
	if len(data) == 0 {
		spm.logger.Debug("No saved state found: key=%s, load_time=%v", key, loadDuration)
		return nil, nil
	}

	spm.logger.Debug("Loaded raw data from Redis in %v: key=%s, size=%d bytes", loadDuration, key, len(data))

	// Check data size
	if len(data) > MaxSerializationSize {
		spm.logger.Error("Loaded data size (%d bytes) exceeds maximum allowed size (%d bytes): key=%s, load_time=%v",
			len(data), MaxSerializationSize, key, loadDuration)
		return nil, fmt.Errorf("loaded data size (%d bytes) exceeds maximum allowed size (%d bytes) for key=%s (load_time=%v)",
			len(data), MaxSerializationSize, key, loadDuration)
	}

	// Deserialize the state
	deserializationStart := time.Now()
	state, err := deserializeDrawState(data)
	deserializationDuration := time.Since(deserializationStart)

	if err != nil {
		spm.logger.Error("Failed to deserialize draw state: key=%s, size=%d bytes, load_time=%v, deserialization_time=%v, error=%v",
			key, len(data), loadDuration, deserializationDuration, err)
		return nil, fmt.Errorf("deserialization failed for key=%s (size=%d bytes, load_time=%v, deserialization_time=%v): %w",
			key, len(data), loadDuration, deserializationDuration, err)
	}

	totalDuration := time.Since(loadStart)
	spm.logger.Debug("Successfully loaded draw state: key=%s, lockKey=%s, size=%d bytes, progress=%.1f%%, load_time=%v, deserialization_time=%v, total_time=%v",
		key, state.LockKey, len(data), state.Progress(), loadDuration, deserializationDuration, totalDuration)
	return state, nil
}

// deleteState removes a DrawState from Redis
func (spm *StatePersistenceManager) deleteState(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("invalid parameters: empty key provided for deleteState operation")
	}

	spm.logger.Debug("Deleting draw state from Redis: key=%s", key)

	var result int64
	var err error

	// Delete from Redis with retry logic
	deleteStart := time.Now()
	err = spm.executeWithRetry(ctx, fmt.Sprintf("delete[%s]", key), func() error {
		result, err = spm.redisClient.Del(ctx, key).Result()
		return err
	})
	deleteDuration := time.Since(deleteStart)

	if err != nil {
		spm.logger.Error("Failed to delete state from Redis after retries: key=%s, delete_time=%v, error=%v", key, deleteDuration, err)
		return fmt.Errorf("Redis delete operation failed for key=%s (delete_time=%v): %w", key, deleteDuration, err)
	}

	if result == 0 {
		spm.logger.Debug("State key did not exist: key=%s, delete_time=%v", key, deleteDuration)
	} else {
		spm.logger.Debug("Successfully deleted draw state: key=%s, delete_time=%v, keys_deleted=%d", key, deleteDuration, result)
	}

	return nil
}

// findStateKeys finds all state keys for a given lock key pattern
func (spm *StatePersistenceManager) findStateKeys(ctx context.Context, lockKey string) ([]string, error) {
	if lockKey == "" {
		return nil, fmt.Errorf("invalid parameters: empty lockKey provided for findStateKeys operation")
	}

	pattern := fmt.Sprintf("%s%s:*", StateKeyPrefix, lockKey)
	spm.logger.Debug("Searching for state keys: pattern=%s", pattern)

	var keys []string
	var err error

	// Search for keys with retry logic
	err = spm.executeWithRetry(ctx, "keys", func() error {
		keys, err = spm.redisClient.Keys(ctx, pattern).Result()
		return err
	})
	if err != nil {
		spm.logger.Error("Failed to search for state keys after retries: pattern=%s, error=%v", pattern, err)
		return nil, fmt.Errorf("Redis keys operation failed for pattern=%s (lockKey=%s): %w", pattern, lockKey, err)
	}

	spm.logger.Debug("Found %d state keys for lockKey=%s", len(keys), lockKey)
	return keys, nil
}

// cleanupExpiredStates removes expired state keys (utility function for maintenance)
func (spm *StatePersistenceManager) cleanupExpiredStates(ctx context.Context, lockKey string) error {
	keys, err := spm.findStateKeys(ctx, lockKey)
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return nil
	}

	spm.logger.Debug("Cleaning up %d potential expired state keys for lockKey=%s", len(keys), lockKey)

	// Check TTL for each key and delete expired ones
	var deletedCount int
	for _, key := range keys {
		ttl, err := spm.redisClient.TTL(ctx, key).Result()
		if err != nil {
			spm.logger.Error("Failed to check TTL for key %s: %v", key, err)
			continue
		}

		// If TTL is -1, key exists but has no expiration (shouldn't happen with our keys)
		// If TTL is -2, key doesn't exist
		// If TTL is 0 or very small, key is about to expire
		if ttl == -2 || (ttl >= 0 && ttl < time.Minute) {
			if err := spm.deleteState(ctx, key); err != nil {
				spm.logger.Error("Failed to cleanup expired state key %s: %v", key, err)
			} else {
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		spm.logger.Info("Cleaned up %d expired state keys for lockKey=%s", deletedCount, lockKey)
	}

	return nil
}
