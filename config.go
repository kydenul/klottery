package lottery

import (
	"fmt"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
)

// Config 生产环境配置结构
type Config struct {
	// Engine config
	Engine *EngineConfig `mapstructure:"lottery"`

	// Redis 配置
	Redis *RedisConfig `mapstructure:"redis"`

	// 熔断器配置
	CircuitBreaker *CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

func (c *Config) Validate() error {
	// 验证锁配置
	if c.Engine.LockTimeout < MinLockTimeout || c.Engine.LockTimeout > MaxLockTimeout {
		return ErrInvalidLockTimeout
	}
	if c.Engine.RetryAttempts < 0 || c.Engine.RetryAttempts > MaxRetryAttempts {
		return ErrInvalidRetryAttempts
	}
	if c.Engine.RetryInterval < 0 {
		return ErrInvalidRetryInterval
	}
	if c.Engine.LockCacheTTL < MinLockCacheTTL || c.Engine.LockCacheTTL > MaxLockCacheTTL {
		return ErrInvalidLockCacheTTL
	}

	// 验证 Redis 配置
	if c.Redis.Addr == "" {
		return fmt.Errorf("redis address is required")
	}
	if c.Redis.PoolSize <= 0 {
		return fmt.Errorf("redis pool size must be positive")
	}

	return nil
}

type EngineConfig struct {
	LockTimeout   time.Duration `mapstructure:"lock_timeout"`
	RetryAttempts int           `mapstructure:"retry_attempts"`
	RetryInterval time.Duration `mapstructure:"retry_interval"`
	LockCacheTTL  time.Duration `mapstructure:"lock_cache_ttl"`
}

func DefaultLotteryConfig() *EngineConfig {
	return &EngineConfig{
		LockTimeout:   DefaultLockTimeout,
		RetryAttempts: DefaultRetryAttempts,
		RetryInterval: DefaultRetryInterval,
		LockCacheTTL:  DefaultLockCacheTTL,
	}
}

func NewEngineConfig(
	lockTimeout time.Duration, attempts int, interval, cacheTTL time.Duration,
) *EngineConfig {
	return &EngineConfig{
		LockTimeout:   lockTimeout,
		RetryAttempts: attempts,
		RetryInterval: interval,
		LockCacheTTL:  cacheTTL,
	}
}

// RedisConfig Redis 配置
type RedisConfig struct {
	// 连接配置
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`

	// 连接池配置
	PoolSize     int `mapstructure:"pool_size"`
	MinIdleConns int `mapstructure:"min_idle_conns"`
	MaxRetries   int `mapstructure:"max_retries"`

	// 超时配置
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	PoolTimeout  time.Duration `mapstructure:"pool_timeout"`

	// 集群配置
	ClusterMode  bool     `mapstructure:"cluster_mode"`
	ClusterAddrs []string `mapstructure:"cluster_addrs"`

	// TLS 配置
	TLSEnabled bool   `mapstructure:"tls_enabled"`
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
	CAFile     string `mapstructure:"ca_file"`
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	Enabled       bool          `mapstructure:"enabled"`
	Name          string        `mapstructure:"name"`
	MaxRequests   uint32        `mapstructure:"max_requests"`
	Interval      time.Duration `mapstructure:"interval"`
	Timeout       time.Duration `mapstructure:"timeout"`
	FailureRatio  float64       `mapstructure:"failure_ratio"`
	MinRequests   uint32        `mapstructure:"min_requests"`
	OnStateChange bool          `mapstructure:"on_state_change"`
}

// DefaultCircuitBreakerConfig 返回默认熔断器配置
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		Enabled:       true,
		Name:          DefaultCircuitBreakerName,
		MaxRequests:   DefaultCircuitBreakerMaxRequests,
		Interval:      DefaultCircuitBreakerInterval,
		Timeout:       DefaultCircuitBreakerTimeout,
		FailureRatio:  DefaultCircuitBreakerFailureRatio,
		MinRequests:   DefaultCircuitBreakerMinRequests,
		OnStateChange: DefaultCircuitBreakerOnStateChange,
	}
}

// ConfigManager 配置管理器
type ConfigManager struct {
	viper  *viper.Viper
	config *Config
}

// NewConfigManager 创建配置管理器
func NewConfigManager() *ConfigManager {
	v := viper.New()

	// 设置配置文件名和路径
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/lottery")
	v.AddConfigPath("$HOME/.lottery")

	// 设置环境变量前缀
	v.SetEnvPrefix("LOTTERY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	return &ConfigManager{
		viper: v,
	}
}

// LoadConfig 加载配置
func (cm *ConfigManager) LoadConfig() (*Config, error) {
	// 设置默认值
	cm.setDefaults()

	// 读取配置文件
	if err := cm.viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		// 配置文件不存在时使用默认配置
	}

	// 解析配置
	config := &Config{}
	if err := cm.viper.Unmarshal(config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 验证配置
	if err := cm.validateConfig(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	cm.config = config
	return config, nil
}

// setDefaults 设置默认配置值
func (cm *ConfigManager) setDefaults() {
	// 抽奖引擎默认配置
	cm.viper.SetDefault("lottery.lock_timeout", "30s")
	cm.viper.SetDefault("lottery.retry_attempts", 3)
	cm.viper.SetDefault("lottery.retry_interval", "100ms")

	// Redis 默认配置
	cm.viper.SetDefault("redis.addr", "localhost:6379")
	cm.viper.SetDefault("redis.password", "")
	cm.viper.SetDefault("redis.db", 0)
	cm.viper.SetDefault("redis.pool_size", 100)
	cm.viper.SetDefault("redis.min_idle_conns", 10)
	cm.viper.SetDefault("redis.max_retries", 3)
	cm.viper.SetDefault("redis.dial_timeout", "5s")
	cm.viper.SetDefault("redis.read_timeout", "3s")
	cm.viper.SetDefault("redis.write_timeout", "3s")
	cm.viper.SetDefault("redis.pool_timeout", "4s")
	cm.viper.SetDefault("redis.cluster_mode", false)
	cm.viper.SetDefault("redis.tls_enabled", false)

	// 熔断器默认配置
	cm.viper.SetDefault("circuit_breaker.enabled", true)
	cm.viper.SetDefault("circuit_breaker.name", "lottery-engine")
	cm.viper.SetDefault("circuit_breaker.max_requests", 3)
	cm.viper.SetDefault("circuit_breaker.interval", "60s")
	cm.viper.SetDefault("circuit_breaker.timeout", "30s")
	cm.viper.SetDefault("circuit_breaker.failure_ratio", 0.6)
	cm.viper.SetDefault("circuit_breaker.min_requests", 3)
	cm.viper.SetDefault("circuit_breaker.on_state_change", true)
}

// validateConfig 验证配置
func (cm *ConfigManager) validateConfig(config *Config) error { return config.Validate() }

// WatchConfig 监听配置变化
func (cm *ConfigManager) WatchConfig(callback func(*Config)) error {
	cm.viper.WatchConfig()
	cm.viper.OnConfigChange(func(e fsnotify.Event) {
		config := &Config{}
		if err := cm.viper.Unmarshal(config); err != nil {
			// 记录错误但不中断服务
			return
		}

		if err := cm.validateConfig(config); err != nil {
			// 记录错误但不中断服务
			return
		}

		cm.config = config
		if callback != nil {
			callback(config)
		}
	})

	return nil
}

// GetConfig 获取当前配置
func (cm *ConfigManager) GetConfig() *Config { return cm.config }

// ReloadConfig 重新加载配置
func (cm *ConfigManager) ReloadConfig() (*Config, error) { return cm.LoadConfig() }

// DefaultRedisConfig 返回默认的Redis配置
func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		Addr:         DefaultRedisAddr,
		Password:     DefaultRedisPassword,
		DB:           DefaultRedisDB,
		PoolSize:     DefaultRedisPoolSize,
		MinIdleConns: DefaultRedisMinIdleConns,
		MaxRetries:   DefaultRedisMaxRetries,
		DialTimeout:  DefaultRedisDialTimeout,
		ReadTimeout:  DefaultRedisReadTimeout,
		WriteTimeout: DefaultRedisWriteTimeout,
		PoolTimeout:  DefaultRedisPoolTimeout,
		ClusterMode:  DefaultRedisClusterMode,
		TLSEnabled:   DefaultRedisTLSEnabled,
	}
}

// NewRedisClient 创建 Redis 客户端, 使用默认配置
func NewRedisClient() *redis.Client {
	config := DefaultRedisConfig()
	return redis.NewClient(&redis.Options{
		Addr:         config.Addr,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		MaxRetries:   config.MaxRetries,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		PoolTimeout:  config.PoolTimeout,
	})
}

// NewRedisClientFromConfig 从配置创建Redis客户端
func NewRedisClientFromConfig(config *RedisConfig) *redis.Client {
	if config == nil {
		config = DefaultRedisConfig()
	}

	return redis.NewClient(&redis.Options{
		Addr:         config.Addr,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		MaxRetries:   config.MaxRetries,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		PoolTimeout:  config.PoolTimeout,
	})
}

// NewDefaultConfigManager 创建默认的抽奖配置
func NewDefaultConfigManager() *ConfigManager {
	cm := NewConfigManager()
	cm.setDefaults()

	// 创建默认配置结构
	cm.config = &Config{
		Engine:         DefaultLotteryConfig(),
		Redis:          DefaultRedisConfig(),
		CircuitBreaker: DefaultCircuitBreakerConfig(),
	}
	return cm
}

// NewLotteryConfig 创建自定义抽奖配置
func NewLotteryConfig(
	lockTimeout time.Duration, retryAttempts int, retryInterval, lockCacheTTL time.Duration,
) (*ConfigManager, error) {
	if lockTimeout <= 0 {
		return nil, fmt.Errorf("lock timeout must be positive")
	}
	if retryAttempts < 0 {
		return nil, fmt.Errorf("retry attempts cannot be negative")
	}
	if retryInterval < 0 {
		return nil, fmt.Errorf("retry interval cannot be negative")
	}

	cm := NewConfigManager()
	cm.setDefaults()

	config := &Config{
		Engine: &EngineConfig{
			LockTimeout:   lockTimeout,
			RetryAttempts: retryAttempts,
			RetryInterval: retryInterval,
			LockCacheTTL:  lockCacheTTL,
		},
		Redis:          DefaultRedisConfig(),
		CircuitBreaker: DefaultCircuitBreakerConfig(),
	}

	cm.config = config
	return cm, nil
}

// NewLotteryConfigFromConfig 从配置文件创建抽奖配置
func NewLotteryConfigFromConfig(config *Config) (*ConfigManager, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	cm := NewConfigManager()
	cm.config = config
	return cm, nil
}
