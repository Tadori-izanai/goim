# Comet 数据结构完整详解

## 概览

Comet 的核心数据结构层次关系：

```
Server (Comet 服务器)
├── Round (资源池管理器)
│   ├── readers []bytes.Pool (读缓冲池数组)
│   ├── writers []bytes.Pool (写缓冲池数组)
│   └── timers []time.Timer (定时器数组)
│
├── buckets []*Bucket (连接分桶数组)
│   └── Bucket (单个桶)
│       ├── chs map[string]*Channel (连接映射)
│       │   └── Channel (单个连接)
│       │       ├── CliProto Ring (客户端消息环形缓冲)
│       │       ├── signal chan (服务端推送队列)
│       │       ├── Reader bufio.Reader (读缓冲)
│       │       ├── Writer bufio.Writer (写缓冲)
│       │       └── Room *Room (所属房间)
│       │
│       └── rooms map[string]*Room (房间映射)
│           └── Room (单个房间)
│               └── next *Channel (房间内连接链表)
│
└── rpcClient logic.LogicClient (Logic gRPC 客户端)
```

---

## 一、Server（服务器）

**定义**：`internal/comet/server.go:55`

```go
type Server struct {
    c         *conf.Config        // 配置
    round     *Round              // 资源池管理器
    buckets   []*Bucket           // 连接分桶数组
    bucketIdx uint32              // 桶数量

    serverID  string              // 服务器 ID
    rpcClient logic.LogicClient   // Logic gRPC 客户端
}
```

### 成员详解

| 字段 | 类型 | 作用 |
|---|---|---|
| `c` | `*conf.Config` | 配置对象，包含所有启动参数 |
| `round` | `*Round` | 资源池管理器，提供缓冲池和定时器 |
| `buckets` | `[]*Bucket` | 连接分桶数组，默认 32 个 |
| `bucketIdx` | `uint32` | 桶数量，用于 Hash 取模 |
| `serverID` | `string` | 服务器唯一标识，如 "comet-01" |
| `rpcClient` | `logic.LogicClient` | Logic gRPC 客户端，用于鉴权 |

### 核心职责

1. **管理所有 Bucket**
2. **提供资源池（Round）**
3. **与 Logic 通信（鉴权）**
4. **路由连接到对应 Bucket**

### 关键方法

#### 1. Bucket() - 根据 Key 找到对应的 Bucket

```go
func (s *Server) Bucket(subKey string) *Bucket {
    idx := cityhash.CityHash32([]byte(subKey), uint32(len(subKey))) % s.bucketIdx
    return s.buckets[idx]
}
```

**作用**：通过 CityHash 将 Key 映射到某个 Bucket

**示例**：
```
Key = "server1_conn123"
→ Hash = 12345678
→ idx = 12345678 % 32 = 14
→ 返回 buckets[14]
```

#### 2. ServeWebsocket() - 处理 WebSocket 连接

```go
func (s *Server) ServeWebsocket(conn net.Conn, rp, wp *bytes.Pool, tr *xtime.Timer)
```

**作用**：处理单个 WebSocket 连接的完整生命周期

**流程**：
1. 读取 HTTP 请求
2. 升级为 WebSocket
3. 鉴权（调用 Logic gRPC）
4. 创建 Channel
5. 加入 Bucket
6. 启动读写协程

---

## 二、Round（资源池管理器）

**定义**：`internal/comet/round.go:22`

```go
type Round struct {
    readers []bytes.Pool    // 读缓冲池数组
    writers []bytes.Pool    // 写缓冲池数组
    timers  []time.Timer    // 定时器数组
    options RoundOptions    // 配置选项
}
```

### 成员详解

| 字段 | 类型 | 作用 |
|---|---|---|
| `readers` | `[]bytes.Pool` | 读缓冲池数组，默认 32 个 |
| `writers` | `[]bytes.Pool` | 写缓冲池数组，默认 32 个 |
| `timers` | `[]time.Timer` | 定时器数组，默认 32 个 |
| `options` | `RoundOptions` | 配置选项（池大小、缓冲区大小等） |

### 核心职责

1. **管理内存缓冲池**（避免频繁分配）
2. **管理定时器池**（心跳、超时检测）
3. **轮询分配资源**（减少锁竞争）

### 关键方法

#### 1. Reader() - 获取读缓冲池

```go
func (r *Round) Reader(rn int) *bytes.Pool {
    return &(r.readers[rn % r.options.Reader])
}
```

