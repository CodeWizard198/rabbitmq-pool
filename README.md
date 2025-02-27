# RabbitMQ Connection Pool for Go

[![Go Report Card](https://goreportcard.com/badge/github.com/yourusername/rabbitmq-pool)](https://goreportcard.com/report/github.com/yourusername/rabbitmq-pool)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Features
- 🚀 **Dual Mode**: Separate configurations for producers/consumers  
- 🔄 **Smart Reconnect**: Exponential backoff + Heartbeat detection  
- ⚙️ **Fine-grained Control**:
  - Connection/channel limits  
  - QoS prefetching (consumers)  
  - Publisher confirms (producers)  
- 📊 **Connection Monitoring**: Auto-cleanup dead connections  
- ⏱️ **Context-aware API**: Built-in timeout control  
- 📦 **Channel Pooling**: Reduce resource creation overhead  

---

## Installation
```bash
go get github.com/CodeWizard198/rabbitmq-pool
```

---

## Quick Start

### Producer Example
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

## Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `URL` | string | Required | RabbitMQ server URL |
| `MaxConnections` | int | 3 | Max physical connections |
| `MaxChannels` | int | 50 | Max channels per connection |
| `PrefetchCount` | int | 0 | Consumer prefetch count (0=unlimited) |
| `PrefetchSize` | int | 0 | Consumer prefetch size (bytes) |
| `ConfirmMode` | bool | false | Enable publisher confirms |
| `ReconnectDelay` | time.Duration | 1s | Base reconnection delay |

---

## Best Practices
1. **Separate Pools**: Use dedicated pools for producers/consumers  
2. **Tuning**:
   - Producers: 3-5 connections, enable ConfirmMode  
   - Consumers: Set PrefetchCount based on workload (10-200)  
3. **Error Handling**:
   ```go
   if errors.Is(err, amqp.ErrClosed) {
       // Handle channel-level errors
   }
   ```
4. **Monitoring**:
   - Active connections  
   - Channel utilization rate  
   - Unacked messages count  

---

## Performance
**Test Environment**: 4-core CPU/8GB RAM, RabbitMQ 3.11

| Scenario | Throughput | Latency |
|----------|------------|---------|
| Producer (no confirm) | 18k msg/s | 0.8ms |
| Producer (confirm) | 12.5k msg/s | 1.2ms |
| Consumer (prefetch=100) | 9.5k msg/s | 2.1ms |

---

## Contribution
1. Follow Go coding standards  
2. Add unit tests (coverage >85%)  
3. Update documentation  

Submit issues/PRs to:  
https://github.com/CodeWizard198/rabbitmq-pool

---

## License
MIT License. See [LICENSE](LICENSE) for details.