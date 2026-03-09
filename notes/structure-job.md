# Job 数据结构详解

## 一、Job 结构体

**定义**：`internal/job/job.go:19`

```go
type Job struct {
    c            *conf.Config        // 配置
    consumer     *cluster.Consumer   // Kafka 消费者
    cometServers map[string]*Comet   // Comet 服务器列表（key: hostname）

    rooms      map[string]*Room      // 房间列表（key: roomID）
    roomsMutex sync.RWMutex          // 房间列表的读写锁
}
```

### 字段说明

| 字段 | 类型 | 作用 |
|---|---|---|
| `c` | `*conf.Config` | Job 的配置信息 |
| `consumer` | `*cluster.Consumer` | Kafka 消费者，从 Kafka 读取推送消息 |
| `cometServers` | `map[string]*Comet` | 所有 Comet 服务器的 gRPC 客户端，key 是 hostname |
| `rooms` | `map[string]*Room` | 房间列表，用于房间广播的批量优化 |
| `roomsMutex` | `sync.RWMutex` | 保护 rooms 的并发读写 |

### 核心职责

1. **消费 Kafka 消息**
   - 从 Kafka Topic 读取 Logic 发布的推送消息
   - 解析 `PushMsg` 并分发处理

2. **管理 Comet 连接**
   - 通过 Discovery 动态发现 Comet 节点
   - 为每个 Comet 创建 gRPC 客户端
   - 监听 Comet 节点变化（上线/下线）

3. **房间消息批量处理**
   - 维护房间列表，合并同一房间的多条消息
   - 批量推送，减少 gRPC 调用次数

### 关键方法

```go
// 消费 Kafka 消息
func (j *Job) Consume() {
    for {
        select {
        case msg := <-j.consumer.Messages():
            pushMsg := new(pb.PushMsg)
            proto.Unmarshal(msg.Value, pushMsg)
            j.push(context.Background(), pushMsg)
        }
    }
}

// 监听 Comet 节点变化
func (j *Job) watchComet(c *naming.Config) {
    // 通过 Discovery 监听 Comet 节点
    // 节点变化时更新 j.cometServers
}

// 获取或创建房间
func (j *Job) getRoom(roomID string) *Room {
    // 双重检查锁，确保线程安全
}
```

---

## 二、Comet 结构体

**定义**：`internal/job/comet.go:59`

```go
type Comet struct {
    serverID      string                        // Comet 服务器 ID（hostname）
    client        comet.CometClient             // gRPC 客户端
    pushChan      []chan *comet.PushMsgReq      // 推送消息队列（多个）
    roomChan      []chan *comet.BroadcastRoomReq // 房间广播队列（多个）
    broadcastChan chan *comet.BroadcastReq      // 全局广播队列（单个）
    pushChanNum   uint64                         // pushChan 轮询计数器
    roomChanNum   uint64                         // roomChan 轮询计数器
    routineSize   uint64                         // 协程数量

    ctx    context.Context                      // 上下文
    cancel context.CancelFunc                   // 取消函数
}
```

### 字段说明

| 字段 | 类型 | 作用 |
|---|---|---|
| `serverID` | `string` | Comet 服务器的唯一标识（hostname） |
| `client` | `comet.CometClient` | gRPC 客户端，用于调用 Comet 的 gRPC 接口 |
| `pushChan` | `[]chan *PushMsgReq` | **多个**推送消息队列，用于负载均衡 |
| `roomChan` | `[]chan *BroadcastRoomReq` | **多个**房间广播队列，用于负载均衡 |
| `broadcastChan` | `chan *BroadcastReq` | **单个**全局广播队列 |
| `pushChanNum` | `uint64` | 原子计数器，用于轮询选择 pushChan |
| `roomChanNum` | `uint64` | 原子计数器，用于轮询选择 roomChan |
| `routineSize` | `uint64` | 协程数量（默认 32） |
| `ctx` / `cancel` | `context` | 用于优雅关闭 |

### 核心设计：多队列 + 多协程

**为什么要多个队列？**

```
单队列问题：
pushChan (1个) → 1个协程处理 → 串行调用 gRPC → 慢

多队列方案：
pushChan[0] → 协程0 ──┐
pushChan[1] → 协程1 ──┼→ 并发调用 gRPC → 快
pushChan[2] → 协程2 ──┘
...
pushChan[31] → 协程31
```

**负载均衡**：

```go
func (c *Comet) Push(arg *comet.PushMsgReq) error {
    // 原子递增，轮询选择队列
    idx := atomic.AddUint64(&c.pushChanNum, 1) % c.routineSize
    c.pushChan[idx] <- arg
    return nil
}
```

### 初始化流程

```go
func NewComet(in *naming.Instance, c *conf.Comet) (*Comet, error) {
    cmt := &Comet{
        serverID:      in.Hostname,
        pushChan:      make([]chan *PushMsgReq, 32),  // 32个队列
        roomChan:      make([]chan *BroadcastRoomReq, 32),
        broadcastChan: make(chan *BroadcastReq, 32),
        routineSize:   32,
    }

    // 创建 gRPC 客户端
    cmt.client, _ = newCometClient(grpcAddr)

    // 启动 32 个协程，每个处理一个队列
    for i := 0; i < 32; i++ {
        cmt.pushChan[i] = make(chan *PushMsgReq, 1024)
        cmt.roomChan[i] = make(chan *BroadcastRoomReq, 1024)
        go cmt.process(cmt.pushChan[i], cmt.roomChan[i], cmt.broadcastChan)
    }

    return cmt, nil
}
```

