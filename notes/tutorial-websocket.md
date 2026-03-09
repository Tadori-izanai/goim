# WebSocket 详解与 goim 实战

## 一、什么是 WebSocket？

WebSocket 是一个**全双工通信协议**，允许服务器和客户端之间进行实时双向通信。

**与 HTTP 的区别**：

| 特性 | HTTP | WebSocket |
|---|---|---|
| 通信方式 | 请求-响应（单向） | 全双工（双向） |
| 连接 | 短连接（每次请求建立） | 长连接（一次建立，持续通信） |
| 服务器推送 | 不支持 | 支持 |
| 适用场景 | 网页浏览、API 调用 | 聊天、实时推送、游戏 |

**类比**：
- HTTP：打电话问问题，对方回答后挂断，下次再打
- WebSocket：打电话后一直保持通话，双方随时可以说话

---

## 二、WebSocket 握手过程

### 1. 客户端发起 HTTP 请求（升级请求）

```http
GET /ws HTTP/1.1
Host: localhost:8080
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Version: 13
```

### 2. 服务器响应（同意升级）

```http
HTTP/1.1 101 Switching Protocols
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=
```

### 3. 连接建立，开始 WebSocket 通信

---

## 三、Go WebSocket 基础

### 常用库

1. **gorilla/websocket**（最流行）
   - 功能完善，API 简单
   - goim 的 `examples/go-client` 使用这个

2. **goim 自己的 pkg/websocket**
   - 针对 goim 场景优化
   - 支持自定义二进制协议
   - 性能更高

**我们先用 gorilla/websocket 学习基础，再看 goim 的实现。**

---

## 四、Demo 详解

### 服务端（server/main.go）

