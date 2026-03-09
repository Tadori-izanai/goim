# 第一步：理解数据流 - 详细指南

## 目标

跟踪一条消息从外部 HTTP 推送到客户端 WebSocket 接收的完整路径。

---

## 阅读顺序（逐文件详解）

### 文件 1：`examples/javascript/index.html`

**目的**：理解外部如何推送消息

**阅读重点**：
```html
<!-- 第 27-33 行 -->
<p>
  curl -d 'mid message' 'http://127.0.0.1:3111/goim/push/mids?operation=1000&mids=123'
</p>
<p>
  curl -d 'room message' 'http://127.0.0.1:3111/goim/push/room?operation=1000&type=live&room=1000'
</p>
```

**记录到笔记**：
```
入口：HTTP POST 请求
- URL: http://127.0.0.1:3111/goim/push/mids
- 参数: operation=1000, mids=123
- Body: 消息内容（字符串）
```

---

### 文件 2：`internal/logic/http/server.go`

**目的**：理解 Logic HTTP 路由

**阅读重点**：
```go
// 第 33-44 行
func (s *Server) initRouter() {
    group := s.engine.Group("/goim")
    group.POST("/push/keys", s.pushKeys)
    group.POST("/push/mids", s.pushMids)   // ← 我们关注这个
    group.POST("/push/room", s.pushRoom)
    group.POST("/push/all", s.pushAll)
    // ...
}
```

**记录到笔记**：
```
Logic HTTP 路由
- /goim/push/mids → s.pushMids 方法
```

---

### 文件 3：`internal/logic/http/push.go`

**目的**：理解 HTTP 接口如何处理请求

**阅读重点**：
```go
// 第 32-52 行
func (s *Server) pushMids(c *gin.Context) {
    var arg struct {
        Op   int32   `form:"operation"`  // 操作码
        Mids []int64 `form:"mids"`       // 用户 ID 列表
    }
    if err := c.BindQuery(&arg); err != nil {
        errors(c, RequestErr, err.Error())
        return
    }
    // 读取消息内容
    msg, err := ioutil.ReadAll(c.Request.Body)
    if err != nil {
        errors(c, RequestErr, err.Error())
        return
    }
    // 调用 Logic 层
    if err = s.logic.PushMids(context.TODO(), arg.Op, arg.Mids, msg); err != nil {
        result(c, nil, ServerErr)
        return
    }
    result(c, nil, OK)
}
```

**记录到笔记**：
```
pushMids 处理流程：
1. 解析参数：operation, mids
2. 读取 Body：消息内容
3. 调用 s.logic.PushMids(op, mids, msg)
```

**画图（第一部分）**：
```
外部 HTTP
  ↓
Logic HTTP Server (port 3111)
  ↓
pushMids(op=1000, mids=[123], msg="hello")
  ↓
s.logic.PushMids()
```

---

### 文件 4：`internal/logic/push.go`

**目的**：理解 Logic 如何处理推送逻辑

**阅读重点**：
```go
// 第 15-40 行
func (l *Logic) PushMids(c context.Context, op int32, mids []int64, msg []byte) (err error) {
    // 1. 根据 mids 查询 keys（从 Redis）
    keyServers, _, err := l.dao.ServersByKeys(c, keys)
    if err != nil {
        return
    }

    // 2. 按 server 分组
    pushKeys := make(map[string][]string)
    for key, server := range keyServers {
        pushKeys[server] = append(pushKeys[server], key)
    }

    // 3. 发送到 Kafka
    for server, keys := range pushKeys {
        if err = l.dao.PushMsg(c, op, server, keys, msg); err != nil {
            return
        }
    }
    return
}
```

**关键步骤**：

1. **查询 keys**：
   - 输入：`mids = [123]`
   - 调用：`l.dao.KeysByMids(mids)` → Redis 查询
   - 输出：`keys = ["server1_conn456", "server2_conn789"]`

2. **按 server 分组**：
   ```
   pushKeys = {
       "server1": ["server1_conn456"],
       "server2": ["server2_conn789"]
   }
   ```

3. **发送到 Kafka**：
   - 为每个 server 发送一条 Kafka 消息

