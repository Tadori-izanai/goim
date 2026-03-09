# 第三步：理解配置与启动 - 详细指南

## 目标

理解 Comet、Logic、Job 三个服务如何启动、如何加载配置、依赖哪些外部服务。

---

## 核心概念

### 三个服务的职责

```
┌─────────┐
│  Comet  │ - 维持客户端长连接（WebSocket/TCP）
│         │ - 接收 Job 的推送请求（gRPC Server）
│         │ - 调用 Logic 鉴权（gRPC Client）
└─────────┘

┌─────────┐
│  Logic  │ - 提供 HTTP API（推送接口）
│         │ - 提供 gRPC API（鉴权接口）
│         │ - 发现 Comet 节点（Discovery Client）
│         │ - 发送消息到 Kafka
└─────────┘

┌─────────┐
│   Job   │ - 消费 Kafka 消息
│         │ - 发现 Comet 节点（Discovery Client）
│         │ - 调用 Comet 推送（gRPC Client）
└─────────┘
```

### 外部依赖

所有服务都依赖：
- **Discovery**（服务发现）：`127.0.0.1:7171`
- **Redis**：存储连接映射（mid → key）
- **Kafka**：消息队列

---

## 阅读顺序

### 第一部分：Comet 启动流程

#### 文件 1：`cmd/comet/main.go`

**阅读重点**：

```go
func main() {
    flag.Parse()

    // 1. 加载配置
    if err := conf.Init(); err != nil {
        panic(err)
    }

    // 2. 初始化日志
    log.Init(conf.Conf.Log)
    defer log.Close()

    // 3. 初始化 Discovery 客户端
    dis := naming.New(conf.Conf.Discovery)
    resolver.Register(dis)

    // 4. 创建 Comet Server
    srv := comet.NewServer(conf.Conf)

    // 5. 注册到 Discovery
    cancel := register(dis, srv)

    // 6. 启动在线统计协程
    go func() {
        for {
            time.Sleep(time.Second)
            if err := dis.Set(srv.Env.Zone, srv.Env.DeployEnv, srv.Env.Host, srv.Env.AppID, srv.Env.Addrs, srv.Env.Weight, srv.Env.Metadata); err != nil {
                log.Errorf("dis.Set() error(%v)", err)
            }
        }
    }()

    // 7. 监听信号，优雅退出
    ch := make(chan os.Signal, 1)
    signal.Notify(ch, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
    for {
        s := <-ch
        switch s {
        case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
            cancel()
            srv.Close()
            return
        case syscall.SIGHUP:
        default:
            return
        }
    }
}
```

**启动步骤**：
1. 解析命令行参数（`-conf` 指定配置文件路径）
2. 加载 TOML 配置文件
3. 初始化 Discovery 客户端
4. 创建 Comet Server（启动 TCP/WebSocket/gRPC）
5. 注册到 Discovery（让 Logic/Job 能发现自己）
6. 定时更新元数据（在线人数等）
7. 监听退出信号，优雅关闭

**记录到笔记**：
```
Comet 启动流程：
1. conf.Init() - 加载配置
2. naming.New() - 创建 Discovery 客户端
3. comet.NewServer() - 启动服务
4. register() - 注册到 Discovery
5. 定时更新元数据
6. 监听信号退出
```

---

#### 文件 2：`internal/comet/conf/conf.go`

**阅读重点**：

```go
type Config struct {
    Env       *Env
    Discovery *naming.Config
    TCP       *TCP
    Websocket *Websocket
    Protocol  *Protocol
    Whitelist *Whitelist
    Bucket    *Bucket
    RPCClient *RPCClient
    RPCServer *RPCServer
}

type Env struct {
    Region    string
    Zone      string
    DeployEnv string
    Host      string
    Weight    int64
    Offline   bool
    Addrs     []string
}

type TCP struct {
    Bind         []string
    Sndbuf       int
    Rcvbuf       int
    KeepAlive    bool
    Reader       int
    ReadBuf      int
    ReadBufSize  int
    Writer       int
    WriteBuf     int
    WriteBufSize int
}

type Websocket struct {
    Bind         []string
    TLSOpen      bool
    TLSBind      []string
    CertFile     string
    PrivateFile  string
    // ... 其他字段
}

type Bucket struct {
    Size          int
    Channel       int
    Room          int
    RoutineAmount uint64
    RoutineSize   int
}
```

