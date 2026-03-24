# 压测计划

## 目标

通过消融实验（每次只改变一个变量）产出三组对比数据，用于简历：

1. **Kafka vs NATS**：MQ 层替换对端到端延迟、吞吐量、资源占用的影响
2. **ACK 开启 vs 关闭**：ACK 机制对延迟、吞吐、内存的额外开销
3. **群聊扇出规模**：群成员数对消息扇出延迟的影响曲线

原则：**只看相对值和归一化指标**，不追求绝对连接数。先本地验证工具正确性，再上云出数据。

## 端到端延迟测量方法

所有实验统一使用同一种延迟测量方式：

```
发送端在消息 body 中嵌入 send_ts (UnixNano)
接收端收到后计算: latency = time.Now().UnixNano() - send_ts
```

要求：发送端和接收端在同一台机器上（时钟一致）。
云测试时：压测工具跑在 Server B，goim 跑在 Server A，消息路径为：
```
Server B (sender HTTP) → Server A (Logic→MQ→Job→Comet) → Server B (receiver TCP/WS)
```
延迟包含一个网络往返，但 Kafka 和 NATS 走同一路径，对比公平。

## 统计指标

| 指标 | 计算方式 | 适用实验 |
|------|----------|----------|
| 延迟 P50/P95/P99 | 收集所有 latency 值，排序取分位 | 1, 2, 3 |
| 吞吐量 (msg/s) | 每秒收到的消息数 | 1, 2 |
| 丢失率 | (sent - received) / sent | 1, 2 |
| CPU 使用率 | `docker stats` 采样 | 1, 2 |
| 内存占用 | `docker stats` 采样 | 1, 2 |
| 扇出完成时间 | last_receive_ts - send_ts | 3 |

---

## 实验 1：Kafka vs NATS

### 控制变量

| 变量 | 值 |
|------|-----|
| 改变 | `MQType`：logic.toml + job.toml 中 `"kafka"` vs `"nats"` |
| 固定 | 连接数、推送速率、消息大小、服务器配置、Comet/Logic/Job 代码 |

### 数据路径（绕过 Gateway）

```
bench-mq (sender)                    bench-mq (receiver)
    │                                      ↑
    │ POST Logic /goim/push/room           │ TCP 连接 Comet
    ↓                                      │
  Logic → MQ (Kafka/NATS) → Job → Comet ──┘
```

客户端直连 Comet TCP，加入同一个房间。发送端通过 Logic HTTP 推送房间消息。
消息体：`{"seq": 12345, "ts": 1710000000000000000}`

### 测试矩阵

| 场景 | 连接数 | 推送速率 | 持续时间 | 关注指标 |
|------|--------|----------|----------|----------|
| 低负载 | 2000 | 50 msg/s | 60s | 延迟 P50/P95/P99 |
| 中负载 | 5000 | 200 msg/s | 60s | 延迟 + 吞吐 |
| 高负载 | 8000 | 400 msg/s | 120s | 吞吐、丢失率 |

本地测试只跑低负载场景，验证工具正确性。

### 切换方式

```bash
# NATS 版本
# docker/logic.toml: MQType = "nats"
# docker/job.toml:   MQType = "nats"
docker compose up -d --build logic job

# Kafka 版本
# docker/logic.toml: MQType = "kafka"
# docker/job.toml:   MQType = "kafka"
docker compose up -d --build logic job
```

其他服务不动，保证只有 MQ 层不同。

---

## 实验 2：ACK 开启 vs 关闭

### 控制变量

| 变量 | 值 |
|------|-----|
| 改变 | `ack.enabled`：gateway.toml 中 `true` vs `false` |
| 固定 | 连接数、推送速率、MQ 类型（NATS）、服务器配置 |

### 代码改动

需要在 Gateway 配置中新增 `ack.enabled` 开关：

```toml
[ack]
    enabled = true        # 新增
    retryInterval = 5
    maxRetries = 3
```

`chat.go` 中 `track()` 检查开关：

```go
func (g *Gateway) track(msgID string, mid int64, msg []byte) {
    if !g.c.ACK.Enabled {
        return
    }
    // ... 原有逻辑
}
```

### 数据路径（经过 Gateway）