**记录到笔记**：
```
PushMids 流程：
1. KeysByMids(mids) → 查询 Redis → keys
2. 按 server 分组 keys
3. 为每个 server 调用 dao.PushMsg() → Kafka
```

**画图（第二部分）**：
```
s.logic.PushMids(op, mids, msg)
  ↓
1. dao.KeysByMids(mids=[123])
  ↓
Redis 查询
  ↓
keys = ["server1_conn456"]
  ↓
2. 按 server 分组
  ↓
pushKeys = {"server1": ["server1_conn456"]}
  ↓
3. dao.PushMsg(op, server, keys, msg)
  ↓
Kafka
```

---

### 文件 5：`internal/logic/dao/kafka.go`

**目的**：理解如何发送到 Kafka

**阅读重点**：
```go
// 找到 PushMsg 方法
func (d *Dao) PushMsg(c context.Context, op int32, server string, keys []string, msg []byte) (err error) {
    pushMsg := &pb.PushMsg{
        Type:      pb.PushMsg_PUSH,
        Operation: op,
        Server:    server,
        Keys:      keys,
        Msg:       msg,
    }
    b, _ := proto.Marshal(pushMsg)

    // 发送到 Kafka
    m := &sarama.ProducerMessage{
        Topic: "goim-push-topic",
        Key:   sarama.StringEncoder(keys[0]),
        Value: sarama.ByteEncoder(b),
    }
    _, _, err = d.kafkaPub.SendMessage(m)
    return
}
```

**记录到笔记**：
```
Kafka 消息格式：
- Topic: "goim-push-topic"
- Key: keys[0]
- Value: PushMsg {
    Type: PUSH,
    Operation: 1000,
    Server: "server1",
    Keys: ["server1_conn456"],
    Msg: "hello"
  }
```

**画图（第三部分）**：
```
dao.PushMsg()
  ↓
构造 PushMsg
  ↓
Kafka.SendMessage(topic="goim-push-topic")
```

---

### 文件 6：`internal/job/job.go`

**目的**：理解 Job 如何消费 Kafka

**阅读重点**：
```go
// 第 50-80 行（大致位置）
func (j *Job) Consume() {
    for {
        msg := <-j.consumer.Messages()

        // 解析消息
        pushMsg := new(pb.PushMsg)
        proto.Unmarshal(msg.Value, pushMsg)

        // 处理推送
        switch pushMsg.Type {
        case pb.PushMsg_PUSH:
            j.push(pushMsg)
        case pb.PushMsg_ROOM:
            j.pushRoom(pushMsg)
        case pb.PushMsg_BROADCAST:
            j.broadcast(pushMsg)
        }
    }
}
```

**记录到笔记**：
```
Job 消费 Kafka：
1. 从 Kafka 读取消息
2. 解析 PushMsg
3. 根据 Type 调用不同方法
   - PUSH → j.push()
   - ROOM → j.pushRoom()
   - BROADCAST → j.broadcast()
```

**画图（第四部分）**：
```
Kafka (topic="goim-push-topic")
  ↓
Job.Consume()
  ↓
解析 PushMsg
  ↓
j.push(pushMsg)
```

---

### 文件 7：`internal/job/push.go`

**目的**：理解 Job 如何调用 Comet

**阅读重点**：
```go
func (j *Job) push(pushMsg *pb.PushMsg) {
    // 1. 获取 Comet gRPC 客户端
    cometClient := j.cometServers[pushMsg.Server]

    // 2. 调用 Comet.PushMsg
    _, err := cometClient.PushMsg(context.Background(), &comet.PushMsgReq{
        Keys:    pushMsg.Keys,
        ProtoOp: pushMsg.Operation,
        Proto: &protocol.Proto{
            Ver:  1,
            Op:   pushMsg.Operation,
            Body: pushMsg.Msg,
        },
    })
}
```

**记录到笔记**：
```
j.push() 流程：
1. 根据 server 找到 Comet gRPC 客户端
2. 调用 cometClient.PushMsg(keys, op, proto)
```

