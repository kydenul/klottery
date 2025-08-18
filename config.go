package lottery

import (
	"fmt"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Config 生产环境配置结构
type Config struct {
	// 服务配置
	Server ServerConfig `mapstructure:"server"`

	// Redis 配置
	Redis RedisConfig `mapstructure:"redis"`

	// 抽奖引擎配置
	Lottery LotteryEngineConfig `mapstructure:"lottery"`

	// 安全配置
	Security SecurityConfig `mapstructure:"security"`

	// 监控配置
	Monitoring MonitoringConfig `mapstructure:"monitoring"`

	// 日志配置
	Logging LoggingConfig `mapstructure:"logging"`

	// 熔断器配置
	CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	IdleTimeout  time.Duration `mapstructure:"idle_timeout"`
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

// LotteryEngineConfig 抽奖引擎配置
type LotteryEngineConfig struct {
	// 锁配置
	LockTimeout   time.Duration `mapstructure:"lock_timeout"`
	RetryAttempts int           `mapstructure:"retry_attempts"`
	RetryInterval time.Duration `mapstructure:"retry_interval"`

	// 状态持久化配置
	StateTTL             time.Duration `mapstructure:"state_ttl"`
	MaxSerializationMB   int           `mapstructure:"max_serialization_mb"`
	StateCleanupInterval time.Duration `mapstructure:"state_cleanup_interval"`

	// 性能配置
	BatchSize        int           `mapstructure:"batch_size"`
	ConcurrencyLimit int           `mapstructure:"concurrency_limit"`
	CacheEnabled     bool          `mapstructure:"cache_enabled"`
	CacheTTL         time.Duration `mapstructure:"cache_ttl"`
	CacheMaxSize     int           `mapstructure:"cache_max_size"`

	// 限流配置
	RateLimitEnabled bool    `mapstructure:"rate_limit_enabled"`
	RateLimitRPS     float64 `mapstructure:"rate_limit_rps"`
	RateLimitBurst   int     `mapstructure:"rate_limit_burst"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	// 认证配置
	EnableAuth    bool          `mapstructure:"enable_auth"`
	JWTSecret     string        `mapstructure:"jwt_secret"`
	TokenExpiry   time.Duration `mapstructure:"token_expiry"`
	RefreshExpiry time.Duration `mapstructure:"refresh_expiry"`

	// 加密配置
	EnableEncryption bool   `mapstructure:"enable_encryption"`
	EncryptionKey    string `mapstructure:"encryption_key"`
	EncryptionAlgo   string `mapstructure:"encryption_algo"`

	// 访问控制
	EnableIPWhitelist bool     `mapstructure:"enable_ip_whitelist"`
	IPWhitelist       []string `mapstructure:"ip_whitelist"`
	EnableCORS        bool     `mapstructure:"enable_cors"`
	CORSOrigins       []string `mapstructure:"cors_origins"`

	// 审计配置
	EnableAudit    bool   `mapstructure:"enable_audit"`
	AuditLogPath   string `mapstructure:"audit_log_path"`
	AuditLogLevel  string `mapstructure:"audit_log_level"`
	AuditRetention int    `mapstructure:"audit_retention_days"`
}

// MonitoringConfig 监控配置
type MonitoringConfig struct {
	// Prometheus 配置
	EnableMetrics bool   `mapstructure:"enable_metrics"`
	MetricsAddr   string `mapstructure:"metrics_addr"`
	MetricsPath   string `mapstructure:"metrics_path"`

	// 健康检查配置
	EnableHealthCheck   bool          `mapstructure:"enable_health_check"`
	HealthCheckAddr     string        `mapstructure:"health_check_addr"`
	HealthCheckPath     string        `mapstructure:"health_check_path"`
	HealthCheckInterval time.Duration `mapstructure:"health_check_interval"`

	// 链路追踪配置
	EnableTracing   bool    `mapstructure:"enable_tracing"`
	TracingEndpoint string  `mapstructure:"tracing_endpoint"`
	TracingSampler  float64 `mapstructure:"tracing_sampler"`
	ServiceName     string  `mapstructure:"service_name"`

	// 告警配置
	EnableAlerting   bool        `mapstructure:"enable_alerting"`
	AlertingWebhooks []string    `mapstructure:"alerting_webhooks"`
	AlertingRules    []AlertRule `mapstructure:"alerting_rules"`
}

// AlertRule 告警规则
type AlertRule struct {
	Name        string        `mapstructure:"name"`
	Metric      string        `mapstructure:"metric"`
	Threshold   float64       `mapstructure:"threshold"`
	Operator    string        `mapstructure:"operator"` // >, <, >=, <=, ==, !=
	Duration    time.Duration `mapstructure:"duration"`
	Severity    string        `mapstructure:"severity"` // critical, warning, info
	Description string        `mapstructure:"description"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level      string `mapstructure:"level"`       // debug, info, warn, error
	Format     string `mapstructure:"format"`      // json, console
	Output     string `mapstructure:"output"`      // stdout, stderr, file
	FilePath   string `mapstructure:"file_path"`   // 日志文件路径
	MaxSize    int    `mapstructure:"max_size"`    // MB
	MaxBackups int    `mapstructure:"max_backups"` // 保留文件数
	MaxAge     int    `mapstructure:"max_age"`     // 保留天数
	Compress   bool   `mapstructure:"compress"`    // 是否压缩
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
	// 服务器默认配置
	cm.viper.SetDefault("server.host", "0.0.0.0")
	cm.viper.SetDefault("server.port", 8080)
	cm.viper.SetDefault("server.read_timeout", "30s")
	cm.viper.SetDefault("server.write_timeout", "30s")
	cm.viper.SetDefault("server.idle_timeout", "120s")

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

	// 抽奖引擎默认配置
	cm.viper.SetDefault("lottery.lock_timeout", "30s")
	cm.viper.SetDefault("lottery.retry_attempts", 3)
	cm.viper.SetDefault("lottery.retry_interval", "100ms")
	cm.viper.SetDefault("lottery.state_ttl", "1h")
	cm.viper.SetDefault("lottery.max_serialization_mb", 10)
	cm.viper.SetDefault("lottery.state_cleanup_interval", "1h")
	cm.viper.SetDefault("lottery.batch_size", 100)
	cm.viper.SetDefault("lottery.concurrency_limit", 1000)
	cm.viper.SetDefault("lottery.cache_enabled", true)
	cm.viper.SetDefault("lottery.cache_ttl", "5m")
	cm.viper.SetDefault("lottery.cache_max_size", 10000)
	cm.viper.SetDefault("lottery.rate_limit_enabled", false)
	cm.viper.SetDefault("lottery.rate_limit_rps", 1000.0)
	cm.viper.SetDefault("lottery.rate_limit_burst", 100)

	// 安全默认配置
	cm.viper.SetDefault("security.enable_auth", false)
	cm.viper.SetDefault("security.token_expiry", "24h")
	cm.viper.SetDefault("security.refresh_expiry", "168h")
	cm.viper.SetDefault("security.enable_encryption", false)
	cm.viper.SetDefault("security.encryption_algo", "AES-256-GCM")
	cm.viper.SetDefault("security.enable_ip_whitelist", false)
	cm.viper.SetDefault("security.enable_cors", false)
	cm.viper.SetDefault("security.enable_audit", false)
	cm.viper.SetDefault("security.audit_log_level", "info")
	cm.viper.SetDefault("security.audit_retention_days", 30)

	// 监控默认配置
	cm.viper.SetDefault("monitoring.enable_metrics", true)
	cm.viper.SetDefault("monitoring.metrics_addr", ":9090")
	cm.viper.SetDefault("monitoring.metrics_path", "/metrics")
	cm.viper.SetDefault("monitoring.enable_health_check", true)
	cm.viper.SetDefault("monitoring.health_check_addr", ":8081")
	cm.viper.SetDefault("monitoring.health_check_path", "/health")
	cm.viper.SetDefault("monitoring.health_check_interval", "30s")
	cm.viper.SetDefault("monitoring.enable_tracing", false)
	cm.viper.SetDefault("monitoring.tracing_sampler", 0.1)
	cm.viper.SetDefault("monitoring.service_name", "lottery-service")
	cm.viper.SetDefault("monitoring.enable_alerting", false)

	// 日志默认配置
	cm.viper.SetDefault("logging.level", "info")
	cm.viper.SetDefault("logging.format", "json")
	cm.viper.SetDefault("logging.output", "stdout")
	cm.viper.SetDefault("logging.max_size", 100)
	cm.viper.SetDefault("logging.max_backups", 3)
	cm.viper.SetDefault("logging.max_age", 28)
	cm.viper.SetDefault("logging.compress", true)

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
func (cm *ConfigManager) validateConfig(config *Config) error {
	// 验证服务器配置
	if config.Server.Port <= 0 || config.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", config.Server.Port)
	}

	// 验证 Redis 配置
	if config.Redis.Addr == "" {
		return fmt.Errorf("redis address is required")
	}
	if config.Redis.PoolSize <= 0 {
		return fmt.Errorf("redis pool size must be positive")
	}

	// 验证抽奖引擎配置
	if config.Lottery.LockTimeout <= 0 {
		return fmt.Errorf("lock timeout must be positive")
	}
	if config.Lottery.RetryAttempts < 0 {
		return fmt.Errorf("retry attempts cannot be negative")
	}
	if config.Lottery.MaxSerializationMB <= 0 {
		return fmt.Errorf("max serialization size must be positive")
	}

	// 验证安全配置
	if config.Security.EnableAuth && config.Security.JWTSecret == "" {
		return fmt.Errorf("JWT secret is required when auth is enabled")
	}
	if config.Security.EnableEncryption && config.Security.EncryptionKey == "" {
		return fmt.Errorf("encryption key is required when encryption is enabled")
	}

	// 验证日志配置
	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLogLevels[config.Logging.Level] {
		return fmt.Errorf("invalid log level: %s", config.Logging.Level)
	}

	return nil
}

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
func (cm *ConfigManager) GetConfig() *Config {
	return cm.config
}

// ReloadConfig 重新加载配置
func (cm *ConfigManager) ReloadConfig() (*Config, error) {
	return cm.LoadConfig()
}