**关键配置项**：

1. **Env**：环境信息
   - `Region/Zone`：区域标识
   - `Host`：本机 IP
   - `Addrs`：对外暴露的地址列表（用于注册到 Discovery）

2. **TCP/Websocket**：监听配置
   - `Bind`：监听地址（如 `["0.0.0.0:3101"]`）
   - `Sndbuf/Rcvbuf`：发送/接收缓冲区大小

3. **Bucket**：连接分桶
   - `Size`：桶数量（默认 32）
   - `Channel`：每个桶最多多少连接（默认 1024）
   - `Room`：每个桶最多多少房间（默认 1024）

4. **RPCServer**：gRPC 服务端配置
   - `Addr`：监听地址（如 `0.0.0.0:3109`）

5. **RPCClient**：gRPC 客户端配置（调用 Logic）
   - `Dial`：Logic 地址（通过 Discovery 发现）

**记录到笔记**：
```
Comet 配置结构：
- Env: 环境信息（Region, Zone, Host）
- TCP/Websocket: 监听地址和缓冲区
- Bucket: 连接分桶配置（32 桶 × 1024 连接）
- RPCServer: gRPC 监听地址（:3109）
- RPCClient: Logic gRPC 地址（通过 Discovery）
```

---

#### 文件 3：`configs/comet-example.toml`

**阅读重点**：

```toml
[env]
region = "sh"
zone = "sh001"
deployEnv = "dev"
host = "127.0.0.1"
weight = 10
addrs = [
    "tcp://127.0.0.1:3101",
    "ws://127.0.0.1:3102",
]

[discovery]
nodes = ["127.0.0.1:7171"]

[tcp]
bind = [
    "0.0.0.0:3101",
]
sndbuf = 4096
rcvbuf = 4096

[websocket]
bind = [
    "0.0.0.0:3102",
]

[bucket]
size = 32
channel = 1024
room = 1024

[rpcServer]
addr = "0.0.0.0:3109"

[rpcClient]
dial = "discovery://default/goim.logic"
```

**关键点**：
- `addrs`：注册到 Discovery 的地址（客户端连接用）
- `discovery.nodes`：Discovery 服务地址
- `rpcClient.dial`：通过 Discovery 发现 Logic（`discovery://default/goim.logic`）

**记录到笔记**：
```
Comet 配置示例：
- TCP 监听 :3101
- WebSocket 监听 :3102
- gRPC 监听 :3109
- 注册地址: tcp://127.0.0.1:3101, ws://127.0.0.1:3102
- Discovery: 127.0.0.1:7171
- Logic 发现: discovery://default/goim.logic
```

---

#### 文件 4：`internal/comet/server.go` - NewServer()

**阅读重点**：

```go
func NewServer(c *conf.Config) *Server {
    s := &Server{
        c:         c,
        round:     &Round{},
        buckets:   make([]*Bucket, c.Bucket.Size),
        bucketIdx: c.Bucket.Size,
    }

    // 1. 初始化 Bucket
    for i := 0; i < s.bucketIdx; i++ {
        s.buckets[i] = NewBucket(c.Bucket)
    }

    // 2. 创建 Logic gRPC 客户端
    var err error
    if s.rpcClient, err = newLogicClient(c.RPCClient); err != nil {
        panic(err)
    }

    // 3. 启动 TCP Server
    if len(c.TCP.Bind) > 0 {
        go s.InitTCP(c.TCP)
    }

    // 4. 启动 WebSocket Server
    if len(c.Websocket.Bind) > 0 {
        go s.InitWebsocket(c.Websocket)
    }

    // 5. 启动 gRPC Server
    s.rpcServer = grpc.New(c.RPCServer, s)

    return s
}
```

**初始化步骤**：
1. 创建 Bucket 数组（默认 32 个）
2. 创建 Logic gRPC 客户端（用于鉴权）
3. 启动 TCP Server（如果配置了）
4. 启动 WebSocket Server（如果配置了）
5. 启动 gRPC Server（接收 Job 的推送请求）

