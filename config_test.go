package lottery

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigManager_LoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		setupEnv    func()
		expectError bool
		validate    func(*testing.T, *Config)
	}{
		{
			name: "default_config",
			setupEnv: func() {
				// 清除环境变量
				os.Clearenv()
			},
			expectError: false,
			validate: func(t *testing.T, config *Config) {
				assert.Equal(t, "localhost:6379", config.Redis.Addr)
				assert.Equal(t, 30*time.Second, config.Engine.LockTimeout)
				assert.Equal(t, 3, config.Engine.RetryAttempts)
			},
		},
		{
			name: "environment_variables",
			setupEnv: func() {
				os.Setenv("LOTTERY_SERVER_PORT", "9090")
				os.Setenv("LOTTERY_REDIS_ADDR", "redis-cluster:6379")
				os.Setenv("LOTTERY_LOTTERY_LOCK_TIMEOUT", "30s")
				os.Setenv("LOTTERY_SECURITY_ENABLE_AUTH", "true")
			},
			expectError: false,
			validate: func(t *testing.T, config *Config) {
				assert.Equal(t, "redis-cluster:6379", config.Redis.Addr)
				assert.Equal(t, 30*time.Second, config.Engine.LockTimeout)
			},
		},
		{
			name: "invalid_config",
			setupEnv: func() {
				os.Setenv("LOTTERY_LOTTERY_LOCK_TIMEOUT", "30ss")
				os.Setenv("LOTTERY_LOTTERY_MAX_SERIALIZATION_MB", "0") // 无效的序列化大小
			},
			expectError: true,
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 设置环境
			tt.setupEnv()
			defer os.Clearenv()

			// 创建配置管理器
			cm := NewConfigManager()

			// 加载配置
			config, err := cm.LoadConfig()

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, config)

			if tt.validate != nil {
				tt.validate(t, config)
			}
		})
	}
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name         string
		modifyConfig func(*Config)
		expectError  bool
		errorMsg     string
	}{
		{
			name: "valid_config",
			modifyConfig: func(config *Config) {
				// 不修改，使用默认配置
			},
			expectError: false,
		},
		{
			name: "invalid_server_port",
			modifyConfig: func(config *Config) {
				config.Redis.Addr = "" // 设置无效的Redis地址
			},
			expectError: true,
			errorMsg:    "redis address is required",
		},
		{
			name: "empty_redis_addr",
			modifyConfig: func(config *Config) {
				config.Redis.Addr = ""
			},
			expectError: true,
			errorMsg:    "redis address is required",
		},
		{
			name: "invalid_pool_size",
			modifyConfig: func(config *Config) {
				config.Redis.PoolSize = 0
			},
			expectError: true,
			errorMsg:    "redis pool size must be positive",
		},
		{
			name: "invalid_lock_timeout",
			modifyConfig: func(config *Config) {
				config.Engine.LockTimeout = 0
			},
			expectError: true,
			errorMsg:    "invalid lock timeout: must be between 1s and 5m",
		},
		{
			name: "negative_retry_attempts",
			modifyConfig: func(config *Config) {
				config.Engine.RetryAttempts = -1
			},
			expectError: true,
			errorMsg:    "invalid retry attempts: must be between 0 and 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建配置管理器
			cm := NewConfigManager()

			// 创建基础配置
			config := &Config{
				Engine: DefaultLotteryConfig(),

				Redis: &RedisConfig{
					Addr:     "localhost:6379",
					PoolSize: 10,
				},
			}

			// 应用修改
			tt.modifyConfig(config)

			// 验证配置
			err := cm.validateConfig(config)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewLotteryConfigFromConfig(t *testing.T) {
	config := &Config{
		Engine: &EngineConfig{
			LockTimeout:   45 * time.Second,
			RetryAttempts: 5,
			RetryInterval: 200 * time.Millisecond,
			LockCacheTTL:  1 * time.Second,
		},

		Redis: DefaultRedisConfig(),
		CircuitBreaker: &CircuitBreakerConfig{
			Enabled:      true,
			Name:         "test",
			MaxRequests:  3,
			Interval:     60 * time.Second,
			Timeout:      30 * time.Second,
			FailureRatio: 0.6,
			MinRequests:  3,
		},
	}

	lotteryConfig, err := NewLotteryConfigFromConfig(config)
	require.NoError(t, err)

	assert.Equal(t, 45*time.Second, lotteryConfig.GetConfig().Engine.LockTimeout)
	assert.Equal(t, 5, lotteryConfig.GetConfig().Engine.RetryAttempts)
	assert.Equal(t, 200*time.Millisecond, lotteryConfig.GetConfig().Engine.RetryInterval)
	assert.Equal(t, 1*time.Second, lotteryConfig.GetConfig().Engine.LockCacheTTL)
}

func TestNewRedisClientFromConfig(t *testing.T) {
	config := &RedisConfig{
		Addr:         "localhost:6379",
		Password:     "test-password",
		DB:           1,
		PoolSize:     50,
		MinIdleConns: 5,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
		ClusterMode:  false,
		TLSEnabled:   false,
	}

	client := NewRedisClientFromConfig(config)
	assert.NotNil(t, client)

	// 注意：这里只是测试客户端创建，不测试实际连接
	// 因为测试环境可能没有 Redis 服务器
}

// 基准测试
func BenchmarkConfigManager_LoadConfig(b *testing.B) {
	cm := NewConfigManager()

	b.ReportAllocs()

	for b.Loop() {
		_, err := cm.LoadConfig()
		if err != nil {
			b.Fatalf("Failed to load config: %v", err)
		}
	}
}

func BenchmarkConfig_Validation(b *testing.B) {
	cm := NewConfigManager()
	config := &Config{
		Engine: &EngineConfig{
			LockTimeout:   DefaultLockTimeout,
			RetryAttempts: DefaultRetryAttempts,
			RetryInterval: DefaultRetryInterval,
			LockCacheTTL:  DefaultLockCacheTTL,
		},

		Redis: &RedisConfig{
			Addr:     "localhost:6379",
			PoolSize: 10,
		},
		CircuitBreaker: &CircuitBreakerConfig{},
	}

	b.ReportAllocs()

	for b.Loop() {
		err := cm.validateConfig(config)
		if err != nil {
			b.Fatalf("Config validation failed: %v", err)
		}
	}
}