**作用**：轮询选择一个读缓冲池

**示例**：
```
rn = 5 → readers[5 % 32] = readers[5]
rn = 35 → readers[35 % 32] = readers[3]
```

#### 2. Writer() - 获取写缓冲池

```go
func (r *Round) Writer(rn int) *bytes.Pool {
    return &(r.writers[rn % r.options.Writer])
}
```

#### 3. Timer() - 获取定时器

```go
func (r *Round) Timer(rn int) *time.Timer {
    return &(r.timers[rn % r.options.Timer])
}
```

### 为什么需要多个池？

**避免锁竞争**：

```
单个池：
所有连接竞争同一把锁 → 性能瓶颈

32 个池：
连接分散到不同池 → 锁竞争减少 32 倍
```

---

## 三、Bucket（连接桶）

**定义**：`internal/comet/bucket.go:13`

```go
type Bucket struct {
    c     *conf.Bucket              // 配置
    cLock sync.RWMutex              // 保护 chs 和 rooms 的读写锁
    chs   map[string]*Channel       // Key → Channel 映射

    rooms       map[string]*Room    // RoomID → Room 映射
    routines    []chan *pb.BroadcastRoomReq  // 房间广播协程队列
    routinesNum uint64               // 轮询计数器

    ipCnts map[string]int32         // IP → 连接数映射（限流用）
}
```

### 成员详解

| 字段 | 类型 | 作用 |
|---|---|---|
| `c` | `*conf.Bucket` | 配置（容量、协程数等） |
| `cLock` | `sync.RWMutex` | 读写锁，保护 chs 和 rooms |
| `chs` | `map[string]*Channel` | 连接映射，Key 是连接标识 |
| `rooms` | `map[string]*Room` | 房间映射，Key 是房间 ID |
| `routines` | `[]chan *BroadcastRoomReq` | 房间广播协程队列，默认 32 个 |
| `routinesNum` | `uint64` | 轮询计数器，用于选择协程 |
| `ipCnts` | `map[string]int32` | IP 连接数统计，用于限流 |

### 核心职责

1. **管理连接**（增删查）
2. **管理房间**（增删查）
3. **房间广播**（多协程并发）
4. **IP 限流**

### 关键方法

#### 1. Put() - 添加连接

```go
func (b *Bucket) Put(rid string, ch *Channel) error {
    b.cLock.Lock()

    // 关闭旧连接（同一个 Key）
    if dch := b.chs[ch.Key]; dch != nil {
        dch.Close()
    }

    // 添加新连接
    b.chs[ch.Key] = ch

    // 加入房间
    if rid != "" {
        if room, ok = b.rooms[rid]; !ok {
            room = NewRoom(rid)
            b.rooms[rid] = room
        }
        ch.Room = room
    }

    // IP 计数
    b.ipCnts[ch.IP]++

    b.cLock.Unlock()

    if room != nil {
        room.Put(ch)
    }
    return nil
}
```

**作用**：将 Channel 加入 Bucket 和 Room

#### 2. Del() - 删除连接

```go
func (b *Bucket) Del(dch *Channel) {
    room := dch.Room
    b.cLock.Lock()

    if ch, ok := b.chs[dch.Key]; ok {
        if ch == dch {
            delete(b.chs, ch.Key)
        }

        // IP 计数减少
        if b.ipCnts[ch.IP] > 1 {
            b.ipCnts[ch.IP]--
        } else {
            delete(b.ipCnts, ch.IP)
        }
    }

    b.cLock.Unlock()

    if room != nil && room.Del(dch) {
        b.DelRoom(room)  // 房间为空则删除
    }
}
```

#### 3. Channel() - 查找连接

```go
func (b *Bucket) Channel(key string) *Channel {
    b.cLock.RLock()
    ch := b.chs[key]
    b.cLock.RUnlock()
    return ch
}
```

#### 4. BroadcastRoom() - 房间广播

```go
func (b *Bucket) BroadcastRoom(arg *pb.BroadcastRoomReq) {
    num := atomic.AddUint64(&b.routinesNum, 1) % uint64(len(b.routines))
    b.routines[num] <- arg  // 轮询选择协程
}
```

**作用**：将广播请求发送到某个协程队列

---

## 四、Channel（连接）

**定义**：`internal/comet/channel.go:12`

