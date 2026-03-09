# GoIM 压测指南

## 概述

goim 自带一套压测工具，位于 `benchmarks/` 目录下。用于测试系统在高并发连接和高频消息推送下的表现。

本文档覆盖：
1. 压测工具介绍
2. 环境准备
3. 压测场景与操作步骤
4. 输出指标解读
5. MQ 替换前后对比方法

---

## 1. 压测工具介绍

```
benchmarks/
├── client/        模拟大量 TCP 客户端（建连接、收消息、统计到达量）
├── push/          按 mid 逐个推送（调用 /goim/push/mids）
├── push_room/     单房间推送（调用 /goim/push/room）
├── push_rooms/    多房间推送（调用 /goim/push/room，遍历多个房间）
└── multi_push/    批量 mid 推送（一次请求包含多个 mid）
```

### 角色分工

```
┌────────────────┐                    ┌────────────────┐
│  push 系列工具  │── HTTP 请求 ──────▶│  Logic :3111   │
│  (消息生产者)   │                    └───────┬────────┘
└────────────────┘                            │
                                         MQ (Kafka/NATS)
                                              │
                                       ┌──────▼────────┐
┌────────────────┐                     │   Job          │
│  client 工具    │◀── TCP 推送 ───────│   → Comet :3101│
│  (消息消费者)   │                     └───────────────┘
└────────────────┘
```

- **client**：模拟 N 个用户，通过 TCP 连接 Comet，接收推送，统计每秒到达消息数
- **push 系列**：模拟消息发送方，通过 HTTP 调用 Logic 的推送接口

---

## 2. 环境准备

### 2.1 编译

```bash
# 编译 goim 服务
make build

# 编译压测工具
make bench-build
```

编译产物：

```
target/
├── comet, logic, job         # goim 服务
├── bench-client              # 压测客户端
├── bench-push                # 按 mid 推送工具
└── bench-push-room           # 房间推送工具
```

### 2.2 启动 goim

压测前需要启动完整的 goim 服务栈：

```bash
# 前提：Discovery(:7171)、Redis(:6379)、Kafka(:9092) 已启动

# 方式一：后台一键启动
make run

# 方式二：前台分别启动（推荐，方便观察日志）
# 终端 1
make comet
# 终端 2
make logic
# 终端 3
make job
```

### 2.3 验证服务正常

```bash
# 检查端口
lsof -i :3101   # Comet TCP
lsof -i :3111   # Logic HTTP

# 简单推送测试
curl -d 'hello' 'http://localhost:3111/goim/push/mids?operation=1000&mids=1'
# 应返回 {"code":0}
```

---

## 3. 压测场景

### 场景一：房间推送（最常用）

> 模拟直播间场景：N 个用户在同一房间，持续推送消息，观察到达率。

**步骤**：

```bash
# 终端 A：启动模拟客户端
make bench-client BENCH_CLIENTS=1000

# 终端 B：启动房间推送
make bench-push-room BENCH_RATE=40
```

**参数说明**：

| 参数 | 默认值 | 含义 |
|---|---|---|
| `BENCH_CLIENTS` | 10000 | 模拟客户端连接数 |
| `BENCH_COMET` | localhost:3101 | Comet TCP 地址 |
| `BENCH_ROOM` | 1 | 房间号 |
| `BENCH_RATE` | 40 | 每秒推送消息数 |
| `BENCH_LOGIC` | localhost:3111 | Logic HTTP 地址 |

**client 工具内部行为**：
1. 创建 `BENCH_CLIENTS` 个 TCP 连接
2. 每个连接使用递增的 uid（从 1 开始）鉴权
3. 所有连接加入房间 `test://1`
4. 每个连接自动维持心跳（240 秒间隔）
5. 收到业务消息时，原子计数器 +1
6. 每 5 秒打印一次统计

**push_room 工具内部行为**：
1. 启动 `BENCH_RATE` 个协程
2. 每个协程每秒向 `/goim/push/room?room=1` 发送一条消息
3. 消息内容：`{"test": <递增序号>}`
4. 总效果：每秒发送 `BENCH_RATE` 条房间消息

---

### 场景二：按 mid 推送

> 模拟单聊场景：对每个用户单独推送消息。

**步骤**：

```bash
# 终端 A：启动模拟客户端
make bench-client BENCH_CLIENTS=1000

# 终端 B：按 mid 推送，持续 60 秒
make bench-push BENCH_CLIENTS=1000 BENCH_DURATION=60
```

**push 工具内部行为**：
1. 按 CPU 核数 ×2 启动协程
2. 将 uid 范围 [0, BENCH_CLIENTS) 均分给各协程
3. 每个协程循环调用 `/goim/push/mids?mids=<uid>`
4. 到达 `BENCH_DURATION` 秒后自动退出

---

### 场景三：自定义参数

```bash
# 大规模：5 万连接 + 每秒 100 条房间消息
make bench-client BENCH_CLIENTS=50000
make bench-push-room BENCH_RATE=100

# 远程服务器
make bench-client BENCH_CLIENTS=10000 BENCH_COMET=192.168.1.100:3101
make bench-push-room BENCH_RATE=40 BENCH_LOGIC=192.168.1.100:3111

# 不同房间号
make bench-push-room BENCH_ROOM=999 BENCH_RATE=40
```

---

## 4. 输出指标解读

### 4.1 client 输出

```
2024-03-08 17:00:05 alive:1000 down:40000 down/s:8000
2024-03-08 17:00:10 alive:1000 down:80000 down/s:8000
2024-03-08 17:00:15 alive:998  down:119840 down/s:7968
```

| 字段 | 含义 |
|---|---|
| `alive` | 当前存活的 TCP 连接数 |
| `down` | 累计收到的业务消息总数 |
| `down/s` | 每秒收到的消息数（5 秒平均值） |

