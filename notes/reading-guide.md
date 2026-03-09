# GoIM 源码阅读指南

## 前置知识：gRPC 快速入门

### gRPC 是什么？

gRPC 是一个高性能的 RPC（远程过程调用）框架，让你可以像调用本地函数一样调用远程服务。

**在 goim 中的作用**：
- Comet ↔ Logic：客户端连接时，Comet 调用 Logic 的 `Connect()` 鉴权
- Logic ↔ Comet：推送消息时，Logic 调用 Comet 的 `PushMsg()` 下发
- Job ↔ Comet：Job 从 Kafka 消费后，调用 Comet 的 `PushMsg()` 推送

### gRPC 三要素

1. **Proto 定义**（接口契约）
   ```protobuf
   service LogicService {
       rpc Connect(ConnectReq) returns (ConnectReply);
   }
   ```

2. **Server 端**（提供服务）
   ```go
   type LogicServer struct {}
   func (s *LogicServer) Connect(ctx, req) (*ConnectReply, error) {
       // 实现逻辑
   }
   ```

3. **Client 端**（调用服务）
   ```go
   client := logic.NewLogicClient(conn)
   reply, err := client.Connect(ctx, &ConnectReq{...})
   ```

---

## 源码阅读路线（5 步）

### 第 1 步：理解数据流（1-2 天）

**目标**：搞清楚一条消息从发送到接收的完整路径

**阅读顺序**：
1. `examples/javascript/index.html` - 看 curl 命令，理解外部如何推送
2. `internal/logic/http/push.go` - Logic HTTP 接口如何接收推送请求
3. `internal/logic/push.go` - Logic 如何将消息发到 Kafka
4. `internal/job/push.go` - Job 如何从 Kafka 消费
5. `internal/job/comet.go` - Job 如何通过 gRPC 调用 Comet
6. `internal/comet/grpc/server.go` - Comet gRPC 接口如何接收
7. `internal/comet/server.go` - Comet 如何找到对应的 WebSocket 连接
8. `internal/comet/channel.go` - 消息如何推送到客户端

**关键文件**：
- `internal/logic/http/push.go:32` - pushMids 入口
- `internal/logic/push.go:15` - PushMids 实现
- `internal/logic/dao/kafka.go` - 发送到 Kafka
- `internal/job/job.go` - Job 启动与消费
- `internal/comet/grpc/server.go:47` - PushMsg 接口

**输出**：画一张数据流图，标注每个函数的作用

---

### 第 2 步：理解连接管理（2-3 天）

**目标**：搞清楚客户端连接如何建立、维护、断开

**阅读顺序**：
1. `internal/comet/server_websocket.go:151` - ServeWebsocket 主流程
2. `internal/comet/server_websocket.go:408` - authWebsocket 鉴权
3. `internal/logic/grpc/server.go` - Logic gRPC 服务端
4. `internal/logic/conn.go:15` - Connect 鉴权逻辑
5. `internal/logic/dao/redis.go` - 连接信息存储到 Redis
6. `internal/comet/bucket.go` - Bucket 结构与连接管理
7. `internal/comet/channel.go` - Channel 结构与消息队列
8. `internal/comet/room.go` - Room 房间管理

**关键概念**：
- **Key**：连接唯一标识，格式 `{ServerID}_{ConnID}`
- **Bucket**：用 Hash 分桶管理连接，减少锁竞争
- **Channel**：每个连接对应一个 Channel，包含消息队列
- **Room**：房间，多个 Channel 可以加入同一个 Room

**输出**：
- 画出 Comet 的连接管理结构图（Server → Bucket → Channel → Room）
- 理解 Redis 中存储的数据结构（key → server 映射）

---

### 第 3 步：理解配置与启动（1 天）

**目标**：搞清楚三个服务如何启动、如何读取配置

**阅读顺序**：
1. `cmd/comet/main.go` - Comet 启动入口
2. `internal/comet/conf/conf.go` - Comet 配置结构
3. `configs/comet-example.toml` - Comet 配置示例
4. `cmd/logic/main.go` - Logic 启动入口
5. `internal/logic/conf/conf.go` - Logic 配置结构
6. `configs/logic-example.toml` - Logic 配置示例
7. `cmd/job/main.go` - Job 启动入口
8. `internal/job/conf/conf.go` - Job 配置结构
9. `configs/job-example.toml` - Job 配置示例

**关键点**：
- 配置文件使用 TOML 格式
- 服务发现使用 bilibili/discovery（你要移除的部分）
- Kafka 配置在 Logic 和 Job 中（你要替换的部分）

**输出**：
- 列出三个服务的启动依赖（Redis、Kafka、Discovery）
- 标注哪些配置需要修改（服务发现 → 硬编码 IP）