```go
type Channel struct {
    Room     *Room                  // 所属房间
    CliProto Ring                   // 客户端消息环形缓冲
    signal   chan *protocol.Proto   // 服务端推送队列
    Writer   bufio.Writer           // 写缓冲
    Reader   bufio.Reader           // 读缓冲
    Next     *Channel               // 链表：下一个 Channel
    Prev     *Channel               // 链表：上一个 Channel

    Mid      int64                  // 用户 ID
    Key      string                 // 连接 Key
    IP       string                 // 客户端 IP
    watchOps map[int32]struct{}     // 订阅的操作码
    mutex    sync.RWMutex           // 保护 watchOps
}
```

### 成员详解

| 字段 | 类型 | 作用 |
|---|---|---|
| `Room` | `*Room` | 所属房间，nil 表示未加入房间 |
| `CliProto` | `Ring` | 客户端消息环形缓冲，存储待发送的消息 |
| `signal` | `chan *protocol.Proto` | 服务端推送队列，用于 dispatch 协程 |
| `Writer` | `bufio.Writer` | 写缓冲，用于发送数据到 WebSocket |
| `Reader` | `bufio.Reader` | 读缓冲，用于从 WebSocket 读取数据 |
| `Next` | `*Channel` | 链表指针，指向房间内下一个 Channel |
| `Prev` | `*Channel` | 链表指针，指向房间内上一个 Channel |
| `Mid` | `int64` | 用户 ID，鉴权时获得 |
| `Key` | `string` | 连接唯一标识，格式 `{ServerID}_{ConnID}` |
| `IP` | `string` | 客户端 IP 地址 |
| `watchOps` | `map[int32]struct{}` | 订阅的操作码集合，用于过滤消息 |
| `mutex` | `sync.RWMutex` | 保护 watchOps 的并发读写 |

### 核心职责

1. **存储连接信息**（Mid、Key、IP）
2. **管理订阅**（watchOps）
3. **缓冲消息**（CliProto、signal）
4. **读写数据**（Reader、Writer）
5. **房间链表**（Next、Prev）

### 关键方法

#### 1. Watch() - 订阅操作码

```go
func (c *Channel) Watch(accepts ...int32) {
    c.mutex.Lock()
    for _, op := range accepts {
        c.watchOps[op] = struct{}{}
    }
    c.mutex.Unlock()
}
```

**作用**：添加订阅的操作码

**示例**：
```go
ch.Watch(1000, 1001, 1002)
// watchOps = {1000: {}, 1001: {}, 1002: {}}
```

#### 2. NeedPush() - 检查是否需要推送

```go
func (c *Channel) NeedPush(op int32) bool {
    c.mutex.RLock()
    _, ok := c.watchOps[op]
    c.mutex.RUnlock()
    return ok
}
```

**作用**：检查是否订阅了某个操作码

**示例**：
```go
ch.NeedPush(1000)  // true（已订阅）
ch.NeedPush(2000)  // false（未订阅）
```

#### 3. Push() - 推送消息

```go
func (c *Channel) Push(p *protocol.Proto) error {
    select {
    case c.signal <- p:
        return nil
    default:
        return errors.ErrSignalFullMsgDropped  // 队列满，丢弃
    }
}
```

**作用**：将消息推送到 signal 队列，供 dispatch 协程发送

#### 4. Ready() - 等待消息

```go
func (c *Channel) Ready() *protocol.Proto {
    return <-c.signal  // 阻塞等待
}
```

**作用**：dispatch 协程调用，阻塞等待消息

---

## 五、Room（房间）

**定义**：`internal/comet/room.go:11`

```go
type Room struct {
    ID        string          // 房间 ID
    rLock     sync.RWMutex    // 读写锁
    next      *Channel        // 链表头（房间内第一个 Channel）
    drop      bool            // 是否已删除
    Online    int32           // 在线人数（当前 Bucket）
    AllOnline int32           // 全局在线人数（所有 Bucket）
}
```

### 成员详解

| 字段 | 类型 | 作用 |
|---|---|---|
| `ID` | `string` | 房间唯一标识，如 `"live://1000"` |
| `rLock` | `sync.RWMutex` | 读写锁，保护链表操作 |
| `next` | `*Channel` | 链表头，指向房间内第一个 Channel |
| `drop` | `bool` | 标记房间是否已删除（Online=0 时设为 true） |
| `Online` | `int32` | 当前 Bucket 内的在线人数 |
| `AllOnline` | `int32` | 全局在线人数（跨所有 Bucket） |