#### 1. 创建升级器

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true  // 允许所有来源（生产环境需要检查）
    },
}
```

**作用**：将 HTTP 连接升级为 WebSocket

**CheckOrigin**：检查请求来源，防止跨域攻击

#### 2. 升级连接

```go
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
    // 升级 HTTP 连接为 WebSocket
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("升级失败: %v", err)
        return
    }
    defer conn.Close()

    // 现在 conn 是一个 WebSocket 连接
}
```

**关键**：<u>*`Upgrade()` 完成握手，返回 WebSocket 连接对象*</u>

#### 3. 读取消息

```go
for {
    // 读取消息（阻塞）
    messageType, message, err := conn.ReadMessage()
    if err != nil {
        log.Printf("读取失败: %v", err)
        break  // 连接断开
    }

    log.Printf("收到消息: %s", string(message))
}
```

**messageType**：
- `websocket.TextMessage` (1)：文本消息
- `websocket.BinaryMessage` (2)：二进制消息

#### 4. 发送消息

```go
response := "服务器收到: " + string(message)
err = conn.WriteMessage(websocket.TextMessage, []byte(response))
if err != nil {
    log.Printf("发送失败: %v", err)
    break
}
```

---

### 客户端（client/main.go）

#### 1. 连接服务器

```go
url := "ws://localhost:8080/ws"
conn, _, err := websocket.DefaultDialer.Dial(url, nil)
if err != nil {
    log.Fatalf("连接失败: %v", err)
}
defer conn.Close()
```

**DefaultDialer**：默认的拨号器，处理握手

#### 2. 发送消息

```go
err := conn.WriteMessage(websocket.TextMessage, []byte("Hello"))
if err != nil {
    log.Printf("发送失败: %v", err)
}
```

#### 3. 接收消息

```go
_, response, err := conn.ReadMessage()
if err != nil {
    log.Printf("接收失败: %v", err)
}
log.Printf("收到: %s", string(response))
```

---

## 五、运行 Demo

### 1. 启动服务端

```bash
cd examples/websocket-demo/server
go run main.go
```

输出：
```
WebSocket 服务器启动在 :8080
```

### 2. 运行客户端

```bash
cd examples/websocket-demo/client
go run main.go
```

输出：
```
✓ 已连接到服务器
→ 发送: Hello
← 收到: 服务器收到: Hello
→ 发送: World
← 收到: 服务器收到: World
→ 发送: WebSocket
← 收到: 服务器收到: WebSocket
客户端退出
```

服务端输出：
```
客户端已连接: 127.0.0.1:xxxxx
收到消息: Hello
收到消息: World
收到消息: WebSocket
客户端断开: 127.0.0.1:xxxxx
```

---

## 六、goim 中的 WebSocket 使用

### 1. goim 的 pkg/websocket 库

**为什么自己实现？**

- **性能优化**：针对高并发场景优化
- **自定义协议**：支持二进制协议（16 字节头 + Body）
- **零拷贝**：减少内存分配

**与 gorilla/websocket 的区别**：

| 特性 | gorilla/websocket | goim/pkg/websocket |
|---|---|---|
| 协议 | 标准 WebSocket | 标准 WebSocket + 自定义二进制 |
| 性能 | 通用 | 高性能优化 |
| API | 简单易用 | 针对 goim 场景 |

---

### 2. goim 服务端：internal/comet/server_websocket.go

#### 启动 WebSocket 服务

```go
// server_websocket.go:19
func InitWebsocket(server *Server, addrs []string, accept int) error {
    for _, bind := range addrs { // addrs: [":3102"]
        // 监听 TCP 端口
        listener, _ := net.ListenTCP("tcp", addr)

        // 启动多个 accept 协程
        for i := 0; i < accept; i++ {
            go acceptWebsocket(server, listener)
        }
    }
    return nil
}
```

**关键**：
- 监听 TCP 端口（如 3102）
- 启动多个协程并发 accept 连接

#### 接受连接

```go
// server_websocket.go:78
func acceptWebsocket(server *Server, lis *net.TCPListener) {
    for {
        // 接受 TCP 连接
        conn, err := lis.AcceptTCP()
        if err != nil {
            continue
        }

        // 处理连接（新协程）
        go serveWebsocket(server, conn, r)
    }
}
```

**关键**：每个连接一个协程

#### 处理连接

```go
// server_websocket.go:151
func serveWebsocket(s *Server, conn *net.TCPConn, r int) {
    // 1. 读取 HTTP 请求 (step := 0)
    req, err := http.ReadRequest(br)

    // 2. 检查路径（必须是 /sub）(step = 1)
    if req.RequestURI != "/sub" {
        return
    }

    // 3. 升级为 WebSocket (step = 2)
    ws := websocket.NewServerConn(conn, req, ...)

    // 4. 鉴权 (step = 3)
    mid, key, rid, accepts, hb, err := s.authWebsocket(ctx, ws, p, cookie)

    // 5. 创建 Channel
    ch := NewChannel(...)
    ch.Mid = mid
    ch.Key = key

    // 6. 加入 Bucket
    bucket := s.Bucket(key)
    bucket.Put(rid, ch)

    // 7. 启动读写协程 (step = 5)
    go s.dispatchWebsocket(ws, wp, wb, ch)  // 写协程
    s.readWebsocket(ws, rp, ch)             // 读协程（当前协程）
}
```

**关键流程**：
1. 升级连接
2. 鉴权（调用 Logic gRPC）
3. 创建 Channel 并加入 Bucket
4. 启动读写协程

#### 鉴权流程

```go
// server_websocket.go:408
func (s *Server) authWebsocket(ctx context.Context, ws *websocket.Conn, p *protocol.Proto, cookie string) (...) {
    for {
        // 读取消息
        if err = p.ReadWebsocket(ws); err != nil {
            return
        }

        // 检查是否是鉴权消息（Op=7）
        if p.Op == protocol.OpAuth {
            break
        }
    }

    // 调用 Logic gRPC 鉴权
    mid, key, rid, accepts, hb, err = s.Connect(ctx, p, cookie)
    if err != nil {
        return
    }

    // 返回鉴权成功（Op=8）
    p.Op = protocol.OpAuthReply
    p.Body = nil
    if err = p.WriteWebsocket(ws); err != nil {
        return
    }

    return
}
```

**关键**：
- 等待客户端发送 Op=7 的鉴权消息
- 调用 Logic.Connect() 验证
- 返回 Op=8 表示成功

#### 读协程

```go
// server_websocket.go:265
func (s *Server) readWebsocket(ws *websocket.Conn, rp *Ring, ch *Channel) {
    for {
        // 读取消息
        if err = p.ReadWebsocket(ws); err != nil {
            break
        }

        // 处理不同操作码
        switch p.Op {
        case protocol.OpHeartbeat:
            // 心跳：回复心跳响应
            p.Op = protocol.OpHeartbeatReply
            p.Body = nil
            ch.Push(p)

        case protocol.OpSendMsg:
            // 客户端发送消息（goim 不处理，需要业务层实现）

        default:
            // 其他操作
        }
    }
}
```

**关键**：
- 循环读取客户端消息
- 处理心跳
- 其他消息可以扩展处理

#### 写协程

```go
// server_websocket.go:349
func (s *Server) dispatchWebsocket(ws *websocket.Conn, wp *bytes.Pool, wb *bytes.Writer, ch *Channel) {
    for {
        // 从 Channel 读取消息（阻塞）
        p := ch.Ready()

        // 写入 WebSocket
        if err = p.WriteWebsocket(ws); err != nil {
            break
        }

        // 刷新缓冲区
        ws.Flush()
    }
}
```

**关键**：
- 从 `ch.signal` 读取消息
- 写入 WebSocket
- 这是推送消息的出口

---

### 3. goim 客户端：examples/go-client/main.go

#### 连接

```go
url := "ws://127.0.0.1:3102/sub"
conn, _, err := websocket.DefaultDialer.Dial(url, nil)
```

**路径**：必须是 `/sub`（goim 规定）

#### 发送鉴权

```go
func sendAuth(conn *websocket.Conn, mid int64, room string) error {
    token := AuthToken{
        Mid:      mid,
        RoomID:   "live://" + room,
        Platform: "web",
        Accepts:  []int32{1000, 1001, 1002},
    }

    body, _ := json.Marshal(token)
    packet := encodePacket(1, OpAuth, 1, body)  // Op=7

    conn.WriteMessage(websocket.BinaryMessage, packet)

    // 等待鉴权响应
    _, data, _ := conn.ReadMessage()
    op := decodeOp(data)
    if op == OpAuthReply {  // Op=8
        return nil
    }

    return fmt.Errorf("鉴权失败")
}
```

#### 发送心跳

```go
func heartbeatLoop(conn *websocket.Conn) {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        packet := encodePacket(1, OpHeartbeat, 1, nil)  // Op=2
        conn.WriteMessage(websocket.BinaryMessage, packet)
    }
}
```

#### 接收消息

```go
func receiveLoop(conn *websocket.Conn) {
    for {
        _, data, _ := conn.ReadMessage()
        handleMessage(data)
    }
}
```

---

## 七、goim 的二进制协议

### 协议格式

```
[0-3]   PackLen    uint32  包总长度
[4-5]   HeaderLen  uint16  头长度（固定 16）
[6-7]   Ver        uint16  协议版本
[8-11]  Op         uint32  操作码
[12-15] Seq        uint32  序列号
[16...] Body       []byte  消息体
```

### 为什么用二进制协议？

| 特性 | 文本协议（JSON） | 二进制协议 |
|---|---|---|
| 大小 | 大（需要编码） | 小（直接字节） |
| 解析速度 | 慢（需要解析 JSON） | 快（直接读取） |
| 可读性 | 好 | 差 |
| 适用场景 | API、调试 | 高性能、实时通信 |

**goim 选择二进制协议**：
- 性能优先
- 减少带宽
- 适合高并发场景

---

## 八、关键概念对比

### Demo vs goim

| 概念 | Demo | goim |
|---|---|---|
| **WebSocket 库** | gorilla/websocket | goim/pkg/websocket |
| **协议** | 文本消息 | 自定义二进制协议 |
| **路径** | `/ws` | `/sub` |
| **鉴权** | 无 | Op=7 鉴权，调用 Logic gRPC |
| **心跳** | 无 | Op=2 心跳，30 秒间隔 |
| **读写** | 同一协程 | 分离（读协程 + 写协程） |

---

## 九、数据流图

### Demo 数据流

```
客户端                    服务端
  ↓                        ↓