```
bench-mq (背景负载)                  bench-mq (receivers)
    │ POST Logic /goim/push/room        ↑ TCP 连接 Comet (5000 conns)
    ↓                                   │
  Logic → NATS → Job → Comet ──────────┘

bench-chat (N senders)               bench-chat (N receivers)
    │ POST Gateway /goim/chat            ↑ TCP 连接 Comet
    ↓                                    │
  Gateway → Logic → NATS → Job → Comet ─┘
       │                                 │
       └── ack.Track() (if enabled)      │
           pending map 跟踪              │
                                         │
                            receiver POST /goim/ack/:msg_id (if ACK enabled)
```

策略：先启动 bench-mq 造背景高吞吐负载（5000 连接 × 200 msg/s），
再启动 bench-chat（200 对 × 50 msg/s）测 ACK 开/关差异。
这样服务端承受高吞吐压力，而 bench-chat 的压测端 HTTP 请求量小，
ACK 开/关的对比公平（压测端不是瓶颈）。

### 测试矩阵

| 场景 | 背景负载 (bench-mq) | bench-chat 对数 | 发送速率 | 持续时间 | 关注指标 |
|------|---------------------|-----------------|----------|----------|----------|
| 中负载 | 5000 conn × 200 msg/s | 200 对 | 50 msg/s | 60s | 延迟开销 + 丢失率 |
| 高负载 | 5000 conn × 200 msg/s | 500 对 | 100 msg/s | 60s | 丢失率 + 内存 |

N 对用户：sender[i] 发给 receiver[i]，每个 receiver 通过 TCP 连接 Comet。
ACK 开启时：receiver 收到消息后 POST `/goim/ack/:msg_id`。

### Setup 阶段

压测工具启动时自动完成（幂等，并发执行）：
1. 注册 N×2 个用户（`bench_chat_0` ~ `bench_chat_{2N-1}`）
2. 登录获取 token
3. 两两配对添加好友（0↔1, 2↔3, ...）
4. N 个 receiver 连接 Comet TCP
5. 排空旧消息（2s drain），然后开始发送

### 运行方式

使用 `bench-chat-with-load.sh` 脚本自动编排：

```sh
# ACK 开启
./benchmarks/bench-chat-with-load.sh -ack true \
  -comet <内网IP>:3101 -logic <内网IP>:3111 -gateway http://<内网IP>:3200

# ACK 关闭
./benchmarks/bench-chat-with-load.sh -ack false \
  -comet <内网IP>:3101 -logic <内网IP>:3111 -gateway http://<内网IP>:3200
```

脚本流程：bench-mq 先启动 → 等 15s 建连稳定 → bench-chat 启动 → bench-chat 结束 → 等 bench-mq 结束

---

## 实验 3：群聊扇出规模

### 控制变量

| 变量 | 值 |
|------|-----|
| 改变 | 群成员数：2（单聊 baseline）、10、100、500 |
| 固定 | 发送速率、MQ 类型（NATS）、ACK 关闭、服务器配置 |

### 数据路径

```
bench-fanout (sender)                bench-fanout (M receivers)
    │                                      ↑
    │ POST Gateway /goim/group/:id/chat    │ WebSocket 连接 Comet
    ↓                                      │
  Gateway → Logic /push/mids → NATS → Job → Comet ──┘
         (fan-out to M mids)
```

群聊走 `/goim/push/mids`（逐个推送给成员），不走 `/goim/push/room`。

### 测试矩阵

| 群规模 | 成员数 | 发送速率 | 持续时间 | 关注指标 |
|--------|--------|----------|----------|----------|
| Baseline | 2（单聊） | 10 msg/s | 30s | 基线延迟 |
| 小群 | 10 | 10 msg/s | 30s | 扇出开销 |
| 中群 | 100 | 10 msg/s | 30s | 扇出延迟增长 |
| 大群 | 500 | 10 msg/s | 30s | 扇出极限 |

### 扇出延迟定义

每条消息记录：
- `first_latency`：第一个成员收到的延迟
- `last_latency`：最后一个成员收到的延迟（扇出完成时间）
- `avg_latency`：所有成员的平均延迟

### Setup 阶段

1. 注册 M+1 个用户（1 个发送者 + M 个接收者）
2. 创建群，所有用户加入
3. 所有接收者连接 WebSocket
4. 发送者通过 Gateway 发群消息

---

## 压测工具设计

### 文件结构

