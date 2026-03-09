# gRPC 详解与 goim 实战

## 一、什么是 gRPC？

gRPC 是一个**远程过程调用**框架，让你可以像调用本地函数一样调用远程服务。

**类比**：
- 普通函数调用：`result = add(1, 2)` - 在同一个进程内
- gRPC 调用：`result = remoteService.Add(1, 2)` - 跨进程、跨机器

**优势**：
- 高性能（基于 HTTP/2）
- 跨语言（Go、Java、Python 等）
- 强类型（通过 Protobuf 定义接口）

---

## 二、gRPC 三要素

### 1. Proto 定义（接口契约）

```protobuf
// hello.proto
service Greeter {
    rpc SayHello (HelloRequest) returns (HelloReply);
}

message HelloRequest {
    string name = 1;
}

message HelloReply {
    string message = 1;
}
```

**作用**：定义服务接口和消息格式，类似 Java 的 interface

**编译**：
```bash
protoc --go_out=. --go-grpc_out=. hello.proto
```

生成两个文件：
- `hello.pb.go` - 消息结构体
- `hello_grpc.pb.go` - 服务接口和客户端

---

### 2. Server 端（提供服务）

```go
// 实现接口
type GreeterServer struct {
    pb.UnimplementedGreeterServer
}

func (s *GreeterServer) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
    return &pb.HelloReply{
        Message: "Hello, " + req.Name,
    }, nil
}

// 启动服务
func main() {
    lis, _ := net.Listen("tcp", ":50051")
    grpcServer := grpc.NewServer()
    pb.RegisterGreeterServer(grpcServer, &GreeterServer{})
    grpcServer.Serve(lis)
}
```

**关键步骤**：
1. 实现 proto 定义的接口
2. 创建 gRPC 服务器
3. 注册服务
4. 启动监听

---

### 3. Client 端（调用服务）

```go
func main() {
    // 连接服务器
    conn, _ := grpc.Dial("localhost:50051", grpc.WithInsecure())
    defer conn.Close()

    // 创建客户端
    client := pb.NewGreeterClient(conn)

    // 调用方法（像调用本地函数一样）
    resp, _ := client.SayHello(context.Background(), &pb.HelloRequest{
        Name: "张三",
    })

    fmt.Println(resp.Message) // 输出: Hello, 张三
}
```

**关键步骤**：
1. 连接到服务器
2. 创建客户端
3. 调用方法（传入 context 和请求）
4. 获取响应

---

## 三、运行 Demo

### 1. 生成代码

```bash
cd examples/grpc-demo

# 安装 protoc 编译器和插件
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 编译 proto
protoc --go_out=. --go-grpc_out=. hello.proto
```

会生成：
- `hello/hello.pb.go`
- `hello/hello_grpc.pb.go`

### 2. 启动服务端

```bash
cd server
go run main.go
```

输出：
```
gRPC 服务器启动在 :50051
```

### 3. 运行客户端

```bash
cd client
go run main.go
```

输出：
```
收到响应: Hello, 张三!
鉴权结果: success=true, session_key=session_abc123
```

---

## 四、goim 中的 gRPC 使用

### 场景 1：Comet 调用 Logic 鉴权

**Proto 定义**：`api/logic/logic.proto`

```protobuf
service Logic {
    rpc Connect (ConnectReq) returns (ConnectReply);
}

message ConnectReq {
    string server = 1;    // Comet 服务器 ID
    string cookie = 2;    // Cookie
    string token = 3;     // 鉴权 Token（JSON）
}

message ConnectReply {
    int64 mid = 1;        // 用户 ID
    string key = 2;       // 连接 Key
    string room_id = 3;   // 房间 ID
    repeated int32 accepts = 4;  // 接受的操作码
    int64 heartbeat = 5;  // 心跳间隔
}
```

**Server 端**：`internal/logic/grpc/server.go`

```go
type LogicServer struct {
    srv *logic.Logic
}

func (s *LogicServer) Connect(ctx context.Context, req *pb.ConnectReq) (*pb.ConnectReply, error) {
    // 调用 Logic 层的 Connect 方法
    mid, key, rid, accepts, hb, err := s.srv.Connect(ctx, req.Server, req.Cookie, req.Token)
    if err != nil {
        return nil, err
    }

    return &pb.ConnectReply{
        Mid:       mid,
        Key:       key,
        RoomId:    rid,
        Accepts:   accepts,
        Heartbeat: hb,
    }, nil
}
```

**Client 端**：`internal/comet/server.go`, `internal/comet/operation.go`

```go
type Server struct {
    rpcClient logic.LogicClient  // Logic gRPC 客户端
}

// Comet 调用 Logic 鉴权
func (s *Server) Connect(ctx context.Context, p *protocol.Proto, cookie string) (mid int64, key, rid string, accepts []int32, hb time.Duration, err error) {
    reply, err := s.rpcClient.Connect(ctx, &logic.ConnectReq{
        Server: s.serverID,
        Cookie: cookie,
        Token:  string(p.Body),
    })
    if err != nil {
        return
    }

    return reply.Mid, reply.Key, reply.RoomId, reply.Accepts, time.Duration(reply.Heartbeat), nil
}
```

**调用流程**：
```
客户端 WebSocket 连接
  → Comet.ServeWebsocket()
  → Comet.authWebsocket()
  → Comet.Connect() [调用 Logic gRPC]
  → Logic.Connect() [验证 Token]
  → 返回 mid, key, room_id
```

---

### 场景 2：Job 调用 Comet 推送消息

**Proto 定义**：`api/comet/comet.proto`

