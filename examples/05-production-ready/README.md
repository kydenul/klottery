# 生产环境就绪示例

本示例展示了如何在生产环境中使用线程安全抽奖系统，包括配置管理、错误处理、熔断器、监控等生产级功能。

## 🚀 功能特性

### 1. 配置管理
- 基于 Viper 的配置管理
- 支持多环境配置 (dev/prod)
- 配置热更新
- 环境变量支持

### 2. 错误处理
- 增强的错误类型系统
- 指数退避重试机制
- 错误分类和严重程度
- 详细的错误上下文

### 3. 熔断器
- 基于 gobreaker 的熔断器
- 自动故障检测
- 状态监控
- 可配置的熔断策略

### 4. 监控和健康检查
- Prometheus 指标集成
- 健康检查端点
- 性能监控
- 告警支持

## 📁 文件结构

```
examples/05-production-ready/
├── main.go                    # 主程序
├── README.md                  # 本文件
├── config.yaml               # 配置文件
├── docker-compose.yml        # Docker 编排文件
└── Dockerfile                # Docker 镜像文件
```

## 🔧 配置说明

### 环境变量

```bash
# 设置环境
export LOTTERY_ENV=production

# Redis 配置
export LOTTERY_REDIS_ADDR=redis:6379
export LOTTERY_REDIS_PASSWORD=your-password

# 安全配置
export LOTTERY_SECURITY_JWT_SECRET=your-jwt-secret
export LOTTERY_SECURITY_ENCRYPTION_KEY=your-encryption-key

# 监控配置
export LOTTERY_MONITORING_JAEGER_ENDPOINT=http://jaeger:14268/api/traces
```

### 配置文件优先级

1. 环境变量 (最高优先级)
2. 配置文件 (config.yaml)
3. 默认值 (最低优先级)

## 🏃‍♂️ 运行示例

### 本地运行

```bash
# 1. 启动 Redis
docker run -d -p 6379:6379 redis:7-alpine

# 2. 设置环境变量
export LOTTERY_ENV=development

# 3. 运行示例
cd examples/05-production-ready
go run main.go
```

### Docker 运行

```bash
# 1. 构建镜像
docker build -t lottery-prod .

# 2. 使用 Docker Compose 启动
docker-compose up -d

# 3. 查看日志
docker-compose logs -f lottery
```

### Kubernetes 部署

```bash
# 1. 创建配置映射
kubectl create configmap lottery-config --from-file=config.yaml

# 2. 创建密钥
kubectl create secret generic lottery-secrets \
  --from-literal=jwt-secret=your-jwt-secret \
  --from-literal=encryption-key=your-encryption-key

# 3. 部署应用
kubectl apply -f k8s/
```

## 📊 监控和观测

### Prometheus 指标

访问 `http://localhost:9090/metrics` 查看指标：

```
# 抽奖相关指标
lottery_draws_total{type="range",status="success"} 1000
lottery_draws_total{type="prize",status="success"} 500
lottery_draw_duration_seconds{type="range"} 0.001

# 熔断器指标
circuit_breaker_state{name="lottery-engine"} 0
circuit_breaker_requests_total{name="lottery-engine"} 1500
circuit_breaker_failures_total{name="lottery-engine"} 10

# Redis 指标
redis_connection_pool_size 100
redis_connection_pool_idle 10
redis_operations_total{operation="get",status="success"} 2000
```

### 健康检查

访问 `http://localhost:8081/health` 查看健康状态：

```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z",
  "checks": {
    "redis": {
      "status": "healthy",
      "response_time": "1ms"
    },
    "circuit_breaker": {
      "status": "healthy",
      "state": "closed",
      "success_rate": 0.99
    },
    "memory": {
      "status": "healthy",
      "usage": "45%"
    }
  }
}
```

### 链路追踪

如果启用了 Jaeger 追踪，可以在 Jaeger UI 中查看请求链路：

