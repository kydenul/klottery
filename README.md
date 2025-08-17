# 🎲 线程安全抽奖系统 (Thread-Safe Lottery System)

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![Redis](https://img.shields.io/badge/Redis-6.0+-DC382D?style=flat&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Test Coverage](https://img.shields.io/badge/Coverage-95%2B-brightgreen)](./coverage.html)

一个高性能、线程安全的分布式抽奖系统，基于 Redis 实现分布式锁和状态持久化，支持范围抽奖、奖品池抽奖、批量抽奖等多种抽奖模式。

## ✨ 核心特性

### 🔒 线程安全与分布式锁
- **分布式锁机制**: 基于 Redis 实现的高性能分布式锁
- **锁超时保护**: 防止死锁，支持自定义超时时间
- **原子操作**: 使用 Lua 脚本确保操作原子性
- **并发安全**: 支持多实例并发访问

### 🎯 多样化抽奖模式
- **范围抽奖**: 在指定数值范围内随机抽取
- **奖品池抽奖**: 基于概率权重的奖品抽取
- **批量抽奖**: 支持一次性进行多次抽奖
- **恢复机制**: 支持中断后的状态恢复

### 💾 状态持久化
- **Redis 持久化**: 抽奖状态自动保存到 Redis
- **状态恢复**: 支持从中断点恢复抽奖操作
- **TTL 管理**: 自动清理过期状态数据
- **序列化优化**: 高效的 JSON 序列化/反序列化

### 🚀 高性能设计
- **连接池**: Redis 连接池管理
- **批量操作**: 支持批量抽奖减少网络开销
- **缓存优化**: 智能缓存机制提升性能
- **异步处理**: 支持异步操作和回调

### 🛡️ 错误处理与监控
- **重试机制**: 指数退避重试策略
- **错误恢复**: 完善的错误处理和恢复机制
- **性能监控**: 内置性能指标收集
- **详细日志**: 可配置的日志记录

## 🏗️ 系统架构

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Client App    │    │   Client App    │    │   Client App    │
└─────────┬───────┘    └─────────┬───────┘    └─────────┬───────┘
          │                      │                      │
          └──────────────────────┼──────────────────────┘
                                 │
                    ┌─────────────▼─────────────┐
                    │    Lottery Engine         │
                    │  ┌─────────────────────┐  │
                    │  │ Distributed Lock    │  │
                    │  │ Manager             │  │
                    │  └─────────────────────┘  │
                    │  ┌─────────────────────┐  │
                    │  │ State Persistence   │  │
                    │  │ Manager             │  │
                    │  └─────────────────────┘  │
                    │  ┌─────────────────────┐  │
                    │  │ Performance         │  │
                    │  │ Monitor             │  │
                    │  └─────────────────────┘  │
                    └─────────────┬─────────────┘
                                  │
                    ┌─────────────▼─────────────┐
                    │        Redis              │
                    │  ┌─────────────────────┐  │
                    │  │ Distributed Locks   │  │
                    │  └─────────────────────┘  │
                    │  ┌─────────────────────┐  │
                    │  │ State Storage       │  │
                    │  └─────────────────────┘  │
                    └───────────────────────────┘
```

## 🚀 快速开始

### 环境要求

- **Go**: 1.24.6+
- **Redis**: 6.0+
- **内存**: 建议 512MB+
- **网络**: Redis 网络连接

### 安装

```bash
go get github.com/kydenul/lottery
```

### 基础使用

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/go-redis/redis/v8"
    "github.com/kydenul/lottery"
)

func main() {
    // 1. 初始化 Redis 客户端
    rdb := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
        DB:   0,
    })
    defer rdb.Close()

    // 2. 创建抽奖引擎
    engine := lottery.NewLotteryEngine(rdb)

    ctx := context.Background()

    // 3. 范围抽奖 (1-100)
    result, err := engine.DrawInRange(ctx, "user:123", 1, 100)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("抽奖结果: %d\n", result)

    // 4. 奖品池抽奖
    prizes := []lottery.Prize{
        {ID: "first", Name: "一等奖", Probability: 0.1, Value: 1000},
        {ID: "second", Name: "二等奖", Probability: 0.2, Value: 500},
        {ID: "third", Name: "三等奖", Probability: 0.7, Value: 100},
    }

    prize, err := engine.DrawFromPrizes(ctx, "activity:123", prizes)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("中奖: %s (价值: %d)\n", prize.Name, prize.Value)
}
```

## 📚 详细文档

### 核心组件

#### 1. LotteryEngine - 抽奖引擎
主要的抽奖接口，提供所有抽奖功能：

```go
// 创建引擎
engine := lottery.NewLotteryEngine(redisClient)

// 自定义配置
config := &lottery.LotteryConfig{
    LockTimeout:   30 * time.Second,
    RetryAttempts: 3,
    RetryInterval: 100 * time.Millisecond,
}
engine := lottery.NewLotteryEngineWithConfig(redisClient, config)
```

#### 2. 抽奖模式

**范围抽奖**
```go
// 单次抽奖
result, err := engine.DrawInRange(ctx, "user:123", 1, 100)

// 批量抽奖
results, err := engine.DrawMultipleInRange(ctx, "user:123", 1, 100, 5)

// 带恢复的批量抽奖
multiResult, err := engine.DrawMultipleInRangeWithRecovery(ctx, "user:123", 1, 100, 10)
```

**奖品池抽奖**
```go
prizes := []lottery.Prize{
    {ID: "gold", Name: "金奖", Probability: 0.1, Value: 1000},
    {ID: "silver", Name: "银奖", Probability: 0.3, Value: 500},
    {ID: "bronze", Name: "铜奖", Probability: 0.6, Value: 100},
}

// 单次抽奖
prize, err := engine.DrawFromPrizes(ctx, "activity:123", prizes)

// 批量抽奖
prizeResults, err := engine.DrawMultipleFromPrizes(ctx, "activity:123", prizes, 5)
```

#### 3. 状态管理

```go
// 保存状态
err := engine.SaveDrawState(ctx, drawState)

// 加载状态
state, err := engine.LoadDrawState(ctx, "lockKey")

// 恢复抽奖
result, err := engine.ResumeMultiDrawInRange(ctx, "lockKey", 1, 100, 10)

// 回滚操作
err := engine.RollbackMultiDraw(ctx, drawState)
```

#### 4. 性能优化

```go
// 带进度回调的优化抽奖
progressCallback := func(completed, total int, currentResult any) {
    fmt.Printf("进度: %d/%d, 当前结果: %v\n", completed, total, currentResult)
}

result, err := engine.DrawMultipleInRangeOptimized(
    ctx, "user:123", 1, 100, 1000, progressCallback,
)
```

### 配置选项

```go
type LotteryConfig struct {
    LockTimeout   time.Duration // 锁超时时间 (默认: 30s)
    RetryAttempts int           // 重试次数 (默认: 3)
    RetryInterval time.Duration // 重试间隔 (默认: 100ms)
}
```

### 错误处理

系统定义了完整的错误类型：

```go
// 常见错误
ErrLockAcquisitionFailed  // 锁获取失败
ErrRedisConnectionFailed  // Redis 连接失败
ErrInvalidParameters      // 参数验证失败
ErrInvalidRange          // 无效范围
ErrInvalidProbability    // 概率值无效
ErrDrawStateCorrupted    // 状态数据损坏
```

## 🎯 使用场景

### 1. 电商促销活动
```go
// 限时抢购抽奖
prizes := []lottery.Prize{
    {ID: "iphone", Name: "iPhone 15", Probability: 0.001, Value: 8000},
    {ID: "coupon", Name: "优惠券", Probability: 0.1, Value: 100},
    {ID: "points", Name: "积分", Probability: 0.899, Value: 10},
}

prize, err := engine.DrawFromPrizes(ctx, "flash_sale:20241201", prizes)
```

### 2. 游戏道具抽取
```go
// 装备抽取
equipment := []lottery.Prize{
    {ID: "legendary", Name: "传说装备", Probability: 0.01, Value: 10000},
    {ID: "epic", Name: "史诗装备", Probability: 0.05, Value: 5000},
    {ID: "rare", Name: "稀有装备", Probability: 0.2, Value: 1000},
    {ID: "common", Name: "普通装备", Probability: 0.74, Value: 100},
}

// 十连抽
results, err := engine.DrawMultipleFromPrizes(ctx, "player:123:gacha", equipment, 10)
```

### 3. 营销活动
```go
// 每日签到奖励
dailyRewards := []lottery.Prize{
    {ID: "bonus", Name: "奖金", Probability: 0.05, Value: 1000},
    {ID: "discount", Name: "折扣券", Probability: 0.15, Value: 50},
    {ID: "points", Name: "积分", Probability: 0.8, Value: 10},
}

reward, err := engine.DrawFromPrizes(ctx, "daily_checkin:user:456", dailyRewards)
```

## 🔧 高级功能

### 1. 自定义随机数生成器

```go
// 实现 SecureRandomGenerator 接口
type CustomRNG struct{}

func (c *CustomRNG) GenerateSecureRandom(min, max int) (int, error) {
    // 自定义随机数生成逻辑
    return customRandomLogic(min, max), nil
}

// 使用自定义 RNG
engine.SetRandomGenerator(&CustomRNG{})
```

### 2. 自定义日志记录器

```go
// 实现 Logger 接口
type CustomLogger struct{}

func (l *CustomLogger) Debug(format string, args ...interface{}) {
    // 自定义调试日志
}

func (l *CustomLogger) Info(format string, args ...interface{}) {
    // 自定义信息日志
}

func (l *CustomLogger) Error(format string, args ...interface{}) {
    // 自定义错误日志
}

// 设置自定义日志记录器
engine.SetLogger(&CustomLogger{})
```

### 3. 性能监控

```go
// 获取性能指标
metrics := engine.GetPerformanceMetrics()
fmt.Printf("总抽奖次数: %d\n", metrics.TotalDraws)
fmt.Printf("成功率: %.2f%%\n", metrics.GetSuccessRate())
fmt.Printf("平均响应时间: %v\n", metrics.GetAverageDrawTime())
```

## 📊 性能基准

### 基准测试结果

```
BenchmarkSerializeDrawState/Small_10_draws-14     1607664    759.0 ns/op    656.14 MB/s
BenchmarkSerializeDrawState/Large_1000_draws-14    81570   14762 ns/op    499.87 MB/s
BenchmarkDeserializeDrawState/Small_10_draws-14   306423    3922 ns/op    126.99 MB/s
BenchmarkDeserializeDrawState/Large_1000_draws-14  15385   73462 ns/op    100.45 MB/s

BenchmarkDrawInRange-14                           500000     2456 ns/op
BenchmarkDrawFromPrizes-14                        300000     4123 ns/op
BenchmarkDrawMultipleInRange-14                    50000    28456 ns/op
```

### 性能特点

- **序列化性能**: 500-800 MB/s
- **反序列化性能**: 100-140 MB/s  
- **单次抽奖延迟**: < 3ms
- **批量抽奖吞吐**: > 35,000 ops/s
- **并发支持**: 1000+ 并发连接

## 🧪 测试

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行基准测试
go test -bench=. -benchmem

# 生成测试覆盖率报告
go test -cover -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 测试覆盖

- **单元测试**: 95%+ 代码覆盖率
- **集成测试**: Redis 集成测试
- **基准测试**: 性能基准测试
- **边界测试**: 边界条件和错误场景

## 📁 项目结构

```
lottery/
├── README.md                          # 项目文档
├── go.mod                             # Go 模块定义
├── go.sum                             # 依赖版本锁定
├── 
├── # 核心代码
├── interfaces.go                      # 接口定义
├── lottery_engine.go                  # 抽奖引擎主逻辑
├── distributed_lock_manager.go        # 分布式锁管理
├── state_persistence.go               # 状态持久化
├── prize.go                          # 奖品相关
├── lottery_result.go                 # 抽奖结果
├── monitor.go                        # 性能监控
├── logger.go                         # 日志接口
├── errs.go                           # 错误定义
├── consts.go                         # 常量定义
├── utils.go                          # 工具函数
├── lottery_config.go                 # 配置管理
├── secure_random_gnerator.go         # 安全随机数
├── 
├── # 测试文件
├── lottery_test.go                   # 主要功能测试
├── lottery_engine_state_test.go      # 状态管理测试
├── state_persistence_test.go         # 持久化测试
├── state_persistence_integration_test.go  # 集成测试
├── state_persistence_edge_cases_test.go   # 边界测试
├── state_persistence_benchmark_test.go    # 基准测试
├── benchmark_test.go                 # 性能测试
├── 
├── # 示例代码
└── examples/                         # 使用示例
    ├── README.md                     # 示例说明
    ├── 01-basic/                     # 基础使用
    ├── 02-advanced/                  # 高级功能
    ├── 03-error-handling/            # 错误处理
    └── 04-enhanced/                  # 增强功能
```

## 🌟 项目优点

### 1. 🔒 **高可靠性**
- **分布式锁**: 基于 Redis 的分布式锁确保并发安全
- **原子操作**: Lua 脚本保证操作原子性
- **状态持久化**: 完整的状态保存和恢复机制
- **错误恢复**: 完善的错误处理和重试机制

### 2. 🚀 **高性能**
- **连接池管理**: 高效的 Redis 连接池
- **批量操作**: 减少网络开销的批量处理
- **序列化优化**: 高效的 JSON 序列化
- **缓存机制**: 智能缓存提升响应速度

### 3. 🎯 **功能丰富**
- **多种抽奖模式**: 范围抽奖、奖品池抽奖、批量抽奖
- **概率控制**: 精确的概率权重控制
- **状态管理**: 完整的状态保存、加载、恢复
- **监控统计**: 内置性能监控和统计

### 4. 🛠️ **易于使用**
- **简洁 API**: 直观易用的接口设计
- **丰富示例**: 完整的使用示例和文档
- **配置灵活**: 支持自定义配置和扩展
- **类型安全**: 完整的类型定义和验证

### 5. 🧪 **测试完善**
- **高覆盖率**: 95%+ 的测试覆盖率
- **多层测试**: 单元测试、集成测试、基准测试
- **边界测试**: 完整的边界条件和异常场景测试
- **性能测试**: 详细的性能基准测试

## ⚠️ 项目不足

### 1. 🔧 **技术限制**
- **Redis 依赖**: 强依赖 Redis，无法在无 Redis 环境使用
- **网络延迟**: 分布式操作存在网络延迟
- **内存消耗**: 大量状态数据可能消耗较多内存
- **单点故障**: Redis 故障会影响整个系统

### 2. 📊 **功能局限**
- **概率算法**: 目前仅支持基础的概率权重算法
- **统计分析**: 缺少详细的抽奖数据分析功能
- **实时监控**: 监控功能相对简单，缺少实时告警
- **数据导出**: 缺少数据导出和报表功能

### 3. 🔐 **安全考虑**
- **权限控制**: 缺少细粒度的权限控制机制
- **审计日志**: 缺少完整的操作审计日志
- **数据加密**: 状态数据未加密存储
- **访问限制**: 缺少 IP 白名单等访问控制

### 4. 🌐 **扩展性**
- **水平扩展**: Redis 集群支持有限
- **多数据中心**: 缺少跨数据中心的支持
- **插件机制**: 缺少插件化的扩展机制
- **协议支持**: 仅支持 Redis 协议

## 🔮 优化建议

### 1. 🚀 **性能优化**

#### 连接池优化
```go
// 建议配置
redis.NewClient(&redis.Options{
    Addr:         "localhost:6379",
    PoolSize:     100,              // 连接池大小
    MinIdleConns: 10,               // 最小空闲连接
    MaxRetries:   3,                // 最大重试次数
    DialTimeout:  5 * time.Second,  // 连接超时
    ReadTimeout:  3 * time.Second,  // 读取超时
    WriteTimeout: 3 * time.Second,  // 写入超时
})
```

#### 批量操作优化
```go
// 使用 Pipeline 减少网络往返
pipe := rdb.Pipeline()
for _, operation := range operations {
    pipe.Set(ctx, operation.Key, operation.Value, operation.TTL)
}
_, err := pipe.Exec(ctx)
```

#### 序列化优化
```go
// 考虑使用更高效的序列化格式
// 1. Protocol Buffers
// 2. MessagePack  
// 3. 自定义二进制格式
```

### 2. 🔧 **功能增强**

#### 多级缓存
```go
// 添加本地缓存层
type CachedLotteryEngine struct {
    engine     *LotteryEngine
    localCache *sync.Map
    cacheTTL   time.Duration
}
```

#### 概率算法增强
```go
// 支持更多概率分布
type ProbabilityDistribution interface {
    Sample() int
    SetWeights(weights []float64)
}

// 正态分布
type NormalDistribution struct{}
// 泊松分布  
type PoissonDistribution struct{}
// 自定义分布
type CustomDistribution struct{}
```

#### 实时监控
```go
// 集成 Prometheus 监控
type PrometheusMonitor struct {
    drawCounter    prometheus.Counter
    errorCounter   prometheus.Counter
    latencyHist    prometheus.Histogram
}
```

### 3. 🔐 **安全增强**

#### 权限控制
```go
// 基于角色的访问控制
type RBACManager struct {
    roles       map[string][]Permission
    userRoles   map[string][]string
}

type Permission struct {
    Resource string // lottery, prize, state
    Action   string // read, write, delete
}
```

#### 数据加密
```go
// 状态数据加密存储
type EncryptedStatePersistence struct {
    cipher     cipher.AEAD
    persistence *StatePersistenceManager
}
```

#### 审计日志
```go
// 操作审计
type AuditLogger struct {
    logger Logger
}

func (a *AuditLogger) LogOperation(userID, operation string, params interface{}) {
    // 记录操作日志
}
```

### 4. 🌐 **架构优化**

#### 微服务化
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Gateway   │    │   Gateway   │    │   Gateway   │
└──────┬──────┘    └──────┬──────┘    └──────┬──────┘
       │                  │                  │
┌──────▼──────┐    ┌──────▼──────┐    ┌──────▼──────┐
│ Lottery     │    │ Prize       │    │ State       │
│ Service     │    │ Service     │    │ Service     │
└─────────────┘    └─────────────┘    └─────────────┘
```

#### 消息队列集成
```go
// 异步处理
type AsyncLotteryEngine struct {
    engine    *LotteryEngine
    publisher MessagePublisher
    consumer  MessageConsumer
}
```

#### 多数据中心支持
```go
// 跨数据中心复制
type MultiDCLotteryEngine struct {
    engines map[string]*LotteryEngine
    router  DCRouter
}
```

### 5. 📊 **监控和运维**

#### 健康检查
```go
// 健康检查端点
func (e *LotteryEngine) HealthCheck() HealthStatus {
    return HealthStatus{
        Redis:      e.checkRedisHealth(),
        Locks:      e.checkLockHealth(),
        Memory:     e.checkMemoryUsage(),
        Timestamp:  time.Now(),
    }
}
```

#### 配置热更新
```go
// 支持配置热更新
type ConfigManager struct {
    config   *LotteryConfig
    watchers []ConfigWatcher
}

func (c *ConfigManager) UpdateConfig(newConfig *LotteryConfig) error {
    // 热更新配置
}
```

#### 自动扩缩容
```go
// 基于负载的自动扩缩容
type AutoScaler struct {
    metrics    MetricsCollector
    scaler     InstanceScaler
    thresholds ScalingThresholds
}
```

## 🤝 贡献指南

我们欢迎所有形式的贡献！

### 贡献方式

1. **报告问题**: 在 Issues 中报告 bug 或提出功能请求
2. **提交代码**: 通过 Pull Request 提交代码改进
3. **完善文档**: 改进文档和示例
4. **分享经验**: 分享使用经验和最佳实践

### 开发流程

1. Fork 项目到您的 GitHub 账户
2. 创建功能分支: `git checkout -b feature/amazing-feature`
3. 提交更改: `git commit -m 'Add amazing feature'`
4. 推送分支: `git push origin feature/amazing-feature`
5. 创建 Pull Request

### 代码规范

- 遵循 Go 官方代码规范
- 添加必要的单元测试
- 更新相关文档
- 确保所有测试通过

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🙏 致谢

感谢以下开源项目的支持：

- [Redis](https://redis.io/) - 高性能内存数据库
- [go-redis](https://github.com/go-redis/redis) - Go Redis 客户端
- [testify](https://github.com/stretchr/testify) - Go 测试框架

## 📞 联系我们

- **项目主页**: https://github.com/kydenul/lottery
- **问题反馈**: https://github.com/kydenul/lottery/issues
- **邮箱**: kydenul@example.com

---

**让抽奖变得简单而可靠！** 🎲✨