### 核心职责

1. **管理房间内的连接**（链表）
2. **统计在线人数**
3. **房间广播**

### 数据结构：双向链表

```
Room "live://1000"
  ↓ next
Channel A ↔ Channel B ↔ Channel C
(Prev=nil)  (Prev=A)    (Prev=B)
(Next=B)    (Next=C)    (Next=nil)
```

### 关键方法

#### 1. Put() - 添加 Channel

```go
func (r *Room) Put(ch *Channel) error {
    r.rLock.Lock()

    if !r.drop {
        // 插入链表头
        if r.next != nil {
            r.next.Prev = ch
        }
        ch.Next = r.next
        ch.Prev = nil
        r.next = ch

        r.Online++
    } else {
        err = errors.ErrRoomDroped
    }

    r.rLock.Unlock()
    return err
}
```

**作用**：将 Channel 插入链表头

**示例**：
```
初始：Room.next → B → C
插入 A：Room.next → A → B → C
```

#### 2. Del() - 删除 Channel

```go
func (r *Room) Del(ch *Channel) bool {
    r.rLock.Lock()

    // 从链表中移除
    if ch.Next != nil {
        ch.Next.Prev = ch.Prev
    }
    if ch.Prev != nil {
        ch.Prev.Next = ch.Next
    } else {
        r.next = ch.Next  // 删除的是头节点
    }

    ch.Next = nil
    ch.Prev = nil
    r.Online--
    r.drop = (r.Online == 0)  // 无人时标记删除

    r.rLock.Unlock()
    return r.drop
}
```

**返回值**：true 表示房间已空，需要删除

#### 3. Push() - 房间广播

```go
func (r *Room) Push(p *protocol.Proto) {
    r.rLock.RLock()

    // 遍历链表，推送给所有 Channel
    for ch := r.next; ch != nil; ch = ch.Next {
        ch.Push(p)
    }

    r.rLock.RUnlock()
}
```

**作用**：向房间内所有 Channel 推送消息

---

## 六、Ring（环形缓冲）

**定义**：`internal/comet/ring.go:11`

```go
type Ring struct {
    rp   uint64            // 读指针
    num  uint64            // 容量（2^N）
    mask uint64            // 掩码（num - 1）

    wp   uint64            // 写指针
    data []protocol.Proto  // 数据数组
}
```

### 成员详解

| 字段 | 类型 | 作用 |
|---|---|---|
| `rp` | `uint64` | 读指针，指向下一个要读取的位置 |
| `num` | `uint64` | 容量，必须是 2 的幂次（如 64、128） |
| `mask` | `uint64` | 掩码，等于 `num - 1`，用于快速取模 |
| `wp` | `uint64` | 写指针，指向下一个要写入的位置 |
| `data` | `[]protocol.Proto` | 数据数组，存储消息 |

### 核心职责

1. **无锁环形缓冲**（单生产者单消费者）
2. **存储客户端消息**（待发送）
3. **高效取模**（位运算）

### 数据结构：环形数组

```
容量 num = 8，mask = 7

data: [0] [1] [2] [3] [4] [5] [6] [7]
       ↑               ↑
       rp=0            wp=4

已使用：wp - rp = 4
剩余空间：num - (wp - rp) = 4
```

### 关键方法

#### 1. Init() - 初始化

```go
func (r *Ring) init(num uint64) {
    // 确保 num 是 2 的幂次
    if num & (num - 1) != 0 {
        for num & (num - 1) != 0 {
            num &= num - 1
        }
        num <<= 1
    }

    r.data = make([]protocol.Proto, num)
    r.num = num
    r.mask = r.num - 1
}
```

**作用**：将 num 向上取整到 2 的幂次

**示例**：
```
输入 60 → 64 (2^6)
输入 100 → 128 (2^7)
```

#### 2. Set() - 获取写位置

```go
func (r *Ring) Set() (*protocol.Proto, error) {
    if r.wp - r.rp >= r.num {
        return nil, errors.ErrRingFull  // 满了
    }
    proto = &r.data[r.wp & r.mask]  // 位运算取模
    return proto, nil
}
```

**作用**：获取下一个可写位置的指针

**位运算取模**：
```
wp = 10, mask = 7
10 & 7 = 0b1010 & 0b0111 = 0b0010 = 2
等价于 10 % 8 = 2（但位运算更快）
```