**记录到笔记**：
```
NewServer() 流程：
1. 初始化 Bucket 数组
2. 创建 Logic gRPC Client
3. 启动 TCP Server (协程)
4. 启动 WebSocket Server (协程)
5. 启动 gRPC Server
```

---

### 第二部分：Logic 启动流程

#### 文件 5：`cmd/logic/main.go`

**阅读重点**：

```go
func main() {
    flag.Parse()

    // 1. 加载配置
    if err := conf.Init(); err != nil {
        panic(err)
    }

    // 2. 初始化 Discovery
    dis := naming.New(conf.Conf.Discovery)
    resolver.Register(dis)

    // 3. 创建 Logic 实例
    srv := logic.New(conf.Conf)

    // 4. 启动 HTTP Server
    httpSrv := http.New(conf.Conf.HTTPServer, srv)

    // 5. 启动 gRPC Server
    rpcSrv := grpc.New(conf.Conf.RPCServer, srv)

    // 6. 注册到 Discovery
    cancel := register(dis, srv)

    // 7. 监听信号退出
    // ...
}
```

**启动步骤**：
1. 加载配置
2. 初始化 Discovery
3. 创建 Logic 实例（连接 Redis、Kafka、发现 Comet）
4. 启动 HTTP Server（`:3111`，提供推送 API）
5. 启动 gRPC Server（`:3119`，提供鉴权 API）
6. 注册到 Discovery
7. 监听退出信号

**记录到笔记**：
```
Logic 启动流程：
1. conf.Init()
2. naming.New() - Discovery 客户端
3. logic.New() - 创建 Logic（连接 Redis/Kafka/Comet）
4. http.New() - 启动 HTTP Server (:3111)
5. grpc.New() - 启动 gRPC Server (:3119)
6. register() - 注册到 Discovery
```

---

#### 文件 6：`internal/logic/conf/conf.go`

**阅读重点**：

```go
type Config struct {
    Env        *Env
    Discovery  *naming.Config
    RPCClient  *RPCClient
    RPCServer  *RPCServer
    HTTPServer *HTTPServer
    Kafka      *Kafka
    Redis      *Redis
    Node       *Node
    Backoff    *Backoff
    Regions    map[string][]int32
}

type Kafka struct {
    Topic   string
    Brokers []string
}

type Redis struct {
    Network      string
    Addr         string
    Auth         string
    Active       int
    Idle         int
    DialTimeout  xtime.Duration
    ReadTimeout  xtime.Duration
    WriteTimeout xtime.Duration
    IdleTimeout  xtime.Duration
    Expire       xtime.Duration
}

type Node struct {
    DefaultDomain string
    HostDomain    string
    TCPPort       int
    WSPort        int
    WSSPort       int
    HeartbeatMax  int
    Heartbeat     xtime.Duration
    RegionWeight  float64
}
```

**关键配置项**：

1. **Kafka**：消息队列
   - `Topic`：推送消息的 Topic（`goim-push-topic`）
   - `Brokers`：Kafka 地址列表

2. **Redis**：存储连接映射
   - `Addr`：Redis 地址
   - `Expire`：Key 过期时间

3. **Node**：Comet 节点配置
   - `TCPPort/WSPort/WSSPort`：Comet 端口
   - `Heartbeat`：心跳间隔

4. **HTTPServer**：HTTP API 配置
   - `Addr`：监听地址（`:3111`）

5. **RPCServer**：gRPC 服务端配置
   - `Addr`：监听地址（`:3119`）

**记录到笔记**：
```
Logic 配置结构：
- Kafka: Topic + Brokers
- Redis: 连接映射存储
- Node: Comet 节点配置（端口、心跳）
- HTTPServer: :3111
- RPCServer: :3119
```

---

#### 文件 7：`configs/logic-example.toml`

**阅读重点**：