连接 ws://localhost:8080/ws
  ↓                        ↓
                      升级为 WebSocket
  ↓                        ↓
发送 "Hello"          ← 读取消息
  ↓                        ↓
                      处理并回复
  ↓                        ↓
接收 "服务器收到: Hello"
```

### goim 数据流

```
客户端                         Comet
  ↓                             ↓
连接 ws://127.0.0.1:3102/sub
  ↓                             ↓
                           升级为 WebSocket
  ↓                             ↓
发送 Op=7 鉴权              ← 读取鉴权消息
  ↓                             ↓
                           调用 Logic.Connect() gRPC
  ↓                             ↓
                           创建 Channel，加入 Bucket
  ↓                             ↓
接收 Op=8 鉴权成功          → 返回鉴权响应
  ↓                             ↓
定时发送 Op=2 心跳          ← 读协程：处理心跳
  ↓                             ↓
                           写协程：从 Channel 读取消息
  ↓                             ↓
接收 Op=1000 业务消息       → 推送消息
```

---

## 十、常见问题

### 1. WebSocket 和 HTTP 的关系？

WebSocket 基于 HTTP 握手，但握手后就是独立的协议。

```
HTTP 握手 → 升级为 WebSocket → 独立通信
```

### 2. 为什么需要心跳？

- 检测连接是否存活
- 防止中间代理（如 Nginx）超时断开
- goim 要求 30 秒心跳

### 3. 读写为什么要分离？

**不分离（Demo）**：
```go
for {
    msg := conn.ReadMessage()  // 阻塞
    conn.WriteMessage(response)
}
```
问题：读取时无法发送消息

**分离（goim）**：
```go
// 读协程
go func() {
    for {
        msg := conn.ReadMessage()
        // 处理消息
    }
}()

