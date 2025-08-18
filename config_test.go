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
				assert.Equal(t, "0.0.0.0", config.Server.Host)
				assert.Equal(t, 8080, config.Server.Port)
				assert.Equal(t, "localhost:6379", config.Redis.Addr)
				assert.Equal(t, 30*time.Second, config.Lottery.LockTimeout)
				assert.Equal(t, 3, config.Lottery.RetryAttempts)
			},
		},
		{
			name: "environment_variables",
			setupEnv: func() {
				os.Setenv("LOTTERY_SERVER_PORT", "9090")
				os.Setenv("LOTTERY_REDIS_ADDR", "redis-cluster:6379")
				os.Setenv("LOTTERY_LOTTERY_LOCK_TIMEOUT", "60s")
				os.Setenv("LOTTERY_SECURITY_ENABLE_AUTH", "true")
			},
			expectError: false,
			validate: func(t *testing.T, config *Config) {
				assert.Equal(t, 9090, config.Server.Port)
				assert.Equal(t, "redis-cluster:6379", config.Redis.Addr)
				assert.Equal(t, 60*time.Second, config.Lottery.LockTimeout)
				assert.True(t, config.Security.EnableAuth)
			},
		},
		{
			name: "invalid_config",
			setupEnv: func() {
				os.Setenv("LOTTERY_SERVER_PORT", "99999") // 无效端口
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
	cm := NewConfigManager()

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
				config.Server.Port = -1
			},
			expectError: true,
			errorMsg:    "invalid server port",
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
				config.Lottery.LockTimeout = 0
			},
			expectError: true,
			errorMsg:    "lock timeout must be positive",
		},
		{
			name: "negative_retry_attempts",
			modifyConfig: func(config *Config) {
				config.Lottery.RetryAttempts = -1
			},
			expectError: true,
			errorMsg:    "retry attempts cannot be negative",
		},
		{
			name: "auth_without_jwt_secret",
			modifyConfig: func(config *Config) {
				config.Security.EnableAuth = true
				config.Security.JWTSecret = ""
			},
			expectError: true,
			errorMsg:    "JWT secret is required when auth is enabled",
		},
		{
			name: "encryption_without_key",
			modifyConfig: func(config *Config) {
				config.Security.EnableEncryption = true
				config.Security.EncryptionKey = ""
			},
			expectError: true,
			errorMsg:    "encryption key is required when encryption is enabled",
		},
		{
			name: "invalid_log_level",
			modifyConfig: func(config *Config) {
				config.Logging.Level = "invalid"
			},
			expectError: true,
			errorMsg:    "invalid log level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建基础配置
			config := &Config{
				Server: ServerConfig{
					Host: "localhost",
					Port: 8080,
				},
				Redis: RedisConfig{
					Addr:     "localhost:6379",
					PoolSize: 10,
				},
				Lottery: LotteryEngineConfig{
					LockTimeout:        30 * time.Second,
					RetryAttempts:      3,
					MaxSerializationMB: 10,
				},
				Security: SecurityConfig{
					EnableAuth:       false,
					EnableEncryption: false,
				},
				Logging: LoggingConfig{
					Level: "info",
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
		Lottery: LotteryEngineConfig{
			LockTimeout:   45 * time.Second,
			RetryAttempts: 5,
			RetryInterval: 200 * time.Millisecond,
		},
	}

	lotteryConfig := NewLotteryConfigFromConfig(config)

	assert.Equal(t, 45*time.Second, lotteryConfig.LockTimeout)
	assert.Equal(t, 5, lotteryConfig.RetryAttempts)
	assert.Equal(t, 200*time.Millisecond, lotteryConfig.RetryInterval)
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

func TestAlertRule(t *testing.T) {
	rule := AlertRule{
		Name:        "test_rule",
		Metric:      "test_metric",
		Threshold:   0.5,
		Operator:    ">",
		Duration:    5 * time.Minute,
		Severity:    "warning",
		Description: "Test alert rule",
	}

	assert.Equal(t, "test_rule", rule.Name)
	assert.Equal(t, "test_metric", rule.Metric)
	assert.Equal(t, 0.5, rule.Threshold)
	assert.Equal(t, ">", rule.Operator)
	assert.Equal(t, 5*time.Minute, rule.Duration)
	assert.Equal(t, "warning", rule.Severity)
	assert.Equal(t, "Test alert rule", rule.Description)
}

// 基准测试
func BenchmarkConfigManager_LoadConfig(b *testing.B) {
	cm := NewConfigManager()

	b.ResetTimer()
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
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Redis: RedisConfig{
			Addr:     "localhost:6379",
			PoolSize: 10,
		},
		Lottery: LotteryEngineConfig{
			LockTimeout:   30 * time.Second,
			RetryAttempts: 3,
		},
		Security: SecurityConfig{
			EnableAuth:       false,
			EnableEncryption: false,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := cm.validateConfig(config)
		if err != nil {
			b.Fatalf("Config validation failed: %v", err)
		}
	}
}
