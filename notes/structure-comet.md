# Comet 数据结构详解

## 一、用生活场景类比

想象一个**大型购物中心**：

```
购物中心 (Server)
├── 楼层 A (Bucket 0)
│   ├── 商铺 1 (Channel) - 张三的连接
│   ├── 商铺 2 (Channel) - 李四的连接
│   └── 活动区 (Room) - 直播间 1000
│       ├── 张三在这里
│       └── 李四在这里
├── 楼层 B (Bucket 1)
│   ├── 商铺 3 (Channel) - 王五的连接
│   └── 活动区 (Room) - 直播间 2000
│       └── 王五在这里
└── ...
```

**对应关系**：
- **Server（购物中心）** = Comet 服务器
- **Bucket（楼层）** = 连接分组，减少锁竞争
- **Channel（商铺）** = 单个 WebSocket 连接
- **Room（活动区）** = 聊天室/直播间
- **Key（商铺号）** = 连接的唯一标识

---

## 二、核心概念详解

### 1. Server（服务器）

**定义**：Comet 服务器的主体

```go
type Server struct {
    buckets   []*Bucket       // 多个 Bucket（默认 32 个）
    rpcClient logic.LogicClient  // Logic gRPC 客户端
    serverID  string           // 服务器 ID
}
```

**作用**：
- 管理所有的 Bucket
- 提供 gRPC 接口给 Job 调用
- 与 Logic 通信（鉴权、心跳）

**类比**：购物中心的管理中心

---

### 2. Bucket（桶）

**定义**：连接的分组容器

```go
type Bucket struct {
    chs   map[string]*Channel  // key → Channel 映射
    rooms map[string]*Room     // roomID → Room 映射
}
```

**为什么要分桶？**

假设有 100 万个连接：
- **不分桶**：所有连接用一把大锁，并发性能差
- **分 32 个桶**：每个桶管理 3 万个连接，锁竞争减少 32 倍

**如何分配到桶？**

```go
// 根据 Key 的 Hash 值分配
func (s *Server) Bucket(subKey string) *Bucket {
    idx := cityhash.CityHash32([]byte(subKey), 32) % 32
    return s.buckets[idx]
}
```

**示例**：
- Key = "server1_conn123" → Hash → Bucket 5
- Key = "server1_conn456" → Hash → Bucket 18

**类比**：购物中心的不同楼层，分散客流

---

### 3. Channel（通道）

**定义**：单个 WebSocket 连接

```go
type Channel struct {
    Mid      int64              // 用户 ID
    Key      string             // 连接 Key（唯一标识）
    IP       string             // 客户端 IP
    Room     *Room              // 所属房间
    signal   chan *Proto        // 消息队列
    watchOps map[int32]struct{} // 订阅的操作码
}
```

**关键字段**：
- **Key**：连接的唯一标识，格式 `{ServerID}_{ConnID}`
  - 例如：`server1_1234567890`
- **Mid**：用户 ID（鉴权时传入）
- **watchOps**：订阅的操作码列表
  - 例如：`[1000, 1001, 1002]` 表示只接收这三种消息

**消息推送流程**：
```go
// 1. 检查是否订阅了这个操作码
if !channel.NeedPush(op) {
    return  // 不推送
}

// 2. 推送消息到 signal 队列
channel.Push(proto)

// 3. dispatch 协程从 signal 读取并发送到 WebSocket
proto := channel.Ready()  // 阻塞等待
ws.WriteMessage(proto)
```

**类比**：商铺（每个客户的专属柜台）

---

### 4. Room（房间）

**定义**：聊天室/直播间

```go
type Room struct {
    ID     string              // 房间 ID（如 "live://1000"）
    Online int32               // 在线人数
    next   *Channel            // 链表头（房间内的所有 Channel）
}
```

**作用**：
- 管理同一个房间内的所有连接
- 支持房间广播（一次推送给房间内所有人）

**数据结构**：双向链表

```
Room "live://1000"
  ↓
Channel A ↔ Channel B ↔ Channel C
(张三)      (李四)      (王五)
```

