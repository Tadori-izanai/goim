# GoIM Go 客户端 Demo

这是一个用 Go 编写的 GoIM WebSocket 客户端示例，功能等同于 `examples/javascript/` 中的 JS 版本。

## 功能

- 连接到 Comet WebSocket 服务（`ws://127.0.0.1:3102/sub`）
- 发送鉴权消息（Op=7）
- 自动心跳（每 30 秒，Op=2）
- 接收并解析服务端推送的消息（Op=9 批量消息、自定义 Op）

## 使用方法

### 1. 安装依赖

```bash
cd examples/go-client
go mod tidy
```

### 2. 启动 GoIM 服务

确保 Comet 和 Logic 服务已启动：
- Comet: `ws://127.0.0.1:3102/sub`
- Logic: `http://127.0.0.1:3111`

### 3. 运行客户端

```bash
go run main.go
```

### 4. 推送测试消息

在另一个终端执行：

```bash
# 推送给指定用户（mid=123）
curl -d 'Hello User 123' 'http://127.0.0.1:3111/goim/push/mids?operation=1000&mids=123'

# 推送给房间（live://1000）
curl -d 'Hello Room 1000' 'http://127.0.0.1:3111/goim/push/room?operation=1000&type=live&room=1000'

# 全员广播
curl -d 'Broadcast Message' 'http://127.0.0.1:3111/goim/push/all?operation=1000'
```

## 输出示例

```
✓ WebSocket 连接成功
→ 发送鉴权: {"mid":123,"room_id":"live://1000","platform":"web","accepts":[1000,1001,1002]}
← 收到消息: PackLen=16, Ver=1, Op=8, Seq=1
✓ 鉴权成功
  [鉴权响应]
→ 发送心跳
← 收到消息: PackLen=16, Ver=1, Op=3, Seq=1
  [心跳响应]
← 收到消息: PackLen=50, Ver=1, Op=1000, Seq=0
  [业务消息] Op=1000, Body=Hello User 123
```

## 代码说明

### 协议格式

```
Header (16 bytes):
  [0-3]   PackLen    uint32  包总长度
  [4-5]   HeaderLen  uint16  头长度（固定 16）
  [6-7]   Ver        uint16  协议版本
  [8-11]  Op         uint32  操作码
  [12-15] Seq        uint32  序列号
Body:
  [16...] 消息体（可选）
```

### 主要函数

- `sendAuth()`: 发送鉴权消息
- `heartbeatLoop()`: 心跳循环（30 秒间隔）
- `receiveLoop()`: 接收消息循环
- `handleMessage()`: 解析并处理收到的消息
- `encodePacket()`: 编码协议包
- `decodeOp()`: 解码操作码

## 与 JavaScript 版本对比

| 功能 | JavaScript | Go |
|---|---|---|
| WebSocket 连接 | ✓ | ✓ |
| 鉴权 | ✓ | ✓ |
| 心跳 | ✓ | ✓ |
| 接收消息 | ✓ | ✓ |
| 批量消息解析 | ✓ | ✓ |

## 参考文档

详细的 API 文档请查看 `notes/api-reference.md`
