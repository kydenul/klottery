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

// RedisConfig 优化的Redis配置
type RedisConfig struct {
	// 连接池配置
	PoolSize     int `json:"pool_size"`      // 连接池大小
	MinIdleConns int `json:"min_idle_conns"` // 最小空闲连接数
	MaxRetries   int `json:"max_retries"`    // 最大重试次数

	// 超时配置
	DialTimeout  time.Duration `json:"dial_timeout"`  // 连接超时
	ReadTimeout  time.Duration `json:"read_timeout"`  // 读取超时
	WriteTimeout time.Duration `json:"write_timeout"` // 写入超时

	// 连接管理
	PoolTimeout        time.Duration `json:"pool_timeout"`         // 连接池超时
	IdleTimeout        time.Duration `json:"idle_timeout"`         // 空闲连接超时
	IdleCheckFrequency time.Duration `json:"idle_check_frequency"` // 空闲连接检查频率
}

// DefaultRedisConfig returns default Redis Config
func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		PoolSize:           50,              // 增加连接池大小以支持高并发
		MinIdleConns:       10,              // 保持一定数量的空闲连接
		MaxRetries:         3,               // 适度的重试次数
		DialTimeout:        5 * time.Second, // 连接超时
		ReadTimeout:        3 * time.Second, // 读取超时
		WriteTimeout:       3 * time.Second, // 写入超时
		PoolTimeout:        4 * time.Second, // 连接池超时
		IdleTimeout:        5 * time.Minute, // 空闲连接超时
		IdleCheckFrequency: 1 * time.Minute, // 空闲连接检查频率
	}
}

// NewRedisClient returns Redis Client
func NewRedisClient(addr string, config *RedisConfig) *redis.Client {
	if config == nil {
		config = DefaultRedisConfig()
	}

	return redis.NewClient(&redis.Options{
		Addr:               addr,
		PoolSize:           config.PoolSize,
		MinIdleConns:       config.MinIdleConns,
		MaxRetries:         config.MaxRetries,
		DialTimeout:        config.DialTimeout,
		ReadTimeout:        config.ReadTimeout,
		WriteTimeout:       config.WriteTimeout,
		PoolTimeout:        config.PoolTimeout,
		IdleTimeout:        config.IdleTimeout,
		IdleCheckFrequency: config.IdleCheckFrequency,
	})
}
