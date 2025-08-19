package lottery

import (
	"errors"
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

	for i := 0; i < b.N; i++ {
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

	for i := 0; i < b.N; i++ {
		err := cm.validateConfig(config)
		if err != nil {
			b.Fatalf("Config validation failed: %v", err)
		}
	}
}

// ================================================================================
// Logger Tests
// ================================================================================

func TestDefaultLogger_Info(t *testing.T) {
	logger := &DefaultLogger{}

	// 测试基本信息日志
	logger.Info("Test info message")
	logger.Info("Test info with args: %s %d", "test", 123)

	// 测试空消息
	logger.Info("")

	// 测试特殊字符
	logger.Info("Test with special chars: %v", map[string]int{"key": 1})
}

func TestDefaultLogger_Error(t *testing.T) {
	logger := &DefaultLogger{}

	// 测试基本错误日志
	logger.Error("Test error message")
	logger.Error("Test error with args: %s %d", "error", 500)

	// 测试空消息
	logger.Error("")

	// 测试错误对象
	logger.Error("Error occurred: %v", errors.New("test error"))
}

func TestDefaultLogger_Debug(t *testing.T) {
	logger := &DefaultLogger{}

	// 测试基本调试日志
	logger.Debug("Test debug message")
	logger.Debug("Test debug with args: %s %d", "debug", 42)

	// 测试空消息
	logger.Debug("")

	// 测试复杂数据结构
	logger.Debug("Debug data: %+v", struct {
		Name  string
		Value int
	}{"test", 100})
}

func TestSilentLogger_NewSilentLogger(t *testing.T) {
	logger := NewSilentLogger()
	if logger == nil {
		t.Error("NewSilentLogger should return non-nil logger")
	}
}

func TestSilentLogger_Info(t *testing.T) {
	logger := NewSilentLogger()

	// 测试静默日志 - 不应该有任何输出
	logger.Info("This should not appear")
	logger.Info("Test with args: %s %d", "silent", 123)
	logger.Info("")

	// 测试不会panic
	logger.Info("Test with nil args: %v", nil)
}

func TestSilentLogger_Error(t *testing.T) {
	logger := NewSilentLogger()

	// 测试静默错误日志 - 不应该有任何输出
	logger.Error("This error should not appear")
	logger.Error("Test error with args: %s %d", "silent", 500)
	logger.Error("")

	// 测试不会panic
	logger.Error("Test with error: %v", errors.New("silent error"))
}

func TestSilentLogger_Debug(t *testing.T) {
	logger := NewSilentLogger()

	// 测试静默调试日志 - 不应该有任何输出
	logger.Debug("This debug should not appear")
	logger.Debug("Test debug with args: %s %d", "silent", 42)
	logger.Debug("")

	// 测试不会panic
	logger.Debug("Test with complex data: %+v", map[string]any{"key": "value"})
}

func TestLogger_Interface_Compliance(t *testing.T) {
	// 测试DefaultLogger实现了Logger接口
	var logger Logger = &DefaultLogger{}
	logger.Info("Interface test")
	logger.Error("Interface test")
	logger.Debug("Interface test")

	// 测试SilentLogger实现了Logger接口
	var silentLogger Logger = NewSilentLogger()
	silentLogger.Info("Silent interface test")
	silentLogger.Error("Silent interface test")
	silentLogger.Debug("Silent interface test")
}

func TestLogger_EdgeCases(t *testing.T) {
	t.Run("default_logger_with_many_args", func(t *testing.T) {
		logger := &DefaultLogger{}
		logger.Info("Many args: %s %d %f %t %v", "str", 123, 3.14, true, []int{1, 2, 3})
	})

	t.Run("silent_logger_with_many_args", func(t *testing.T) {
		logger := NewSilentLogger()
		logger.Error("Many args: %s %d %f %t %v", "str", 123, 3.14, true, []int{1, 2, 3})
	})

	t.Run("format_string_mismatch", func(t *testing.T) {
		logger := &DefaultLogger{}
		// 测试格式字符串与参数不匹配的情况
		logger.Info("Format mismatch: %s %d", "only_one_arg")
		logger.Debug("Too many args: %s", "arg1", "arg2", "arg3")
	})

	t.Run("nil_args", func(t *testing.T) {
		logger := &DefaultLogger{}
		logger.Info("Nil test: %v %s", nil, "after nil")

		silentLogger := NewSilentLogger()
		silentLogger.Error("Silent nil test: %v %s", nil, "after nil")
	})
}

// 性能测试
func BenchmarkDefaultLogger_Info(b *testing.B) {
	logger := &DefaultLogger{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("Benchmark test message %d", i)
	}
}

func BenchmarkSilentLogger_Info(b *testing.B) {
	logger := NewSilentLogger()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("Benchmark silent message %d", i)
	}
}
