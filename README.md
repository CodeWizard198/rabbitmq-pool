# RabbitMQ Connection Pool for Go

[![Go Report Card](https://goreportcard.com/badge/github.com/yourusername/rabbitmq-pool)](https://goreportcard.com/report/github.com/yourusername/rabbitmq-pool)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

高效的RabbitMQ连接池实现，支持生产者和消费者专用配置，提供完善的连接管理和故障恢复机制。

---

## 功能特性
- 🚀 **双模式支持**：独立的生产者/消费者连接池配置
- 🔄 **智能重连**：指数退避重试算法 + 心跳检测
- ⚙️ **精细化控制**：
  - 连接数/通道数限制
  - QoS预取策略（消费者）
  - 发布确认模式（生产者）
- 📊 **连接监控**：自动清理无效连接
- ⏱️ **超时控制**：上下文感知的API设计
- 📦 **通道复用**：减少资源创建开销

---

## 安装依赖
```bash
go get github.com/CodeWizard198/rabbitmq-pool
```

---

## 快速开始

### 生产者模式
```go
package main

import (
    "context"
    "time"
    "github.com/CodeWizard198/rabbitmq-pool"
)

func main() {
    conf := &rabbitmq.PoolConfig{
        URL:            "amqp://user:pass@host:5672/vhost",
        MaxConnections: 3,
        MaxChannels:    50,
        ConfirmMode:    true,
        ReconnectDelay: 2 * time.Second,
    }
    
    pool := rabbitmq.NewConnectionPool(rabbitmq.ProducerPool, conf)
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    ch, release, err := pool.GetChannel(ctx)
    if err != nil {
        panic(err)
    }
    defer release()
    
    err = ch.Publish(
        "exchange",
        "routing.key",
        true,
        false,
        amqp.Publishing{
            ContentType:  "text/plain",
            Body:         []byte("message"),
            DeliveryMode: amqp.Persistent,
        },
    )
}
```

### Consumer Example
```go
func startConsumer() {
    conf := &rabbitmq.PoolConfig{
        URL:            "amqp://user:pass@host:5672/vhost",
        MaxConnections: 5,
        MaxChannels:    100,
        PrefetchCount:  30,
        ReconnectDelay: 3 * time.Second,
    }
    
    pool := rabbitmq.NewConnectionPool(rabbitmq.ConsumerPool, conf)
    
    for {
        ch, release, _ := pool.GetChannel(context.Background())
        
        msgs, _ := ch.Consume(
            "queue.name",
            "",
            false,
            false,
            false,
            false,
            nil,
        )
        
        go func() {
            for msg := range msgs {
                // Process message
                msg.Ack(false)
            }
            release()
        }()
    }
}
```

---

## 配置选项

| 参数 | 类型 | 默认值 | 描述 |
|-----------|------|---------|-------------|
| `URL` | string | Required | RabbitMQ连接地址 |
| `MaxConnections` | int | 3 | 最大物理连接数 |
| `MaxChannels` | int | 50 | 单个连接最大通道数 |
| `PrefetchCount` | int | 0 | 消费者预取数量 (0=无限制) |
| `PrefetchSize` | int | 0 | 消费者预取体积 (字节) |
| `ConfirmMode` | bool | false | 生产者确认模式 |
| `ReconnectDelay` | time.Duration | 1s | 基础重连间隔 |

---

## 最佳实践
1. **连接池分离**: 生产/消费使用独立连接池 
2. **参数调优**:
   - 生产者：适当减少连接数（3-5），启用ConfirmMode 
   - 消费者：根据负载调整PrefetchCount（建议10-200  
3. **错误处理**:
   ```go
   if errors.Is(err, amqp.ErrClosed) {
       // Handle channel-level errors
   }
   ```
4. **监控指标**:
   - 活跃连接数  
   - 通道利用率 
   - 未确认消息数 

---

## 性能基准
**测试环境**: 4核CPU/8GB内存，RabbitMQ 3.11

| 场景 | 吞吐量 | 平均延迟 |
|----------|------------|---------|
| 生产者（确认模式关闭） | 18k msg/s | 0.8ms |
| 生产者（确认模式开启） | 12.5k msg/s | 1.2ms |
| 消费者（Prefetch=100） | 9.5k msg/s | 2.1ms |

---

## 贡献指南

欢迎通过Issue和PR参与贡献！开发前请阅读：

1. 遵循Go编码规范 
2. 添加单元测试（覆盖率需>85%）  
3. 更新相关文档 

https://github.com/CodeWizard198/rabbitmq-pool

---

## License
MIT License. See [LICENSE](LICENSE) for details.