```toml
[env]
region = "sh"
zone = "sh001"
deployEnv = "dev"
host = "127.0.0.1"

[discovery]
nodes = ["127.0.0.1:7171"]

[httpServer]
addr = "0.0.0.0:3111"

[rpcServer]
addr = "0.0.0.0:3119"

[kafka]
topic = "goim-push-topic"
brokers = ["127.0.0.1:9092"]

[redis]
network = "tcp"
addr = "127.0.0.1:6379"
expire = "30m"

[node]
defaultDomain = "127.0.0.1"
tcpPort = 3101
wsPort = 3102
heartbeat = "30s"
heartbeatMax = 3
```

**关键点**：
- HTTP API 监听 `:3111`（推送接口）
- gRPC 监听 `:3119`（鉴权接口）
- Kafka Topic：`goim-push-topic`
- Redis 存储连接映射，过期时间 30 分钟
- Comet 节点配置：TCP 3101、WS 3102

**记录到笔记**：
```
Logic 配置示例：
- HTTP: :3111
- gRPC: :3119
- Kafka: 127.0.0.1:9092, topic=goim-push-topic
- Redis: 127.0.0.1:6379, expire=30m
- Comet 端口: TCP 3101, WS 3102
```

---

#### 文件 8：`internal/logic/logic.go` - New()

**阅读重点**：

```go
func New(c *conf.Config) *Logic {
    l := &Logic{
        c:   c,
        dao: dao.New(c),
    }

    // 1. 初始化 Comet 节点发现
    l.loadOnline()
    go l.onlineproc()

    // 2. 监听 Comet 节点变化
    resolver.Register(naming.New(c.Discovery))
    conn, err := grpc.DialContext(context.Background(), "discovery://default/goim.comet", ...)
    if err != nil {
        panic(err)
    }
    l.cometServiceClient = comet.NewCometServiceClient(conn)

    return l
}
```

**初始化步骤**：
1. 创建 DAO（连接 Redis、Kafka）
2. 加载在线统计
3. 启动在线统计协程
4. 通过 Discovery 发现 Comet 节点
5. 创建 Comet gRPC 客户端（用于推送）

**记录到笔记**：
```
Logic.New() 流程：
1. dao.New() - 连接 Redis/Kafka
2. loadOnline() - 加载在线统计
3. onlineproc() - 定时更新在线统计 (协程)
4. Discovery 发现 Comet 节点
5. 创建 Comet gRPC Client
```

---

### 第三部分：Job 启动流程

#### 文件 9：`cmd/job/main.go`

**阅读重点**：

```go
func main() {
    flag.Parse()

    // 1. 加载配置
    if err := conf.Init(); err != nil {
        panic(err)
    }

    // 2. 初始化 Discovery
    dis := naming.New(conf.Conf.Discovery)
    resolver.Register(dis)

    // 3. 创建 Job 实例
    j := job.New(conf.Conf)

    // 4. 启动 Kafka 消费
    go j.Consume()

    // 5. 监听信号退出
    // ...
}
```

**启动步骤**：
1. 加载配置
2. 初始化 Discovery
3. 创建 Job 实例（发现 Comet、连接 Kafka）
4. 启动 Kafka 消费协程
5. 监听退出信号

**记录到笔记**：
```
Job 启动流程：
1. conf.Init()
2. naming.New() - Discovery 客户端
3. job.New() - 创建 Job（发现 Comet、连接 Kafka）
4. j.Consume() - 启动 Kafka 消费 (协程)
5. 监听信号退出
```

---

#### 文件 10：`internal/job/conf/conf.go`

**阅读重点**：

```go
type Config struct {
    Env       *Env
    Discovery *naming.Config
    Comet     *Comet
    Room      *Room
    Kafka     *Kafka
}

type Kafka struct {
    Topic   string
    Group   string
    Brokers []string
}

type Comet struct {
    RoutineSize uint64
    RoutineAmount uint64
}

type Room struct {
    Batch  int
    Signal time.Duration
    Idle   time.Duration
}
```

**关键配置项**：

1. **Kafka**：消息消费
   - `Topic`：消费的 Topic（`goim-push-topic`）
   - `Group`：消费者组（`goim-job`）
   - `Brokers`：Kafka 地址

