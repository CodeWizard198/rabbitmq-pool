package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/streadway/amqp"
)

// PoolType 区分连接池类型
type PoolType int

const (
	ProducerPool PoolType = iota // 生产者
	ConsumerPool                 // 消费者
)

type PoolConfig struct {
	URL            string        // rabbitmq连接地址
	MaxConnections int           // 最大连接数
	MaxChannels    int           // 最大通道量
	PrefetchCount  int           // 消费者专属 Qos配置 消费者预取数量 0: 不限制
	PrefetchSize   int           // 消费者专属 Qos配置 消费者预取体积 KB
	ConfirmMode    bool          // 生产者确认模式
	ReconnectDelay time.Duration // 基础重连时间间隔
}

type ConnectionPool struct {
	config        *PoolConfig          // 连接池配置
	poolType      PoolType             // 连接池类型 生产者/消费者
	connections   []*ConnectionWrapper // 连接列表
	mu            sync.Mutex
	closeNotifier chan struct{}
}

type ConnectionWrapper struct {
	conn           *amqp.Connection   // 具体rabbbitmq连接
	channels       chan *amqp.Channel // 可复用的channels
	mu             sync.Mutex         // 互斥锁
	poolType       PoolType           // 连接类型 在创建ConnectionWrapper时由ConnectionPool导入
	activeChannels int                // 活跃的channel数
	closed         bool               // 关闭标识
}

// NewConnectionPool 创建一个新的连接池
// poolType 连接池类型
// config连接池配置
func NewConnectionPool(poolType PoolType, config *PoolConfig) *ConnectionPool {
	if config.ReconnectDelay < time.Second { // 设置最小重连间隔
		config.ReconnectDelay = time.Second
	}

	pool := &ConnectionPool{
		config:        config,
		poolType:      poolType,
		closeNotifier: make(chan struct{}),
	}
	go pool.connectionMonitor()
	return pool
}

// GetChannel 获取channel（使用指数退避重试）
func (p *ConnectionPool) GetChannel(ctx context.Context) (*amqp.Channel, func(), error) {
	for retry := 0; ; retry++ {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
			ch, release, err := p.tryGetChannel()
			if err == nil {
				return ch, release, nil
			}

			delay := p.calculateBackoff(retry)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
	}
}

func (p *ConnectionPool) tryGetChannel() (*amqp.Channel, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 尝试从现有连接获取通道
	for i := 0; i < len(p.connections); i++ {
		cw := p.connections[i]
		if cw.isClosed() {
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			i--
			continue
		}

		ch, release, err := cw.getChannel(p.config)
		if err == nil {
			return ch, release, nil
		}
	}

	// 创建新连接
	if len(p.connections) < p.config.MaxConnections {
		conn, err := amqp.DialConfig(p.config.URL, amqp.Config{
			Heartbeat: 10 * time.Second,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("connection failed: %w", err)
		}

		cw := &ConnectionWrapper{
			conn:     conn,
			channels: make(chan *amqp.Channel, p.config.MaxChannels),
		}

		// 监听连接关闭
		go func() {
			<-conn.NotifyClose(make(chan *amqp.Error))
			cw.markAsClosed()
		}()

		p.connections = append(p.connections, cw)
		ch, release, err := cw.getChannel(p.config)
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
		return ch, release, nil
	}

	return nil, nil, errors.New("max connections reached")
}

// 指数退避计算（使用ReconnectDelay作为基础时间）
func (p *ConnectionPool) calculateBackoff(retry int) time.Duration {
	base := float64(p.config.ReconnectDelay)
	maxDelay := 30 * time.Second

	delay := time.Duration(math.Pow(2, float64(retry))) * time.Duration(base)
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

// 每30s清理一次已关闭的连接
func (p *ConnectionPool) connectionMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.cleanupConnections()
		case <-p.closeNotifier:
			return
		}
	}
}

func (p *ConnectionPool) cleanupConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()

	activeConns := make([]*ConnectionWrapper, 0, len(p.connections))
	for _, cw := range p.connections {
		if !cw.isClosed() {
			activeConns = append(activeConns, cw)
		} else {
			cw.close()
		}
	}
	p.connections = activeConns
}

// 关闭连接池
func (p *ConnectionPool) Close() {
	close(p.closeNotifier)
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, cw := range p.connections {
		cw.close()
	}
	p.connections = nil
}

// 获取channel
func (cw *ConnectionWrapper) getChannel(config *PoolConfig) (*amqp.Channel, func(), error) {
	select {
	case ch := <-cw.channels: // try to get old channel
		return ch, func() { cw.releaseChannel(ch, config) }, nil
	default:
		cw.mu.Lock()
		defer cw.mu.Unlock()

		if cw.activeChannels >= config.MaxChannels {
			return nil, nil, errors.New("max channels reached")
		}

		ch, err := cw.conn.Channel()
		if err != nil {
			cw.markAsClosed()
			return nil, nil, fmt.Errorf("channel creation failed: %w", err)
		}

		if err := setupChannel(ch, config, cw.poolType); err != nil {
			ch.Close()
			return nil, nil, err
		}

		// 监控channel的退出
		closeChan := make(chan *amqp.Error)
		ch.NotifyClose(closeChan)
		go func() {
			<-closeChan
			cw.mu.Lock()
			cw.activeChannels--
			cw.mu.Unlock()
		}()

		cw.activeChannels++
		return ch, func() { cw.releaseChannel(ch, config) }, nil
	}
}

func setupChannel(ch *amqp.Channel, config *PoolConfig, poolType PoolType) error {
	switch poolType {
	case ProducerPool:
		if config.ConfirmMode {
			if err := ch.Confirm(false); err != nil {
				return fmt.Errorf("confirm mode failed: %w", err)
			}
		}
	case ConsumerPool:
		if config.PrefetchCount > 0 || config.PrefetchSize > 0 {
			if err := ch.Qos(config.PrefetchCount, config.PrefetchSize, false); err != nil {
				return fmt.Errorf("qos setup failed: %w", err)
			}
		}
	}
	return nil
}

// 释放channel
func (cw *ConnectionWrapper) releaseChannel(ch *amqp.Channel, config *PoolConfig) {
	// 重置channel的状态 如果消费者配置了PrefetchCount
	if config.PrefetchCount > 0 {
		ch.Qos(0, 0, false) //  Qos
	}

	select {
	case cw.channels <- ch:
	default:
		cw.mu.Lock()
		ch.Close()
		cw.activeChannels--
		cw.mu.Unlock()
	}
}

func (cw *ConnectionWrapper) isClosed() bool {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.closed
}

func (cw *ConnectionWrapper) markAsClosed() {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.closed = true
}

func (cw *ConnectionWrapper) close() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	close(cw.channels)
	for ch := range cw.channels {
		ch.Close()
	}
	cw.conn.Close()
	cw.closed = true
}