---

### 第 4 步：理解 gRPC 调用链（1-2 天）

**目标**：搞清楚服务间如何通过 gRPC 通信

**阅读顺序**：
1. `api/comet/comet.proto` - Comet gRPC 接口定义
2. `api/logic/logic.proto` - Logic gRPC 接口定义
3. `internal/comet/grpc/server.go` - Comet gRPC Server
4. `internal/logic/grpc/server.go` - Logic gRPC Server
5. `internal/comet/server.go:50` - Comet 如何创建 Logic Client
6. `internal/job/comet.go` - Job 如何创建 Comet Client
7. `internal/logic/logic.go` - Logic 如何通过 Discovery 找到 Comet

**关键调用链**：
```
客户端 WebSocket 连接
  → Comet.authWebsocket()
  → Logic.Connect() [gRPC]
  → Redis 存储映射

外部推送消息
  → Logic HTTP /push/mids
  → Kafka 发布消息
  → Job 消费消息
  → Comet.PushMsg() [gRPC]
  → WebSocket 推送
```

**输出**：
- 列出所有 gRPC 接口及其作用
- 画出服务间的调用关系图

---

### 第 5 步：理解 Kafka 与 Discovery（1 天）

**目标**：搞清楚 Kafka 和服务发现的使用方式（为后续替换做准备）

**阅读顺序**：
1. `internal/logic/dao/kafka.go` - Logic 如何发送到 Kafka
2. `internal/job/job.go` - Job 如何消费 Kafka
3. `internal/logic/logic.go:40` - Logic 如何使用 Discovery
4. `internal/logic/nodes.go` - Logic 如何管理 Comet 节点列表
5. `internal/logic/balancer.go` - Logic 如何负载均衡选择 Comet

**关键点**：
- Kafka Topic：`goim-push-topic`
- Kafka 消息格式：`protocol.Proto`
- Discovery 作用：动态发现 Comet 节点
- 负载均衡：根据 Key 的 Hash 选择 Comet

**输出**：
- 理解 Kafka 的消息格式
- 理解 Discovery 的作用（为移除做准备）
- 设计 NATS 替换方案

---

## 学习建议

### 1. 边读边做笔记

在 `notes/` 目录下创建：
- `comet.md` - Comet 层笔记
- `logic.md` - Logic 层笔记
- `job.md` - Job 层笔记
- `grpc.md` - gRPC 调用链笔记

### 2. 边读边画图

推荐工具：
- draw.io（在线画图）
- Excalidraw（手绘风格）
- 纸笔（最快）

### 3. 边读边调试

在关键函数打日志：
```go
log.Printf("[DEBUG] pushMids: op=%d, mids=%v, msg=%s", op, mids, string(msg))
```

### 4. 边读边提问

遇到不懂的地方，随时问我：
- "这个函数的作用是什么？"
- "为什么要用 Bucket 分桶？"
- "gRPC 的 Context 是干什么的？"

---

## 快速参考

### 核心数据结构

```go
// Comet
type Server struct {
    buckets []*Bucket  // 连接分桶
    rpcClient LogicClient  // Logic gRPC 客户端
}

type Bucket struct {
    chs map[string]*Channel  // key → Channel
    rooms map[string]*Room   // roomID → Room
}

type Channel struct {
    Mid int64           // 用户 ID
    Key string          // 连接 Key
    Room *Room          // 所属房间
    signal chan *Proto  // 消息队列
}

// Logic
type Logic struct {
    dao *dao.Dao        // Redis + Kafka
    nodes []*Instance   // Comet 节点列表
}

// Job
type Job struct {
    cometServers map[string]*CometClient  // Comet gRPC 客户端
    consumer *kafka.Consumer              // Kafka 消费者
}
```

### 关键文件速查

| 功能 | 文件路径 |
|---|---|
| WebSocket 握手 | `internal/comet/server_websocket.go:151` |
| 鉴权逻辑 | `internal/logic/conn.go:15` |
| 推送入口 | `internal/logic/http/push.go:32` |
| Kafka 发送 | `internal/logic/dao/kafka.go` |
| Kafka 消费 | `internal/job/job.go` |
| Comet gRPC | `internal/comet/grpc/server.go:47` |
| Bucket 管理 | `internal/comet/bucket.go` |
| Channel 管理 | `internal/comet/channel.go` |

---

## 下一步

完成这 5 步后，你就可以开始：
1. **阶段二**：移除 Discovery，改为硬编码 IP
2. **阶段三**：用 interface 抽象 Kafka，替换为 NATS

准备好了吗？从第 1 步开始，我会陪你一起读源码！