**房间广播流程**：
```go
// 遍历房间内的所有 Channel
for ch := room.next; ch != nil; ch = ch.Next {
    if ch.NeedPush(op) {
        ch.Push(proto)
    }
}
```

**类比**：活动区（所有参与活动的人）

---

### 5. Key（连接标识）

**格式**：`{ServerID}_{ConnID}`

**示例**：
- `server1_1234567890`
- `comet-node-01_9876543210`

**作用**：
- 唯一标识一个连接
- 用于 Hash 分配到 Bucket
- 存储在 Redis 中（Key → ServerID 映射）

**生成时机**：
- 客户端连接时，Comet 调用 Logic.Connect()
- Logic 生成 Key 并返回

---

## 三、完整的数据流

### 场景：用户 A 给用户 B 发消息

```
1. 外部调用 Logic HTTP API
   POST /goim/push/mids?operation=1000&mids=123

2. Logic 查询 Redis
   mid=123 → keys=["server1_conn456", "server2_conn789"]
   (用户 B 有两个设备在线)

3. Logic 发布到 Kafka
   {
     "operation": 1000,
     "server": "server1",
     "keys": ["server1_conn456"],
     "msg": "Hello"
   }

4. Job 消费 Kafka
   根据 server 字段，调用对应 Comet 的 gRPC

5. Job → Comet gRPC
   PushMsg(keys=["server1_conn456"], op=1000, msg="Hello")

6. Comet 处理
   a. 根据 Key Hash 找到 Bucket
      "server1_conn456" → Bucket 5

   b. 在 Bucket 中找到 Channel
      Bucket.chs["server1_conn456"] → Channel

   c. 检查 Channel 是否订阅了 op=1000
      if !channel.NeedPush(1000) { return }

   d. 推送消息到 Channel
      channel.Push(proto)

   e. dispatch 协程发送到 WebSocket
      ws.WriteMessage(proto)

7. 用户 B 收到消息
```

---

## 四、关键代码解析

### 1. 根据 Key 找到 Channel

```go
// internal/comet/grpc/server.go:47
func (s *CometServer) PushMsg(ctx context.Context, req *pb.PushMsgReq) (*pb.PushMsgReply, error) {
    for _, key := range req.Keys {
        // 1. 根据 Key 找到 Bucket
        bucket := s.srv.Bucket(key)

        // 2. 在 Bucket 中找到 Channel
        if channel := bucket.Channel(key); channel != nil {
            // 3. 检查是否订阅了这个操作码
            if !channel.NeedPush(req.ProtoOp) {
                continue
            }

            // 4. 推送消息
            channel.Push(req.Proto)
        }
    }
    return &pb.PushMsgReply{}, nil
}
```

### 2. Bucket.Channel() 实现

```go
// internal/comet/bucket.go
func (b *Bucket) Channel(key string) *Channel {
    b.cLock.RLock()
    ch := b.chs[key]
    b.cLock.RUnlock()
    return ch
}
```

### 3. Channel.NeedPush() 实现

```go
// internal/comet/channel.go:56
func (c *Channel) NeedPush(op int32) bool {
    c.mutex.RLock()
    _, ok := c.watchOps[op]  // 检查是否在订阅列表中
    c.mutex.RUnlock()
    return ok
}
```

### 4. Channel.Push() 实现

```go
// internal/comet/channel.go:67
func (c *Channel) Push(p *protocol.Proto) error {
    select {
    case c.signal <- p:  // 非阻塞发送
        return nil
    default:
        return errors.ErrSignalFullMsgDropped  // 队列满了，丢弃
    }
}
```

---

## 五、图解结构

```
Server (Comet 服务器)
│
├─ Bucket[0] (Hash: 0-31)
│  ├─ chs (map[string]*Channel)
│  │  ├─ "server1_conn123" → Channel {Mid: 123, Key: "...", watchOps: [1000,1001]}
│  │  └─ "server1_conn456" → Channel {Mid: 456, Key: "...", watchOps: [1000]}
│  │
│  └─ rooms (map[string]*Room)
│     └─ "live://1000" → Room {Online: 2, next: Channel链表}
│
├─ Bucket[1] (Hash: 32-63)
│  └─ ...
│
└─ Bucket[31] (Hash: 992-1023)
   └─ ...
```