2. **Comet**：推送协程配置
   - `RoutineSize`：每个协程的缓冲区大小
   - `RoutineAmount`：协程数量

3. **Room**：房间推送配置
   - `Batch`：批量大小
   - `Signal`：信号间隔
   - `Idle`：空闲超时

**记录到笔记**：
```
Job 配置结构：
- Kafka: Topic + Group + Brokers
- Comet: 推送协程配置
- Room: 房间批量推送配置
```

---

#### 文件 11：`configs/job-example.toml`

**阅读重点**：

```toml
[env]
region = "sh"
zone = "sh001"
deployEnv = "dev"

[discovery]
nodes = ["127.0.0.1:7171"]

[kafka]
topic = "goim-push-topic"
group = "goim-job"
brokers = ["127.0.0.1:9092"]

[comet]
routineAmount = 32
routineSize = 1024

[room]
batch = 20
signal = "1s"
idle = "60s"
```

**关键点**：
- Kafka 消费 `goim-push-topic`，消费者组 `goim-job`
- 32 个推送协程，每个缓冲 1024 条消息
- 房间推送：批量 20 条，信号间隔 1 秒，空闲 60 秒后回收

**记录到笔记**：
```
Job 配置示例：
- Kafka: topic=goim-push-topic, group=goim-job
- Comet: 32 协程 × 1024 缓冲
- Room: batch=20, signal=1s, idle=60s
```

---

#### 文件 12：`internal/job/job.go` - New()

**阅读重点**：

```go
func New(c *conf.Config) *Job {
    j := &Job{
        c:            c,
        consumer:     kafka.NewConsumer(c.Kafka),
        cometServers: make(map[string]*CometClient),
        rooms:        make(map[string]*Room),
    }

    // 1. 通过 Discovery 发现 Comet 节点
    resolver.Register(naming.New(c.Discovery))
    conn, err := grpc.DialContext(context.Background(), "discovery://default/goim.comet", ...)
    if err != nil {
        panic(err)
    }

    // 2. 监听 Comet 节点变化
    go j.watchComet(conn)

    return j
}
```

**初始化步骤**：
1. 创建 Kafka 消费者
2. 初始化 Comet 客户端映射
3. 初始化 Room 映射
4. 通过 Discovery 发现 Comet 节点
5. 启动协程监听 Comet 节点变化

**记录到笔记**：
```
Job.New() 流程：
1. kafka.NewConsumer() - 创建 Kafka 消费者
2. 初始化 cometServers 映射
3. 初始化 rooms 映射
4. Discovery 发现 Comet 节点
5. watchComet() - 监听 Comet 节点变化 (协程)
```

---

## 完整启动流程图

### 方式 1：时序图

```
时间线 →

[Comet]
  1. 加载配置
  2. 创建 Discovery 客户端
  3. 初始化 Bucket
  4. 创建 Logic gRPC Client
  5. 启动 TCP Server (:3101)
  6. 启动 WebSocket Server (:3102)
  7. 启动 gRPC Server (:3109)
  8. 注册到 Discovery
  9. 定时更新元数据

[Logic]
  1. 加载配置
  2. 创建 Discovery 客户端
  3. 连接 Redis
  4. 连接 Kafka
  5. 发现 Comet 节点
  6. 启动 HTTP Server (:3111)
  7. 启动 gRPC Server (:3119)
  8. 注册到 Discovery
  9. 定时更新在线统计

[Job]
  1. 加载配置
  2. 创建 Discovery 客户端
  3. 创建 Kafka 消费者
  4. 发现 Comet 节点
  5. 启动 Kafka 消费协程
  6. 监听 Comet 节点变化
```

---

### 方式 2：依赖关系图

```
外部依赖：
┌──────────┐  ┌──────────┐  ┌──────────┐
│Discovery │  │  Redis   │  │  Kafka   │
│:7171     │  │:6379     │  │:9092     │
└────┬─────┘  └────┬─────┘  └────┬─────┘
     │             │              │
     ├─────────────┼──────────────┤
     │             │              │
┌────▼─────┐  ┌───▼──────┐  ┌───▼──────┐
│  Comet   │  │  Logic   │  │   Job    │
│:3101 TCP │◄─┤:3111 HTTP│  │          │
│:3102 WS  │  │:3119 gRPC│  │          │
│:3109 gRPC│  └──────────┘  └──────────┘
└──────────┘       ▲              │
     ▲             │              │
     └─────────────┴──────────────┘
       gRPC 调用
```

