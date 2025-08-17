# 示例代码目录

本目录包含了线程安全抽奖系统的各种使用示例，从基础功能到高级特性，帮助您快速上手和深入了解系统功能。

## 📁 目录结构

```
examples/
├── 01-basic/              # 基础使用示例
├── 02-advanced/           # 高级功能示例  
├── 03-error-handling/     # 错误处理最佳实践
├── 04-enhanced/           # 增强功能演示
└── README.md             # 本文件
```

## 🚀 快速开始

### 前置条件

1. **Go环境**: Go 1.19+ 
2. **Redis服务**: Redis 6.0+ 运行在 `localhost:6379`
3. **依赖安装**: 
   ```bash
   go mod tidy
   ```

### 运行示例

每个示例都可以独立运行：

```bash
# 基础使用示例
cd examples/01-basic && go run main.go

# 高级功能示例
cd examples/02-advanced && go run main.go

# 错误处理示例
cd examples/03-error-handling && go run main.go

# 增强功能示例
cd examples/04-enhanced && go run main.go
```

## 📚 示例说明

### 01-basic - 基础使用示例
**适合人群**: 初次使用者  
**学习目标**: 了解基本的抽奖功能

- ✅ Redis连接和配置
- ✅ 创建抽奖引擎
- ✅ 范围抽奖 (DrawInRange)
- ✅ 奖品池抽奖 (DrawFromPrizes)
- ✅ 连续抽奖 (DrawMultiple*)

### 02-advanced - 高级功能示例
**适合人群**: 需要自定义配置的用户  
**学习目标**: 掌握高级配置和优化功能

- ⚙️ 自定义配置 (LotteryConfig)
- 🔄 错误恢复机制
- 🚀 性能优化功能
- 📝 自定义日志记录
- 🔧 运行时配置更新

### 03-error-handling - 错误处理最佳实践
**适合人群**: 生产环境部署者  
**学习目标**: 学习健壮的错误处理和生产实践

- ✅ 参数验证策略
- ⏱️ 超时和取消处理
- 🔌 Redis连接错误处理
- 🔒 并发安全性验证
- 🔄 错误恢复策略 (重试、降级、断路器)
- 🏭 生产环境配置

### 04-enhanced - 增强功能演示
**适合人群**: 需要高级特性的用户  
**学习目标**: 了解系统的增强功能和状态管理

- 🛡️ 带错误恢复的连抽
- 📊 进度回调和监控
- 💾 状态保存和恢复
- 📈 详细的错误统计
- 🎯 部分成功处理

## 🎯 学习路径

### 新手路径
1. **01-basic** → 了解基础功能
2. **02-advanced** → 学习高级配置
3. **03-error-handling** → 掌握错误处理

### 进阶路径
1. **02-advanced** → 高级配置和优化
2. **04-enhanced** → 增强功能和状态管理
3. **03-error-handling** → 生产环境实践

### 生产部署路径
1. **03-error-handling** → 错误处理和最佳实践
2. **02-advanced** → 性能优化配置
3. **04-enhanced** → 监控和恢复机制

## 🔧 配置说明

### Redis配置
所有示例默认使用以下Redis配置：
```go
redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
    DB:   0,
})
```

### 自定义Redis配置
如果您的Redis运行在不同的地址或端口，请修改示例中的配置：
```go
redis.NewClient(&redis.Options{
    Addr:     "your-redis-host:6379",
    Password: "your-password",
    DB:       0,
})
```

## 🐛 故障排除

### 常见问题

1. **Redis连接失败**
   ```
   错误: dial tcp [::1]:6379: connect: connection refused
   ```
   **解决**: 确保Redis服务正在运行
   ```bash
   # macOS (Homebrew)
   brew services start redis
   
   # Linux
   sudo systemctl start redis
   
   # Docker
   docker run -d -p 6379:6379 redis:7-alpine
   ```

2. **模块导入错误**
   ```
   错误: cannot find module thread-safe-lottery
   ```
   **解决**: 确保在项目根目录运行 `go mod tidy`

3. **权限错误**
   ```
   错误: permission denied
   ```
   **解决**: 检查文件权限或使用 `sudo` (不推荐)

### 调试技巧

1. **启用详细日志**
   ```go
   // 使用默认日志记录器而不是静默记录器
   engine := lottery.NewLotteryEngine(rdb)
   ```

2. **检查Redis状态**
   ```bash
   redis-cli ping  # 应该返回 PONG
   redis-cli info  # 查看Redis信息
   ```

3. **监控Redis操作**
   ```bash
   redis-cli monitor  # 实时监控Redis命令
   ```

## 📖 相关文档

- [API文档](../API_DOCUMENTATION.md) - 完整的API参考
- [安装指南](../INSTALLATION_GUIDE.md) - 详细的安装和配置说明
- [性能基准](../PERFORMANCE_BENCHMARK.md) - 性能测试结果
- [README](../README.md) - 项目主要文档

## 💡 提示

- 每个示例都包含详细的中文注释
- 示例代码可以直接复制到您的项目中使用
- 建议按顺序学习示例，从基础到高级
- 生产环境部署前请仔细阅读错误处理示例

## 🤝 贡献

如果您有新的示例想法或发现了问题，欢迎：
1. 提交Issue描述问题或建议
2. 提交Pull Request贡献代码
3. 完善文档和注释

---

**开始您的抽奖系统之旅吧！** 🎲