```
benchmarks/
├── client/main.go          # 已有：TCP 连接压测
├── push_room/main.go       # 已有：房间推送
├── pkg/                    # 新增：公共库
│   ├── metrics.go          # 延迟直方图、吞吐计数、丢失追踪
│   ├── tcpclient.go        # TCP 客户端（复用已有协议编解码）
│   └── wsclient.go         # WebSocket 客户端（复用 offline-demo 的编解码）
├── bench-mq/main.go        # 新增：实验 1
├── bench-chat/main.go      # 新增：实验 2
└── bench-fanout/main.go    # 新增：实验 3
```

### pkg/metrics.go 核心接口

```go
type Metrics struct {
    latencies []int64       // 所有延迟值 (ns)
    sent      atomic.Int64  // 已发送
    received  atomic.Int64  // 已接收
    mu        sync.Mutex
}

func (m *Metrics) RecordLatency(sendTs int64) // 记录一条延迟
func (m *Metrics) IncSent()                   // 发送计数+1
func (m *Metrics) IncReceived()               // 接收计数+1
func (m *Metrics) Report()                    // 输出统计报告
// Report 输出:
//   Sent: 6000, Received: 5998, Loss: 0.03%
//   Latency P50: 12ms, P95: 28ms, P99: 45ms
//   Throughput: 99.7 msg/s
```

### bench-mq/main.go 流程

```
flags: -conns 500 -rate 10 -duration 60s -comet localhost:3101 -logic localhost:3111 -room test://1

1. 启动 N 个 TCP 客户端连接 Comet
   - mid = 100000 + i（避免和业务用户冲突）
   - room_id = "test://1"
   - accepts = [1000]
2. 等待所有连接建立
3. 启动 sender goroutine:
   - 按 rate 速率 POST Logic /goim/push/room
   - body: {"seq": <seq>, "ts": <UnixNano>}
   - operation=1000, type=test, room=test://1
4. 每个 receiver 收到消息后:
   - 解析 ts，计算 latency
   - metrics.RecordLatency(ts)
5. duration 到期后停止发送
6. 等待 2s 让在途消息到达
7. metrics.Report() 输出结果
```

```sh
go run benchmarks/bench-mq/main.go \
    -conns 500 -rate 10 -duration 60s \
    -comet localhost:3101 -logic localhost:3111
```

### bench-chat/main.go 流程

```
flags: -pairs 200 -rate 50 -duration 60s -gateway http://localhost:3200 -comet localhost:3101 -ack true

1. Setup (幂等, 并发):
   - 注册 pairs×2 个用户
   - 登录获取 token
   - 两两配对添加好友 (0↔1, 2↔3, ...)
2. N 个 receiver 连接 Comet TCP
3. 排空旧消息 (2s drain, 旧消息仍 ACK)
4. sender goroutines 轮流发送:
   - body.content = UnixNano 时间戳
   - 按 rate 速率均匀发送
5. 每个 receiver 收到消息后:
   - ACK (停止重试)
   - sync.Map 去重 (按 msg_id)
   - 解析 content 中的 ts
   - metrics.RecordLatency(ts)
6. 结束后 Report()
```

配合 bench-mq 背景负载使用：

```sh
# ACK 开启
./benchmarks/bench-chat-with-load.sh -ack true \
    -comet <内网IP>:3101 -logic <内网IP>:3111 -gateway http://<内网IP>:3200

# ACK 关闭
./benchmarks/bench-chat-with-load.sh -ack false \
    -comet <内网IP>:3101 -logic <内网IP>:3111 -gateway http://<内网IP>:3200
```

```sh
  # ACK 开启
  go run benchmarks/bench-chat-load/main.go \
    -bg-conns 5000 -bg-rate 200 \
    -pairs 200 -rate 50 -duration 60s \
    -comet 10.206.0.3:3101 -logic 10.206.0.3:3111 \
    -gateway http://10.206.0.3:3200 -ack=true

  # ACK 关闭
  go run benchmarks/bench-chat-load/main.go \
    -bg-conns 8000 -bg-rate 300 \
    -pairs 100 -rate 50 -duration 60s \
    -comet localhost:3101 -logic localhost:3111 \
    -gateway http://localhost:3200 -ack=false
```

### bench-fanout/main.go 流程

