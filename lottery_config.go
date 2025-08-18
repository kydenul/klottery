package lottery

import (
	"time"

	"github.com/go-redis/redis/v8"
)

// LotteryConfig holds configuration for the lottery system
type LotteryConfig struct {
	LockTimeout   time.Duration `json:"lock_timeout"`   // Lock timeout duration
	RetryAttempts int           `json:"retry_attempts"` // Number of retry attempts
	RetryInterval time.Duration `json:"retry_interval"` // Retry interval
}

// NewLotteryConfig creates a new lottery configuration with validation
func NewLotteryConfig(
	lockTimeout time.Duration, retryAttempts int, retryInterval time.Duration,
) (*LotteryConfig, error) {
	config := &LotteryConfig{
		LockTimeout:   lockTimeout,
		RetryAttempts: retryAttempts,
		RetryInterval: retryInterval,
	}

	// Set defaults if zero values provided
	config.SetDefaults()

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// NewDefaultLotteryConfig creates a new lottery configuration with default values
func NewDefaultLotteryConfig() *LotteryConfig {
	return &LotteryConfig{
		LockTimeout:   DefaultLockTimeout,
		RetryAttempts: DefaultRetryAttempts,
		RetryInterval: DefaultRetryInterval,
	}
}

// NewLotteryConfigFromConfig 从新配置系统创建 LotteryConfig
func NewLotteryConfigFromConfig(config *Config) *LotteryConfig {
	return &LotteryConfig{
		LockTimeout:   config.Lottery.LockTimeout,
		RetryAttempts: config.Lottery.RetryAttempts,
		RetryInterval: config.Lottery.RetryInterval,
	}
}

// Validate validates the lottery configuration
func (lc *LotteryConfig) Validate() error {
	if lc.LockTimeout < MinLockTimeout || lc.LockTimeout > MaxLockTimeout {
		return ErrInvalidLockTimeout
	}
	if lc.RetryAttempts < 0 || lc.RetryAttempts > MaxRetryAttempts {
		return ErrInvalidRetryAttempts
	}
	if lc.RetryInterval < 0 {
		return ErrInvalidRetryInterval
	}
	return nil
}

// SetDefaults sets default values for the configuration
func (lc *LotteryConfig) SetDefaults() {
	if lc.LockTimeout == 0 {
		lc.LockTimeout = DefaultLockTimeout
	}
	if lc.RetryAttempts == 0 {
		lc.RetryAttempts = DefaultRetryAttempts
	}
	if lc.RetryInterval == 0 {
		lc.RetryInterval = DefaultRetryInterval
	}
}

// LegacyRedisConfig 优化的Redis配置 (重命名以避免冲突)
type LegacyRedisConfig struct {
	// 连接池配置
	PoolSize     int `json:"pool_size"`      // 连接池大小
	MinIdleConns int `json:"min_idle_conns"` // 最小空闲连接数
	MaxRetries   int `json:"max_retries"`    // 最大重试次数

	// 超时配置
	DialTimeout  time.Duration `json:"dial_timeout"`  // 连接超时
	ReadTimeout  time.Duration `json:"read_timeout"`  // 读取超时
	WriteTimeout time.Duration `json:"write_timeout"` // 写入超时

	// 连接管理
	PoolTimeout time.Duration `json:"pool_timeout"` // 连接池超时
}

// DefaultLegacyRedisConfig returns default Redis Config
func DefaultLegacyRedisConfig() *LegacyRedisConfig {
	return &LegacyRedisConfig{
		PoolSize:     50,              // 增加连接池大小以支持高并发
		MinIdleConns: 10,              // 保持一定数量的空闲连接
		MaxRetries:   3,               // 适度的重试次数
		DialTimeout:  5 * time.Second, // 连接超时
		ReadTimeout:  3 * time.Second, // 读取超时
		WriteTimeout: 3 * time.Second, // 写入超时
		PoolTimeout:  4 * time.Second, // 连接池超时
	}
}

// NewLegacyRedisClient returns Redis Client (保持向后兼容)
func NewLegacyRedisClient(addr string, config *LegacyRedisConfig) *redis.Client {
	if config == nil {
		config = DefaultLegacyRedisConfig()
	}

	return redis.NewClient(&redis.Options{
		Addr:         addr,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		MaxRetries:   config.MaxRetries,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		PoolTimeout:  config.PoolTimeout,
	})
}

// DefaultRedisConfig 返回默认 Redis 配置 (向后兼容)
func DefaultRedisConfig() *LegacyRedisConfig {
	return DefaultLegacyRedisConfig()
}

// NewRedisClient 创建 Redis 客户端 (向后兼容)
func NewRedisClient(addr string, config *LegacyRedisConfig) *redis.Client {
	return NewLegacyRedisClient(addr, config)
}

// NewRedisClientFromConfig 从新配置系统创建 Redis 客户端
func NewRedisClientFromConfig(config *RedisConfig) *redis.Client {
	options := &redis.Options{
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
	}

	// 如果启用了 TLS
	if config.TLSEnabled {
		// 这里可以添加 TLS 配置
		// options.TLSConfig = &tls.Config{...}
	}

	// 如果是集群模式
	if config.ClusterMode && len(config.ClusterAddrs) > 0 {
		clusterOptions := &redis.ClusterOptions{
			Addrs:        config.ClusterAddrs,
			Password:     config.Password,
			PoolSize:     config.PoolSize,
			MinIdleConns: config.MinIdleConns,
			MaxRetries:   config.MaxRetries,
			DialTimeout:  config.DialTimeout,
			ReadTimeout:  config.ReadTimeout,
			WriteTimeout: config.WriteTimeout,
			PoolTimeout:  config.PoolTimeout,
		}

		// 对于集群模式，我们需要返回一个包装的客户端
		// 这里暂时返回单节点客户端，实际使用时需要根据具体需求调整
		clusterClient := redis.NewClusterClient(clusterOptions)
		_ = clusterClient // 避免未使用变量警告
		// 集群模式下使用第一个地址作为单节点连接
		options.Addr = config.ClusterAddrs[0]
	}

	return redis.NewClient(options)
}