**画图（第五部分）**：
```
j.push(pushMsg)
  ↓
获取 cometClient (server="server1")
  ↓
cometClient.PushMsg(
    keys=["server1_conn456"],
    op=1000,
    proto={Ver:1, Op:1000, Body:"hello"}
)
  ↓
gRPC 调用 Comet
```

---

### 文件 8：`internal/comet/grpc/server.go`

**目的**：理解 Comet gRPC 接口

**阅读重点**：
```go
// 第 47-70 行
func (s *CometServer) PushMsg(ctx context.Context, req *pb.PushMsgReq) (*pb.PushMsgReply, error) {
    // 遍历所有 keys
    for _, key := range req.Keys {
        // 1. 根据 key 找到 Bucket
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

**记录到笔记**：
```
Comet.PushMsg() 流程：
1. 遍历 keys
2. Bucket(key) → 找到 Bucket
3. bucket.Channel(key) → 找到 Channel
4. channel.NeedPush(op) → 检查订阅
5. channel.Push(proto) → 推送到队列
```

**画图（第六部分）**：
```
Comet gRPC Server
  ↓
PushMsg(keys=["server1_conn456"], op=1000, proto)
  ↓
for key in keys:
  ↓
  Bucket(key) → Bucket[5]
  ↓
  bucket.Channel(key) → Channel
  ↓
  channel.NeedPush(1000)? → true
  ↓
  channel.Push(proto)
```

---

### 文件 9：`internal/comet/channel.go`

**目的**：理解 Channel 如何推送消息

**阅读重点**：
```go
// 第 67-74 行
func (c *Channel) Push(p *protocol.Proto) (err error) {
    select {
    case c.signal <- p:  // 非阻塞发送到队列
    default:
        err = errors.ErrSignalFullMsgDropped  // 队列满了，丢弃
    }
    return
}
```

**记录到笔记**：
```
channel.Push() 流程：
1. 将 proto 发送到 c.signal 队列
2. 如果队列满了，丢弃消息
```

---

### 文件 10：`internal/comet/server_websocket.go`

**目的**：理解消息如何发送到 WebSocket

**阅读重点**：
```go
// 第 350-395 行（dispatchWebsocket 函数）
func dispatchWebsocket(conn *websocket.Conn, ch *Channel) {
    for {
        // 1. 从 signal 队列读取消息
        proto := ch.Ready()  // 阻塞等待

        // 2. 写入 WebSocket
        if err := proto.WriteWebsocket(conn); err != nil {
            break
        }

        // 3. Flush
        conn.Flush()
    }
}
```

**记录到笔记**：
```
dispatchWebsocket 流程：
1. ch.Ready() → 从 signal 队列读取 proto
2. proto.WriteWebsocket(conn) → 写入 WebSocket
3. conn.Flush() → 发送
```

**画图（第七部分）**：
```
channel.Push(proto)
  ↓
c.signal <- proto (队列)
  ↓
dispatchWebsocket 协程
  ↓
proto := ch.Ready() (阻塞读取)
  ↓
proto.WriteWebsocket(conn)
  ↓
conn.Flush()
  ↓
客户端 WebSocket 收到消息
```

---

## 完整数据流图

### 方式 1：文字版（适合笔记）

```
[外部 HTTP POST]
  ↓
[Logic HTTP Server :3111]
  /goim/push/mids?operation=1000&mids=123
  ↓
[Logic.PushMids()]
  ├─ 1. dao.KeysByMids(123) → Redis
  │    └─ 返回: keys=["server1_conn456"]
  ├─ 2. 按 server 分组
  │    └─ pushKeys={"server1": ["server1_conn456"]}
  └─ 3. dao.PushMsg() → Kafka
       └─ topic="goim-push-topic"
            value=PushMsg{server, keys, op, msg}
  ↓
[Kafka]
  ↓
[Job.Consume()]
  ├─ 解析 PushMsg
  └─ j.push(pushMsg)
       └─ cometClient.PushMsg() [gRPC]
  ↓
[Comet gRPC Server]
  PushMsg(keys, op, proto)
  ├─ for key in keys:
  │    ├─ bucket = Bucket(key)
  │    ├─ channel = bucket.Channel(key)
  │    ├─ if channel.NeedPush(op):
  │    └─    channel.Push(proto)
  ↓