```
flags: -members 100 -rate 10 -duration 30s -gateway localhost:3200 -comet localhost:3102

1. Setup:
   - 注册 members+1 个用户
   - 登录获取 token
   - 创建群，所有用户加入
2. members 个用户连接 WebSocket
3. sender 通过 Gateway /goim/group/:id/chat 发消息
   - content 包含 ts
4. 每个 receiver 记录延迟
5. 每条消息额外统计:
   - first_latency: min(所有成员的 latency)
   - last_latency: max(所有成员的 latency)
6. Report() 输出 first/last/avg 的 P50/P95/P99
```

```sh
go run benchmarks/bench-fanout/main.go \
    -members 10 -rate 10 -duration 30s \
    -gateway http://localhost:3200 -comet localhost:3101
```

---

## 代码改动清单

| 文件 | 改动 | 用途 |
|------|------|------|
| `internal/gateway/conf/conf.go` | ACK 结构体加 `Enabled bool` | 实验 2 开关 |
| `internal/gateway/chat.go` | `track()` 检查 `Enabled` | 实验 2 开关 |
| `docker/gateway.toml` | `[ack]` 加 `enabled = true` | 配置 |
| `benchmarks/pkg/metrics.go` | 新增 | 公共统计库 |
| `benchmarks/pkg/tcpclient.go` | 新增 | TCP 客户端封装 |
| `benchmarks/pkg/wsclient.go` | 新增 | WebSocket 客户端封装 |
| `benchmarks/bench-mq/main.go` | 新增 | 实验 1 工具 |
| `benchmarks/bench-chat/main.go` | 新增 | 实验 2 工具 |
| `benchmarks/bench-fanout/main.go` | 新增 | 实验 3 工具 |

---

## 实施顺序

### 第一阶段：工具开发 + 本地验证

1. `benchmarks/pkg/metrics.go` — 公共统计库
2. `benchmarks/pkg/tcpclient.go` — TCP 客户端（从 benchmarks/client 提取）
3. `benchmarks/pkg/wsclient.go` — WebSocket 客户端（从 offline-demo 提取）
4. `benchmarks/bench-mq/main.go` — 实验 1 工具
5. 本地跑 bench-mq（500 连接 + 10 msg/s），验证延迟测量正确
6. `internal/gateway/conf/conf.go` + `chat.go` — ACK 开关
7. `benchmarks/bench-chat/main.go` — 实验 2 工具
8. 本地跑 bench-chat（50 对 + 10 msg/s），验证 ACK 开关有效
9. `benchmarks/bench-fanout/main.go` — 实验 3 工具
10. 本地跑 bench-fanout（10 成员 + 10 msg/s），验证扇出测量正确

### 第二阶段：云服务器压测

11. 租两台云服务器（同地域同可用区）
12. Server A: `docker compose up -d`（goim 全套）
13. Server B: 编译压测工具，调整 ulimit
14. 跑实验 1 全部场景（NATS → Kafka → NATS 重复验证）
15. 跑实验 2 全部场景（ACK on → off）
16. 跑实验 3 全部场景（2 → 10 → 100 → 500）
17. 收集 `docker stats` 数据
18. 整理结果到 `notes/bench-result.md`

---

## 本地测试命令参考

```bash
# 实验 1：Kafka vs NATS（本地，低负载）
go run benchmarks/bench-mq/main.go \
  -conns 500 -rate 10 -duration 60s \
  -comet localhost:3101 -logic localhost:3111

# 实验 2：ACK 对比（使用背景负载脚本）
./benchmarks/bench-chat-with-load.sh -ack true \
  -comet localhost:3101 -logic localhost:3111 -gateway http://localhost:3200

./benchmarks/bench-chat-with-load.sh -ack false \
  -comet localhost:3101 -logic localhost:3111 -gateway http://localhost:3200

# 实验 3：群聊扇出（本地）
go run benchmarks/bench-fanout/main.go \
  -members 10 -rate 10 -duration 30s \
  -gateway localhost:3200 -comet localhost:3102
```

## 预期简历产出

> - 替换 Kafka 为 NATS，推送延迟 P99 从 Xms 降至 Yms，内存占用降低 Z%
> - 实现单聊 ACK 可靠投递机制，投递成功率 99.9%+，额外延迟开销 <Xms
> - 群聊扇出：100 人群 P99 延迟 Xms，500 人群 P99 延迟 Yms