---

## 六、常见问题

### 1. 为什么要用 Hash 分桶？

**不分桶的问题**：
```go
// 所有连接用一把锁
var channels map[string]*Channel
var lock sync.RWMutex

// 每次查找都要加锁
lock.RLock()
ch := channels[key]
lock.RUnlock()
```

**分桶的好处**：
```go
// 32 个桶，每个桶独立加锁
buckets := make([]*Bucket, 32)

// 只锁对应的桶
bucket := buckets[hash(key) % 32]
bucket.lock.RLock()
ch := bucket.chs[key]
bucket.lock.RUnlock()
```

锁竞争减少 32 倍！

### 2. Key 和 Mid 的区别？

| 概念 | 含义 | 示例 | 唯一性 |
|---|---|---|---|
| **Mid** | 用户 ID | 123 | 全局唯一 |
| **Key** | 连接 ID | server1_conn456 | 全局唯一 |

**关系**：
- 一个 Mid 可以有多个 Key（多设备登录）
- 一个 Key 只对应一个 Mid

**Redis 存储**：
```
# Mid → Keys 映射
mid:123 → ["server1_conn456", "server2_conn789"]

# Key → Server 映射
key:server1_conn456 → "server1"
```

### 3. watchOps 是什么？

**作用**：过滤消息，只推送订阅的操作码

**示例**：
```go
// 客户端连接时指定
token := {
    "mid": 123,
    "accepts": [1000, 1001, 1002]  // 只接收这三种消息
}

// Comet 存储到 Channel
channel.Watch(1000, 1001, 1002)

// 推送时检查
if channel.NeedPush(1000) {  // true
    channel.Push(proto)
}

if channel.NeedPush(2000) {  // false，不推送
    // skip
}
```

**好处**：节省带宽，客户端不会收到不需要的消息

---

## 七、实战练习

### 练习 1：追踪一次推送

在代码中打日志，追踪一次推送的完整流程：

```go
// internal/comet/grpc/server.go:47
func (s *CometServer) PushMsg(...) {
    log.Printf("[PushMsg] keys=%v, op=%d", req.Keys, req.ProtoOp)

    for _, key := range req.Keys {
        log.Printf("[PushMsg] processing key=%s", key)

        bucket := s.srv.Bucket(key)
        log.Printf("[PushMsg] bucket index=%d", bucketIndex)

        if channel := bucket.Channel(key); channel != nil {
            log.Printf("[PushMsg] found channel, mid=%d", channel.Mid)

            if !channel.NeedPush(req.ProtoOp) {
                log.Printf("[PushMsg] skip, not watching op=%d", req.ProtoOp)
                continue
            }

            channel.Push(req.Proto)
            log.Printf("[PushMsg] pushed to channel")
        }
    }
}
```

### 练习 2：查看 Bucket 分布

```go
// 统计每个 Bucket 的连接数
for i, bucket := range server.Buckets() {
    count := bucket.ChannelCount()
    fmt.Printf("Bucket[%d]: %d channels\n", i, count)
}
```

---

## 八、总结

**核心概念**：
- **Server**：Comet 服务器
- **Bucket**：连接分组（减少锁竞争）
- **Channel**：单个 WebSocket 连接
- **Room**：聊天室/直播间
- **Key**：连接的唯一标识

**数据流**：
```
Job gRPC 调用
  → Comet.PushMsg(keys, op, msg)
  → 根据 Key Hash 找到 Bucket
  → 在 Bucket 中找到 Channel
  → 检查 Channel.NeedPush(op)
  → Channel.Push(proto)
  → dispatch 协程发送到 WebSocket
```

**下一步**：
- 阅读 `internal/comet/grpc/server.go:47` - PushMsg 实现
- 阅读 `internal/comet/bucket.go` - Bucket 管理
- 阅读 `internal/comet/channel.go` - Channel 结构

理解了这些，你就能看懂 Job 如何调用 Comet 推送消息了！