### 处理协程

```go
func (c *Comet) process(pushChan, roomChan, broadcastChan) {
    for {
        select {
        case pushArg := <-pushChan:
            // 调用 Comet gRPC: PushMsg
            c.client.PushMsg(ctx, pushArg)

        case roomArg := <-roomChan:
            // 调用 Comet gRPC: BroadcastRoom
            c.client.BroadcastRoom(ctx, roomArg)

        case broadcastArg := <-broadcastChan:
            // 调用 Comet gRPC: Broadcast
            c.client.Broadcast(ctx, broadcastArg)

        case <-c.ctx.Done():
            return  // 优雅退出
        }
    }
}
```

---

## 三、Room 结构体

**定义**：`internal/job/room.go:25`

```go
type Room struct {
    c     *conf.Room              // 配置
    job   *Job                    // 所属 Job
    id    string                  // 房间 ID（如 "live://1000"）
    proto chan *protocol.Proto    // 消息队列
}
```

### 字段说明

| 字段 | 类型 | 作用 |
|---|---|---|
| `c` | `*conf.Room` | 房间配置（批量大小、超时时间等） |
| `job` | `*Job` | 所属的 Job 实例 |
| `id` | `string` | 房间 ID，如 `"live://1000"` |
| `proto` | `chan *protocol.Proto` | 消息队列，缓冲区大小为 `Batch*2` |

### 核心职责：批量合并推送

**问题**：如果房间有 1000 人，收到 100 条消息，需要调用 gRPC 多少次？

- **不优化**：100 次 gRPC 调用（每条消息一次）
- **优化后**：1-10 次 gRPC 调用（批量合并）

**Room 的作用**：

1. 收集同一房间的多条消息
2. 合并成一个大包（最多 `Batch` 条）
3. 一次性推送给 Comet

### 批量合并逻辑

```go
func (r *Room) pushproc(batch int, sigTime time.Duration) {
    var (
        n   int                          // 当前累积的消息数
        buf = bytes.NewWriterSize(4096)  // 缓冲区
    )

    for {
        p := <-r.proto  // 读取消息

        if p != roomReadyProto {
            // 写入缓冲区
            p.WriteTo(buf)
            n++

            // 达到批量大小或超时，立即推送
            if n >= batch || time.Since(last) > sigTime {
                r.job.broadcastRoomRawBytes(r.id, buf.Buffer())
                buf = bytes.NewWriterSize(4096)
                n = 0
            }
        }
    }
}
```

**触发推送的条件**：

1. 消息数达到 `batch`（如 20 条）
2. 距离上次推送超过 `sigTime`（如 1 秒）

**示例**：

```
时间轴：
0ms:  收到消息1 → 写入 buf
10ms: 收到消息2 → 写入 buf
20ms: 收到消息3 → 写入 buf
...
100ms: 收到消息20 → 达到 batch=20 → 推送！
```

---

## 四、数据流图

### 完整推送流程

```
Kafka
  ↓
Job.Consume()
  ↓
解析 PushMsg
  ├─ Type=PUSH → j.push()
  │    ↓
  │    根据 server 找到 Comet
  │    ↓
  │    comet.Push(PushMsgReq)
  │    ↓
  │    轮询选择 pushChan[idx]
  │    ↓
  │    协程 process() 读取
  │    ↓
  │    client.PushMsg() [gRPC]
  │
  ├─ Type=ROOM → j.pushRoom()
  │    ↓
  │    j.getRoom(roomID)
  │    ↓
  │    room.Push(proto)
  │    ↓
  │    room.pushproc() 批量合并
  │    ↓
  │    j.broadcastRoomRawBytes()
  │    ↓
  │    遍历所有 Comet
  │    ↓
  │    comet.BroadcastRoom() [gRPC]
  │
  └─ Type=BROADCAST → j.broadcast()
       ↓
       遍历所有 Comet
       ↓
       comet.Broadcast() [gRPC]
```

---

## 五、关键设计点

### 1. Comet 的多队列设计

**目的**：提高并发性能

```
32 个队列 + 32 个协程 = 32 倍并发
```

> ith 协程多路处理 pushChan[i], roomChan[i] 和 broadcastChan[i], i=0,...,31

### 2. Room 的批量合并

**目的**：减少 gRPC 调用次数

```
100 条消息 → 合并成 5 个批次 → 5 次 gRPC 调用
```

### 3. Job 的动态服务发现

**目的**：自动感知 Comet 节点变化

```
Comet-1 上线 → Discovery 通知 → Job 创建 gRPC 客户端
Comet-2 下线 → Discovery 通知 → Job 关闭 gRPC 客户端
```

---

## 六、配置示例

```toml
# job.toml

[kafka]
  brokers = ["127.0.0.1:9092"]
  topic = "goim-push-topic"
  group = "goim-job-group"

[comet]
  routineSize = 32      # 每个 Comet 的协程数
  routineChan = 1024    # 每个队列的缓冲区大小

[room]
  batch = 20            # 批量大小
  signal = 1000         # 超时时间（毫秒）
  idle = 60000          # 空闲超时（毫秒）
```

---

## 七、总结

| 结构体 | 核心职责 | 关键设计 |
|---|---|---|
| **Job** | 消费 Kafka，管理 Comet 连接 | 动态服务发现 |
| **Comet** | 封装 gRPC 客户端，并发调用 | 多队列 + 多协程 |
| **Room** | 房间消息批量合并 | 批量推送优化 |

**数据流**：
```
Kafka → Job → Comet → gRPC → Comet Server
              ↓
            Room（批量合并）
```
