# WebSocket Demo

这是一个简单的 WebSocket 示例，帮助理解 goim 中的 WebSocket 使用。

## 功能

- 服务端：接收客户端消息并回复
- 客户端：发送消息并接收响应

## 使用方法

### 1. 安装依赖

```bash
cd examples/websocket-demo
go mod tidy
```

### 2. 启动服务端

```bash
cd server
go run main.go
```

输出：
```
WebSocket 服务器启动在 :8080
```

### 3. 运行客户端（新终端）

```bash
cd client
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

## 核心概念

### 服务端

1. **升级连接**：`upgrader.Upgrade()` 将 HTTP 升级为 WebSocket
2. **读取消息**：`conn.ReadMessage()` 阻塞读取
3. **发送消息**：`conn.WriteMessage()` 发送响应

### 客户端

1. **连接服务器**：`websocket.DefaultDialer.Dial()`
2. **发送消息**：`conn.WriteMessage()`
3. **接收消息**：`conn.ReadMessage()`

## 对应 goim 的使用

| Demo | goim |
|---|---|
| `upgrader.Upgrade()` | `websocket.NewServerConn()` |
| `conn.ReadMessage()` | `p.ReadWebsocket(ws)` |
| `conn.WriteMessage()` | `p.WriteWebsocket(ws)` |
| 文本消息 | 二进制协议（16字节头+Body） |

## 与 goim 的区别

| 特性 | Demo | goim |
|---|---|---|
| 库 | gorilla/websocket | goim/pkg/websocket |
| 协议 | 标准 WebSocket | 自定义二进制协议 |
| 消息格式 | 文本/二进制 | Proto（Ver/Op/Seq/Body） |
| 鉴权 | 无 | Op=7 鉴权，Op=8 响应 |
| 心跳 | 无 | Op=2 心跳，Op=3 响应 |

详细说明见 `../../notes/websocket-tutorial.md`