**关键指标：`down/s`**

理论值计算：
- 房间推送：`down/s = BENCH_RATE × alive`
- 例：40 条/秒 × 1000 连接 = 40000 down/s

如果 `down/s` 显著低于理论值，说明有消息丢失或延迟。

### 4.2 push_room 输出

```
2024-03-08 17:00:05 postId:42, response:{"code":0}
2024-03-08 17:00:06 postId:43, response:{"code":0}
```

| 字段 | 含义 |
|---|---|
| `postId` | 推送序号（递增） |
| `response` | Logic HTTP 响应，`code:0` 表示成功 |

### 4.3 push 输出

```
response {"code":0}
```

持续打印每次推送的响应。

---

## 5. MQ 替换前后对比方法

### 5.1 对比流程

```
┌──────────────────────────────────────────────┐
│  第一轮：Kafka 版本（基线）                     │
│  1. 启动 goim（Kafka）                         │
│  2. 跑压测，记录 down/s                        │
│  3. 停止                                      │
├──────────────────────────────────────────────┤
│  第二轮：NATS 版本                              │
│  1. 启动 goim（NATS）                          │
│  2. 同参数跑压测，记录 down/s                    │
│  3. 停止                                      │
├──────────────────────────────────────────────┤
│  对比 down/s，确认无明显退化                     │
└──────────────────────────────────────────────┘
```

### 5.2 具体步骤

#### 第一轮：Kafka 基线

```bash
# 1. 确保使用 Kafka 版本的代码
git checkout main

# 2. 编译
make build && make bench-build

# 3. 启动 goim
make run

# 4. 等待 10 秒，确保所有服务就绪
sleep 10

# 5. 启动客户端（终端 A）
make bench-client BENCH_CLIENTS=10000 2>&1 | tee target/bench-kafka-client.log

# 6. 等待连接建立稳定（观察 alive 达到目标数）
# 7. 启动推送（终端 B）
make bench-push-room BENCH_RATE=40 2>&1 | tee target/bench-kafka-push.log

# 8. 观察 2-3 分钟，记录稳定后的 down/s
# 9. Ctrl+C 停止压测
# 10. 停止 goim
make stop
```

#### 第二轮：NATS 版本

```bash
# 1. 切换到 NATS 版本
git checkout feature/nats

# 2. 编译
make build && make bench-build

# 3. 启动 goim（NATS 版本）
make run

sleep 10

# 4. 同参数压测（终端 A）
make bench-client BENCH_CLIENTS=10000 2>&1 | tee target/bench-nats-client.log

# 5. 同参数推送（终端 B）
make bench-push-room BENCH_RATE=40 2>&1 | tee target/bench-nats-push.log

# 6. 观察 2-3 分钟，记录 down/s
# 7. 停止
make stop
```

### 5.3 结果记录模板

将结果记录到 `notes/bench-result.md`：

```markdown
# 压测结果对比

## 环境
- 机器：MacBook Pro M1 / 16GB
- OS：macOS 14.x
- Go：1.21

## 参数
- 客户端连接数：10000
- 推送速率：40 条/秒（房间推送）
- 观测时长：3 分钟

## 结果

| 指标 | Kafka | NATS | 差异 |
|---|---|---|---|
| alive（稳定连接数） | 10000 | 10000 | - |
| down/s（消息到达/秒） | 400000 | 398000 | -0.5% |
| Logic HTTP 响应 | 全部 code:0 | 全部 code:0 | - |

## 结论
NATS 替换后性能无明显退化，down/s 差异在 ±2% 以内。
```

### 5.4 多轮梯度测试（可选）

如果时间允许，可以做多组参数对比：

| 轮次 | 连接数 | 推送速率 | 观测重点 |
|---|---|---|---|
| 1 | 1000 | 10 条/秒 | 功能验证，确认链路跑通 |
| 2 | 10000 | 40 条/秒 | 标准压测，对比 down/s |
| 3 | 50000 | 100 条/秒 | 高压测试，观察是否有消息丢失 |

每轮都对比 Kafka 和 NATS 的 `down/s`。

---

## 6. 注意事项

### 6.1 系统资源限制

大量连接会受操作系统文件描述符限制：

```bash
# 查看当前限制
ulimit -n

# 临时调大（需要 > 连接数）
ulimit -n 65535
```

### 6.2 client 连接建立是渐进的

client 工具中每个连接启动前会随机 sleep 0~120 秒：

```go
func startClient(key int64) {
    time.Sleep(time.Duration(rand.Intn(120)) * time.Second)  // ← 随机延迟
    // ...
}
```

所以 `alive` 数不会立即达到目标值，需要等待 **2 分钟左右** 才能全部连接建立。观察 `alive` 字段达到目标数后，再启动 push 工具。

### 6.3 心跳超时

client 的心跳间隔是 240 秒（4 分钟）。如果 Comet 配置的心跳超时小于这个值，连接会被 Comet 主动断开。确保 Comet 配置中：

```toml
[protocol]
heartbeatMax = 5
heartbeat = "5m"    # 需要 > 240s
```

### 6.4 debug 日志对性能的影响

压测时建议关闭 debug 日志，大量 `log.Infof` 会显著影响性能：

```toml
# target/comet.toml
debug = false
```

或者启动时不加 `-debug=true`：

```bash
# 压测时用 make run（不带 -alsologtostderr）
make run
```

### 6.5 压测日志位置

所有压测日志保存在 `target/` 下：

```
target/
├── bench-client.log        # client 统计输出
├── bench-push.log          # push 响应日志
├── bench-push-room.log     # push_room 响应日志
├── bench-kafka-client.log  # Kafka 对比专用
└── bench-nats-client.log   # NATS 对比专用
```