// 写协程
go func() {
    for {
        msg := <-ch.signal  // 从 Channel 读取
        conn.WriteMessage(msg)
    }
}()
```
好处：读写互不阻塞，可以随时推送

### 4. goim 的 pkg/websocket 需要深入吗？

**不需要**。你只需要知道：
- 它实现了标准 WebSocket 协议
- 提供了 `ReadWebsocket()` 和 `WriteWebsocket()` 方法
- 支持自定义二进制协议

**重点关注**：
- `server_websocket.go` 的业务逻辑
- 鉴权流程
- 读写协程的分工

---

## 十一、总结

### WebSocket 基础

1. **全双工通信**：服务器和客户端可以随时发送消息
2. **长连接**：一次握手，持续通信
3. **适合实时场景**：聊天、推送、游戏

### Go WebSocket

1. **gorilla/websocket**：简单易用，适合学习
2. **goim/pkg/websocket**：高性能，针对 goim 优化

### goim 的使用

1. **服务端**：
   - 升级连接 → 鉴权 → 创建 Channel → 读写分离
2. **客户端**：
   - 连接 → 鉴权 → 心跳 → 接收消息

### 下一步

现在你可以：
1. 运行 `websocket-demo` 理解基础
2. 阅读 `server_websocket.go` 理解 goim 的实现
3. 对比 Demo 和 goim，理解优化点

准备好了吗？我们可以开始详细阅读 `server_websocket.go` 了！
