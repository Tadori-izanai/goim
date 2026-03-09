# NATS 入门教程

## 1. NATS 简介

NATS 是一个轻量级、高性能的云原生消息系统，核心特点：

- **极简** — 核心只有 Pub/Sub 和 Request/Reply 两种模式，无 broker 级别的消息持久化（JetStream 扩展可提供持久化）
- **高性能** — 单节点可达百万级 msg/s，延迟微秒级
- **零依赖** — 不依赖 ZooKeeper 等外部组件，单二进制即可运行
- **原生集群** — 内置集群、超级集群（Gateway）、Leaf Node 等拓扑

### NATS vs Kafka

| 特性 | NATS（Core） | Kafka |
|------|-------------|-------|
| 消息持久化 | 否（JetStream 支持） | 是 |
| 消息回溯/重放 | 否（JetStream 支持） | 是 |
| 部署复杂度 | 极低，单二进制 | 需要 ZooKeeper / KRaft |
| 吞吐 | 极高 | 极高 |
| 延迟 | 微秒级 | 毫秒级 |
| 适用场景 | 实时推送、命令分发 | 日志流、事件溯源 |

对于本项目（goim）的 IM 推送场景，消息是即时推送的，不需要持久化回溯，NATS Core 的 Pub/Sub 模式即可满足。

---

## 2. Docker 安装与配置

### 2.1 拉取镜像

```bash
docker pull nats:latest
```

### 2.2 启动 NATS Server

```bash
docker run -d --name nats \
    -p 4222:4222 \
    -p 6222:6222 \
    -p 8222:8222 \
    nats:latest
```

**端口说明：**

- `4222` — 客户端连接端口（Go SDK 连接此端口）
- `6222` — 集群路由端口（多节点组网使用）
- `8222` — HTTP 监控端口（浏览器访问 `http://localhost:8222` 查看状态）

### 2.3 验证启动

```bash
# 查看容器状态
docker ps | grep nats

# 查看监控信息
curl http://localhost:8222/varz
```

### 2.4 启用 JetStream（可选，持久化）

如需消息持久化能力，添加 `-js` 参数：

```bash
docker run -d --name nats \
    -p 4222:4222 \
    -p 6222:6222 \
    -p 8222:8222 \
    nats:latest -js
```

### 2.5 自定义配置文件（可选）

创建 `nats-server.conf`：

```conf
listen: 0.0.0.0:4222
http: 0.0.0.0:8222

# 认证（可选）
# authorization {
#     user: admin
#     password: secret
# }
```

挂载运行：

```bash
docker run -d --name nats \
    -p 4222:4222 \
    -p 8222:8222 \
    -v $(pwd)/nats-server.conf:/etc/nats/nats-server.conf \
    nats:latest -c /etc/nats/nats-server.conf
```

---

## 3. Go 客户端 SDK

### 3.1 安装

```bash
go get github.com/nats-io/nats.go
```

### 3.2 核心 API

```go
import "github.com/nats-io/nats.go"
```

#### 连接

```go
// 连接到默认地址 nats://localhost:4222
nc, err := nats.Connect(nats.DefaultURL)
defer nc.Close()

// 连接到指定地址
nc, err := nats.Connect("nats://192.168.1.100:4222")

// 多地址（自动 failover）
nc, err := nats.Connect("nats://host1:4222,nats://host2:4222")
```

#### 发布消息

```go
// 发布 []byte 到 subject
err := nc.Publish("subject-name", []byte("hello"))
```

#### 订阅消息

```go
// 异步订阅
sub, err := nc.Subscribe("subject-name", func(msg *nats.Msg) {
    fmt.Printf("Received: %s\n", string(msg.Data))
})

// 队列订阅（同一 queue group 内负载均衡）
sub, err := nc.QueueSubscribe("subject-name", "worker-group", func(msg *nats.Msg) {
    fmt.Printf("Received: %s\n", string(msg.Data))
})
```

#### 关闭连接

```go
nc.Drain()  // 优雅关闭，处理完已接收消息后断开
nc.Close()  // 立即关闭
```

---

## 4. 在本项目中使用 NATS

### 4.1 概念映射

本项目中 Kafka 的使用方式与 NATS 的映射关系：

| Kafka 概念 | NATS 概念 | 本项目用途 |
|-----------|----------|----------|
| Topic | Subject | 推送消息的通道，如 `"goim-push-topic"` |
| Producer | Publisher (`nc.Publish`) | logic 层发布推送消息 |
| Consumer / Consumer Group | Subscriber / Queue Subscribe | job 层消费推送消息 |
| Broker 地址 | Server URL | 连接地址 |

### 4.2 配置结构

当前 `conf.Nats` 为空结构体，建议填充为：

```go
type Nats struct {
    Addr    string   // NATS server URL, e.g. "nats://localhost:4222"
    Subject string   // 发布/订阅的 subject, 对应 Kafka 的 Topic
}
```

TOML 配置示例：

```toml
MQType = "nats"

[Nats]
    Addr = "nats://localhost:4222"
    Subject = "goim-push-topic"
```

### 4.3 实现 NatsProducer

参照现有 `KafkaProducer` 的模式，实现 `Producer` 接口：

```go
package dao

import (
    "github.com/Terry-Mao/goim/internal/logic/conf"
    "github.com/nats-io/nats.go"
)

type NatsProducer struct {
    conn    *nats.Conn
    subject string
}

var _ Producer = new(NatsProducer)

func NewNatsProducer(c *conf.Nats) *NatsProducer {
    nc, err := nats.Connect(c.Addr)
    if err != nil {
        panic(err)
    }
    return &NatsProducer{
        conn:    nc,
        subject: c.Subject,
    }
}

func (p *NatsProducer) ProduceMessage(key string, msg []byte) error {
    return p.conn.Publish(p.subject, msg)
}

func (p *NatsProducer) Close() error {
    p.conn.Close()
    return nil
}
```

> **注意：** NATS Core 的 Publish 没有 key 的概念。`key` 参数在此被忽略。
> 如需按 key 分发到不同 subject，可改为 `p.conn.Publish(p.subject+"."+key, msg)`。

### 4.4 对比 KafkaProducer

```
KafkaProducer                          NatsProducer
─────────────                          ────────────
kafkaPub kafka.SyncProducer            conn *nats.Conn
c.Topic                                c.Subject
kafkaPub.SendMessage(msg)              conn.Publish(subject, data)
kafkaPub.Close()                       conn.Close()
```

两者都实现 `Producer` 接口，上层 `produce.go` 完全无感知。