[Channel.signal 队列]
  ↓
[dispatchWebsocket 协程]
  ├─ proto = ch.Ready()
  ├─ proto.WriteWebsocket(conn)
  └─ conn.Flush()
  ↓
[客户端 WebSocket]
```

### 方式 2：表格版（适合对照代码）

| 步骤 | 组件 | 文件 | 函数 | 输入 | 输出 |
|---|---|---|---|---|---|
| 1 | 外部 | - | curl | operation, mids, msg | HTTP 请求 |
| 2 | Logic HTTP | http/server.go | initRouter | - | 路由注册 |
| 3 | Logic HTTP | http/push.go | pushMids | op, mids, msg | 调用 Logic |
| 4 | Logic | push.go | PushMids | op, mids, msg | 调用 dao |
| 5 | Logic DAO | dao/redis.go | KeysByMids | mids | keys |
| 6 | Logic DAO | dao/kafka.go | PushMsg | op, server, keys, msg | Kafka 消息 |
| 7 | Kafka | - | - | PushMsg | 存储 |
| 8 | Job | job.go | Consume | - | 读取 Kafka |
| 9 | Job | push.go | push | pushMsg | gRPC 调用 |
| 10 | Comet gRPC | grpc/server.go | PushMsg | keys, op, proto | 找 Channel |
| 11 | Comet | server.go | Bucket | key | Bucket |
| 12 | Bucket | bucket.go | Channel | key | Channel |
| 13 | Channel | channel.go | NeedPush | op | bool |
| 14 | Channel | channel.go | Push | proto | signal 队列 |
| 15 | Comet | server_websocket.go | dispatchWebsocket | - | WebSocket 发送 |
| 16 | 客户端 | - | - | - | 收到消息 |

### 方式 3：画图工具（推荐）

使用 draw.io 或 Excalidraw 画图：

**布局建议**：
```
从上到下：
┌─────────────┐
│ 外部 HTTP   │
└──────┬──────┘
       ↓
┌─────────────┐
│ Logic HTTP  │
└──────┬──────┘
       ↓
┌─────────────┐
│ Logic.Push  │
└──┬───┬───┬──┘
   ↓   ↓   ↓
 Redis Kafka
       ↓
┌─────────────┐
│ Job.Consume │
└──────┬──────┘
       ↓ gRPC
┌─────────────┐
│ Comet.Push  │
└──────┬──────┘
       ↓
┌─────────────┐
│ Channel     │
└──────┬──────┘
       ↓
┌─────────────┐
│ WebSocket   │
└─────────────┘
```

---

## 实践步骤

### 1. 边读边记（30 分钟）

创建 `notes/data-flow.md`，按上面的顺序逐个文件阅读，记录：
- 函数名
- 输入参数
- 输出结果
- 调用的下一个函数

### 2. 画图（30 分钟）

选择一种方式画图：
- **简单**：用文字版（复制上面的模板）
- **清晰**：用表格版（Excel 或 Markdown）
- **专业**：用 draw.io（https://app.diagrams.net/）

### 3. 验证（30 分钟）

运行 demo，打日志验证：

```go
// 在关键函数打日志
log.Printf("[DEBUG] pushMids: op=%d, mids=%v, msg=%s", op, mids, string(msg))
log.Printf("[DEBUG] PushMids: keys=%v", keys)
log.Printf("[DEBUG] PushMsg to Kafka: server=%s, keys=%v", server, keys)
log.Printf("[DEBUG] Comet.PushMsg: keys=%v, op=%d", req.Keys, req.ProtoOp)
log.Printf("[DEBUG] Channel.Push: key=%s, op=%d", ch.Key, p.Op)
```

然后：
```bash
# 启动服务
# 运行客户端
# 推送消息
curl -d 'test' 'http://127.0.0.1:3111/goim/push/mids?operation=1000&mids=123'

# 查看日志，验证数据流
```

---

## 输出

完成后你应该有：
1. `notes/data-flow.md` - 详细的阅读笔记
2. 一张数据流图（文字/表格/图片）
3. 对整个推送流程的清晰理解

准备好了吗？从文件 1 开始！