---

### 方式 3：配置对照表

| 服务 | 监听端口 | 依赖服务 | 注册到 Discovery | 发现其他服务 |
|---|---|---|---|---|
| Comet | 3101 (TCP)<br>3102 (WS)<br>3109 (gRPC) | Discovery | ✅ | Logic (鉴权) |
| Logic | 3111 (HTTP)<br>3119 (gRPC) | Discovery<br>Redis<br>Kafka | ✅ | Comet (推送) |
| Job | 无 | Discovery<br>Kafka | ❌ | Comet (推送) |

---

## 关键配置项速查

### Comet

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `tcp.bind` | `0.0.0.0:3101` | TCP 监听地址 |
| `websocket.bind` | `0.0.0.0:3102` | WebSocket 监听地址 |
| `rpcServer.addr` | `0.0.0.0:3109` | gRPC 监听地址 |
| `bucket.size` | `32` | Bucket 数量 |
| `bucket.channel` | `1024` | 每个 Bucket 最大连接数 |
| `rpcClient.dial` | `discovery://default/goim.logic` | Logic 发现地址 |

### Logic

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `httpServer.addr` | `0.0.0.0:3111` | HTTP API 监听地址 |
| `rpcServer.addr` | `0.0.0.0:3119` | gRPC 监听地址 |
| `kafka.topic` | `goim-push-topic` | Kafka Topic |
| `kafka.brokers` | `["127.0.0.1:9092"]` | Kafka 地址 |
| `redis.addr` | `127.0.0.1:6379` | Redis 地址 |
| `redis.expire` | `30m` | Key 过期时间 |

### Job

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `kafka.topic` | `goim-push-topic` | Kafka Topic |
| `kafka.group` | `goim-job` | 消费者组 |
| `kafka.brokers` | `["127.0.0.1:9092"]` | Kafka 地址 |
| `comet.routineAmount` | `32` | 推送协程数 |
| `room.batch` | `20` | 房间批量大小 |
| `room.idle` | `60s` | 房间空闲超时 |

---

## 实践步骤

### 1. 阅读启动代码（30 分钟）

按顺序阅读：
1. `cmd/comet/main.go` → `internal/comet/server.go`
2. `cmd/logic/main.go` → `internal/logic/logic.go`
3. `cmd/job/main.go` → `internal/job/job.go`

记录每个服务的启动步骤。

### 2. 阅读配置文件（30 分钟）

对照配置文件和配置结构：
1. `configs/comet-example.toml` ↔ `internal/comet/conf/conf.go`
2. `configs/logic-example.toml` ↔ `internal/logic/conf/conf.go`
3. `configs/job-example.toml` ↔ `internal/job/conf/conf.go`

标注哪些配置需要修改（Discovery、Kafka）。

### 3. 画依赖图（20 分钟）

画出：
- 三个服务的依赖关系
- 外部服务依赖（Discovery、Redis、Kafka）
- 服务间的 gRPC 调用关系

### 4. 验证启动（10 分钟）

尝试启动服务，观察日志：
```bash
# 启动 Comet
./comet -conf comet-example.toml

# 观察日志
# [INFO] comet: tcp listen on 0.0.0.0:3101
# [INFO] comet: websocket listen on 0.0.0.0:3102
# [INFO] comet: grpc listen on 0.0.0.0:3109
# [INFO] comet: register to discovery
```

---

## 输出

完成后你应该有：
1. 三个服务的启动流程笔记
2. 配置项对照表
3. 服务依赖关系图
4. 对启动流程的清晰理解

---

## 下一步

理解了配置与启动后，你可以：
1. **修改配置**：移除 Discovery，改为硬编码 IP
2. **替换 Kafka**：用 interface 抽象，替换为 NATS
3. **简化部署**：减少外部依赖

准备好进入第 4 步了吗？
