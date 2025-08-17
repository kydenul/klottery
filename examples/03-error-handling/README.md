# 错误处理最佳实践示例

本示例展示了线程安全抽奖系统的错误处理和生产环境最佳实践。

## 功能演示

1. **参数验证** - 各种无效参数的处理和验证
2. **超时和取消处理** - Context超时和取消的正确处理
3. **Redis连接错误** - 网络连接问题的处理和恢复
4. **并发安全性** - 多goroutine并发访问的安全性验证
5. **错误恢复策略** - 重试、降级、断路器等恢复模式
6. **生产环境配置** - 生产级别的配置和监控

## 运行示例

```bash
cd examples/03-error-handling
go run main.go
```

## 错误处理策略

### 参数验证
- 范围参数验证 (min ≤ max)
- 锁键非空验证
- 计数参数验证 (count > 0)
- 奖品池概率验证

### 超时处理
- Context超时检测
- 部分成功结果返回
- 优雅的操作中断

### 连接错误
- Redis连接失败检测
- 自动重连机制
- 连接池配置优化

### 并发安全
- 多goroutine并发测试
- 锁竞争处理
- 数据竞争检测

### 恢复策略
- **重试机制**: 指数退避重试
- **降级处理**: 本地随机数生成
- **断路器模式**: 快速失败保护

## 生产环境最佳实践

### Redis配置
```go
redis.NewClient(&redis.Options{
    Addr:         "localhost:6379",
    PoolSize:     10,                // 连接池大小
    MinIdleConns: 5,                 // 最小空闲连接
    MaxRetries:   3,                 // 最大重试次数
    DialTimeout:  5 * time.Second,   // 连接超时
    ReadTimeout:  3 * time.Second,   // 读超时
    WriteTimeout: 3 * time.Second,   // 写超时
    PoolTimeout:  4 * time.Second,   // 连接池超时
})
```

### 抽奖引擎配置
```go
config := &LotteryConfig{
    LockTimeout:   30 * time.Second,       // 锁超时
    RetryAttempts: 5,                      // 重试次数
    RetryInterval: 100 * time.Millisecond, // 重试间隔
}
```

### 监控指标
- 响应时间监控
- 成功率统计
- 错误率告警
- 资源使用监控

## 示例输出

```
=== 线程安全抽奖系统 - 错误处理最佳实践 ===

--- 参数验证最佳实践 ---
✓ 正确捕获无效范围错误: invalid range: min must be less than or equal to max
✓ 正确捕获空锁键错误: invalid parameters
✓ 正确捕获无效计数错误: invalid count: must be greater than 0
✓ 正确捕获无效奖品池错误: invalid probability: must be between 0 and 1
✓ 正确捕获概率总和错误: invalid probability: must be between 0 and 1

--- 超时和取消处理 ---
✓ 正确处理超时: draw operation was interrupted
  部分完成: 0/10
✓ 正确处理取消: draw operation was interrupted
  部分完成: 2/20

--- Redis连接错误处理 ---
✓ 正确处理Redis连接错误: redis connection failed
✓ 连接恢复成功，抽奖结果: 73

--- 并发安全性验证 ---
  并发抽奖 #1 成功: 42
  并发抽奖 #2 成功: 87
  ...
✓ 并发测试完成: 成功 10 次, 失败 0 次

--- 错误恢复策略 ---
1. 重试机制演示:
✓ 重试成功
2. 降级处理演示:
✓ 降级处理结果: 42
3. 断路器模式演示:
  请求 1 成功
  请求 2 成功
  ...

--- 生产环境最佳实践 ---
1. Redis连接池配置:
2. 生产级抽奖引擎配置:
3. 健康检查:
✓ 系统健康
4. 执行监控抽奖:
✓ 抽奖成功: 456 (耗时: 2.3ms)
```