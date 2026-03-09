# WebSocket Demo (goim/pkg/websocket)

这是一个使用 goim/pkg/websocket 库的示例，展示了 goim 内部使用的 WebSocket 实现。

## 与 gorilla/websocket 的区别

| 特性 | gorilla/websocket | goim/pkg/websocket |
|---|---|---|
| **API 层级** | 高层封装 | 底层实现 |
| **握手** | `upgrader.Upgrade()` | 手动读取 HTTP 请求 + `Upgrade()` |
| **缓冲** | 内部管理 | 需要手动创建 bufio |
| **刷新** | 自动 | 需要手动调用 `Flush()` |
| **客户端** | 支持 | 不支持（只有服务端） |

## 使用方法

### 1. 安装依赖

```bash
cd examples/websocket-goim
go mod tidy
```

### 2. 启动服务端

```bash
cd server
go run main.go
```

### 3. 运行客户端（新终端）

```bash
cd client
go run main.go
```

## 核心代码解析

### 服务端关键步骤

```go
// 1. 接受 TCP 连接
conn, _ := listener.Accept()

// 2. 创建 bufio（goim 的优化）
rb := bufio.NewReaderSize(conn, 4096)
wb := bufio.NewWriterSize(conn, 4096)

// 3. 读取 HTTP 请求
req, _ := websocket.ReadRequest(rb)

// 4. 升级为 WebSocket
ws, _ := websocket.Upgrade(conn, rb, wb, req)

// 5. 读写消息
msgType, msg, _ := ws.ReadMessage()
ws.WriteMessage(msgType, response)
ws.Flush()  // 必须手动刷新！
```

### 与 goim 实际代码的对应

| Demo | goim (server_websocket.go) |
|---|---|
| `listener.Accept()` | `lis.AcceptTCP()` (line 88) |
| `bufio.NewReaderSize()` | `ch.Reader.ResetBuffer()` (line 170) |
| `websocket.ReadRequest()` | `websocket.ReadRequest(rr)` (line 184) |
| `websocket.Upgrade()` | `websocket.Upgrade(conn, rr, wr, req)` (line 197) |
| `ws.ReadMessage()` | `p.ReadWebsocket(ws)` (line 247) |
| `ws.WriteMessage()` | `p.WriteWebsocket(ws)` (line 354) |
| `ws.Flush()` | `ws.Flush()` (line 382) |

## 为什么 goim 使用自己的 websocket 库？

### 1. 性能优化

- **零拷贝**：直接操作 bufio，减少内存分配
- **手动刷新**：控制何时发送数据，批量发送

### 2. 自定义协议

- 支持 goim 的二进制协议（16字节头 + Body）
- 与 `protocol.Proto` 无缝集成

### 3. 精简实现

- 只实现服务端（goim 不需要客户端）
- 去掉不需要的功能，代码更简洁

## 注意事项

### 1. 必须手动 Flush

```go
ws.WriteMessage(msgType, msg)
ws.Flush()  // 不调用这个，消息不会发送！
```

### 2. bufio 的重要性

goim 使用自己的 `pkg/bufio`，提供了：
- `Peek()` - 预览缓冲区
- `Pop()` - 读取并移除数据
- 更高效的内存管理

### 3. 客户端使用 gorilla

goim/pkg/websocket 只实现了服务端，客户端仍然使用 gorilla/websocket（如 examples/go-client）。

## 总结

**学习建议**：
1. 先理解 gorilla/websocket（简单）
2. 再看 goim/pkg/websocket（底层）
3. 阅读 goim 代码时，把它当作"优化版的 WebSocket 库"

**关键区别**：
- gorilla：开箱即用，自动管理
- goim：手动控制，性能优先

详细说明见 `../../notes/websocket-tutorial.md`