```protobuf
service Comet {
    rpc PushMsg (PushMsgReq) returns (PushMsgReply);
}

message PushMsgReq {
    repeated string keys = 1;  // 连接 Key 列表
    Proto proto = 2;           // 消息内容
    int32 proto_op = 3;        // 操作码
}

message PushMsgReply {
}
```

**Server 端**：`internal/comet/grpc/server.go`

```go
type CometServer struct {
    srv *comet.Server
}

func (s *CometServer) PushMsg(ctx context.Context, req *pb.PushMsgReq) (*pb.PushMsgReply, error) {
    // 遍历所有 key，推送消息
    for _, key := range req.Keys {
        bucket := s.srv.Bucket(key)
        if channel := bucket.Channel(key); channel != nil {
            // 检查 channel 是否订阅了这个操作码
            if !channel.NeedPush(req.ProtoOp) {
                continue
            }
            // 推送消息到 channel
            channel.Push(req.Proto)
        }
    }
    return &pb.PushMsgReply{}, nil
}
```

**Client 端**：`internal/job/comet.go`

```go
type Job struct {
    cometServers map[string]*CometClient  // Comet gRPC 客户端
}

// Job 推送消息到 Comet
func (j *Job) push(ctx context.Context, op int32, server string, keys []string, msg []byte) error {
    client := j.cometServers[server]

    _, err := client.PushMsg(ctx, &comet.PushMsgReq{
        Keys:    keys,
        ProtoOp: op,
        Proto: &protocol.Proto{
            Ver:  1,
            Op:   op,
            Body: msg,
        },
    })

    return err
}
```

**调用流程**：
```
外部 HTTP 推送
  → Logic.PushMids()
  → Kafka 发布消息
  → Job 消费消息 `func (j *Job) Consume()` `j.push(context.Background(), pushMsg)`
  j.pushKeys(...)
  c.Push(&args)
  c.pushChan[idx] <- arg
  
          → Job.push() [调用 Comet gRPC]
          → Comet.PushMsg() [找到 Channel]
          → Channel.Push() [推送到 WebSocket]
  
func New(c *conf.Config) *Job --- j.watchComet
func (j *Job) watchComet --- go func() {
    for { j.newAddress(ins.Instances) }
}()
func (j *Job) newAddress --- c, err := NewComet(in, j.c.Comet)
func NewComet --- go cmt.process(cmt.pushChan[i], cmt.roomChan[i], cmt.broadcastChan)
func (c *Comet) process --- 
  case pushArg := <-pushChan:
    c.client.PushMsg(...)
```

---

## 五、关键概念对比

### Demo vs goim

| 概念 | Demo | goim |
|---|---|---|
| **Proto 文件** | `hello.proto` | `api/logic/logic.proto`<br>`api/comet/comet.proto` |
| **Server 实现** | `GreeterServer` | `LogicServer`<br>`CometServer` |
| **Client 创建** | `pb.NewGreeterClient(conn)` | `logic.NewLogicClient(conn)`<br>`comet.NewCometClient(conn)` |
| **方法调用** | `client.SayHello(ctx, req)` | `client.Connect(ctx, req)`<br>`client.PushMsg(ctx, req)` |

---

## 六、常见问题

### 1. Context 是什么？

Context 用于传递请求上下文，包括：
- 超时控制：`context.WithTimeout(ctx, 3*time.Second)`
- 取消信号：`context.WithCancel(ctx)`
- 元数据：`metadata.NewOutgoingContext(ctx, md)`

**示例**：
```go
// 设置 3 秒超时
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

resp, err := client.SayHello(ctx, req)
if err != nil {
    // 可能是超时错误
}
```

### 2. 为什么要用 gRPC 而不是 HTTP？

| 特性 | gRPC | HTTP/JSON |
|---|---|---|
| 性能 | 高（二进制 Protobuf） | 低（文本 JSON） |
| 类型安全 | 强类型 | 弱类型 |
| 代码生成 | 自动生成 | 手动编写 |
| 流式传输 | 支持 | 不支持 |

### 3. goim 中有哪些 gRPC 调用？

| 调用方 | 被调用方 | 方法 | 作用 |
|---|---|---|---|
| Comet | Logic | Connect | 客户端鉴权 |
| Comet | Logic | Disconnect | 客户端断开 |
| Comet | Logic | Heartbeat | 心跳 |
| Comet | Logic | RenewOnline | 更新在线状态 |
| Job | Comet | PushMsg | 推送消息 |
| Job | Comet | Broadcast | 广播消息 |
| Job | Comet | BroadcastRoom | 房间广播 |

---

## 七、实战练习

### 练习 1：修改 Demo

在 `hello.proto` 中添加一个新方法：

```protobuf
rpc GetUserInfo (GetUserInfoReq) returns (GetUserInfoReply);

message GetUserInfoReq {
    int64 user_id = 1;
}

message GetUserInfoReply {
    string name = 1;
    int32 age = 2;
}
```

然后实现 Server 和 Client。

### 练习 2：阅读 goim 代码

1. 打开 `api/logic/logic.proto`，看看定义了哪些方法
2. 打开 `internal/logic/grpc/server.go`，看看如何实现这些方法
3. 打开 `internal/comet/server.go:50`，看看如何创建 Logic Client
4. 在 `internal/comet/server_websocket.go:419` 找到调用 `s.Connect()` 的地方

---

## 八、总结

**gRPC 三步走**：
1. 写 `.proto` 定义接口
2. 实现 Server 端
3. 创建 Client 调用

**goim 中的应用**：
- Comet ↔ Logic：鉴权、心跳、在线状态
- Job ↔ Comet：消息推送

**下一步**：
- 运行 Demo，理解基本流程
- 阅读 goim 的 proto 文件
- 跟踪一次 gRPC 调用（打日志）

有问题随时问我！