- 访问 `http://localhost:16686`
- 搜索服务名: `lottery-service`
- 查看请求链路和性能分析

## 🚨 告警配置

### 告警规则示例

```yaml
alerting_rules:
  - name: "high_error_rate"
    metric: "lottery_error_rate"
    threshold: 0.05
    operator: ">"
    duration: "5m"
    severity: "warning"
    description: "抽奖错误率超过 5%"

  - name: "circuit_breaker_open"
    metric: "circuit_breaker_state"
    threshold: 2
    operator: "=="
    duration: "30s"
    severity: "critical"
    description: "熔断器已打开"

  - name: "redis_connection_failure"
    metric: "redis_connection_failures"
    threshold: 10
    operator: ">"
    duration: "1m"
    severity: "critical"
    description: "Redis 连接失败次数过多"
```

### Webhook 通知

支持多种通知方式：

```yaml
alerting_webhooks:
  - "https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK"
  - "https://events.pagerduty.com/integration/YOUR-KEY/enqueue"
  - "https://api.dingtalk.com/robot/send?access_token=YOUR-TOKEN"
```

## 🔒 安全最佳实践

### 1. 认证和授权

```go
// 启用 JWT 认证
security:
  enable_auth: true
  jwt_secret: "${JWT_SECRET}"
  token_expiry: "8h"
```

### 2. 数据加密

```go
// 启用数据加密
security:
  enable_encryption: true
  encryption_key: "${ENCRYPTION_KEY}"
  encryption_algo: "AES-256-GCM"
```

### 3. 网络安全

```go
// IP 白名单
security:
  enable_ip_whitelist: true
  ip_whitelist:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"
```

### 4. 审计日志

```go
// 启用审计日志
security:
  enable_audit: true
  audit_log_path: "/var/log/lottery/audit.log"
  audit_retention_days: 90
```

## 🔧 故障排除

### 常见问题

1. **配置文件未找到**
   ```
   错误: Config File "config" Not Found
   解决: 确保配置文件在正确路径，或设置环境变量
   ```

2. **Redis 连接失败**
   ```
   错误: Redis connection failed
   解决: 检查 Redis 服务状态和网络连接
   ```

3. **熔断器打开**
   ```
   错误: circuit breaker is open
   解决: 检查下游服务状态，等待熔断器自动恢复
   ```

### 调试技巧

1. **启用调试日志**
   ```yaml
   logging:
     level: "debug"
   ```

2. **检查指标**
   ```bash
   curl http://localhost:9090/metrics | grep lottery
   ```

3. **查看健康状态**
   ```bash
   curl http://localhost:8081/health | jq
   ```

## 📈 性能调优

### Redis 连接池优化

```yaml
redis:
  pool_size: 200        # 根据并发量调整
  min_idle_conns: 20    # 保持足够的空闲连接
  max_retries: 5        # 增加重试次数
  dial_timeout: "10s"   # 适当增加超时时间
```

### 抽奖引擎优化

```yaml
lottery:
  batch_size: 200           # 增加批处理大小
  concurrency_limit: 5000   # 根据系统能力调整
  cache_enabled: true       # 启用缓存
  cache_ttl: "10m"         # 适当的缓存时间
```

### 熔断器调优

```yaml
circuit_breaker:
  failure_ratio: 0.5    # 50% 失败率触发熔断
  min_requests: 5       # 最少请求数
  timeout: "60s"        # 熔断器超时时间
```

## 📚 相关文档

- [配置管理文档](../../docs/configuration.md)
- [错误处理指南](../../docs/error-handling.md)
- [监控集成指南](../../docs/monitoring.md)
- [部署指南](../../docs/deployment.md)
- [安全配置指南](../../docs/security.md)

## 🤝 贡献

如果您发现问题或有改进建议，欢迎：

1. 提交 Issue
2. 创建 Pull Request
3. 完善文档

---

**生产环境就绪，让抽奖系统更可靠！** 🎯✨