#### 3. SetAdv() - 移动写指针

```go
func (r *Ring) SetAdv() {
    r.wp++
}
```

**使用示例**：
```go
proto, _ := ring.Set()      // 获取写位置
proto.Op = 1000             // 写入数据
proto.Body = []byte("msg")
ring.SetAdv()               // 移动写指针
```

#### 4. Get() - 获取读位置

```go
func (r *Ring) Get() (*protocol.Proto, error) {
    if r.rp == r.wp {
        return nil, errors.ErrRingEmpty  // 空了
    }
    proto = &r.data[r.rp & r.mask]
    return proto, nil
}
```

#### 5. GetAdv() - 移动读指针

```go
func (r *Ring) GetAdv() {
    r.rp++
}
```

**使用示例**：
```go
proto, _ := ring.Get()   // 获取读位置
// 处理 proto
ring.GetAdv()            // 移动读指针
```

### 为什么用环形缓冲？

**优势**：
1. **无锁**：单生产者单消费者场景下无需加锁
2. **高效**：位运算取模，比 % 运算快
3. **固定内存**：预分配，不需要动态扩容

**适用场景**：
- Channel.CliProto：存储客户端发送的消息
- 读协程写入，dispatch 协程读取

---

## 七、数据流示例

### 场景：用户 A 发送消息给用户 B

```
1. 用户 A 的 WebSocket 发送消息
   ↓
2. Comet 读协程读取
   ↓
3. 写入 Channel.CliProto (Ring)
   ↓
4. 通知 dispatch 协程
   ↓
5. dispatch 协程从 Ring 读取
   ↓
6. 调用 Logic gRPC 处理
   ↓
7. Logic 发布到 Kafka
   ↓
8. Job 消费 Kafka
   ↓
9. Job 调用 Comet gRPC: PushMsg(keys=["server1_conn456"])
   ↓
10. Comet.Bucket(key) 找到 Bucket
   ↓
11. Bucket.Channel(key) 找到 Channel
   ↓
12. Channel.NeedPush(op) 检查订阅
   ↓
13. Channel.Push(proto) 写入 signal 队列
   ↓
14. dispatch 协程从 signal 读取
   ↓
15. 写入 Channel.Writer (bufio)
   ↓
16. 发送到 WebSocket
   ↓
17. 用户 B 收到消息
```

---

## 八、内存占用估算

假设配置：
- Bucket 数量：32
- 每个 Bucket 容量：10 万连接
- Ring 大小：64
- signal 队列：128

**单个 Channel**：
- Ring：64 × 48 字节 ≈ 3KB
- signal：128 × 8 字节 ≈ 1KB
- 其他字段：≈ 1KB
- **总计**：≈ 5KB

**100 万连接**：
- Channel：100 万 × 5KB = 5GB
- Bucket：32 × 1MB ≈ 32MB
- Round 缓冲池：512MB
- **总计**：≈ 5.5GB

---

## 九、关键设计模式

### 1. 分桶（Sharding）

**目的**：减少锁竞争

```
32 个 Bucket → 锁竞争减少 32 倍
```

### 2. 对象池（Object Pool）

**目的**：减少内存分配和 GC

```
Round.readers/writers → 复用 Buffer
```

### 3. 环形缓冲（Ring Buffer）

**目的**：无锁高效

```
Channel.CliProto → 单生产者单消费者
```

### 4. 双向链表（Doubly Linked List）

**目的**：快速插入删除

```
Room.next → 房间内连接链表
```

### 5. 订阅过滤（Subscription Filter）

**目的**：减少无效推送

```
Channel.watchOps → 只推送订阅的消息
```

---

## 十、总结

| 结构 | 作用 | 关键字段 |
|---|---|---|
| **Server** | 服务器主体 | buckets, round, rpcClient |
| **Round** | 资源池管理 | readers, writers, timers |
| **Bucket** | 连接分桶 | chs, rooms, routines |
| **Channel** | 单个连接 | CliProto, signal, watchOps |
| **Room** | 房间 | next, Online |
| **Ring** | 环形缓冲 | rp, wp, data |

**核心思想**：
- **分而治之**：Bucket 分桶、Round 多池
- **复用资源**：对象池、环形缓冲
- **无锁设计**：Ring、原子操作
- **订阅过滤**：watchOps

这些设计让 Comet 能够高效处理百万级并发连接。
