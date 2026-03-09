# GoIM API 参考文档

## 一、整体架构与消息流

### 消息推送流程

```
外部系统 → Logic HTTP API → Kafka → Job 消费 → Comet gRPC → WebSocket 客户端
```

1. 外部系统通过 HTTP 调用 Logic 的推送接口
2. Logic 将消息发布到 Kafka
3. Job 从 Kafka 消费消息
4. Job 通过 gRPC 调用 Comet 的 PushMsg 接口
5. Comet 找到对应的 WebSocket 连接，推送给客户端

### 客户端连接流程

```
客户端 → ws://comet:3102/sub → 发送 OpAuth(7) → Comet 调用 Logic gRPC 鉴权 → 返回 OpAuthReply(8) → 开始心跳
```

---

## 二、Comet 服务（接入层）

### WebSocket 接口

**地址**: `ws://127.0.0.1:3102/sub`

**协议格式**: 二进制协议（16 字节头 + Body）

```
Header (16 bytes):
  [0-3]   PackLen    int32  包总长度（含头）
  [4-5]   HeaderLen  int16  头长度（固定 16）
  [6-7]   Ver        int16  协议版本
  [8-11]  Op         int32  操作码
  [12-15] Seq        int32  序列号
Body:
  [16...]  消息体（可选）
```

### 操作码（Operation）

| Op | 名称 | 方向 | 说明 |
|---|---|---|---|
| 7 | OpAuth | Client → Server | 鉴权请求 |
| 8 | OpAuthReply | Server → Client | 鉴权响应 |
| 2 | OpHeartbeat | Client → Server | 心跳请求 |
| 3 | OpHeartbeatReply | Server → Client | 心跳响应 |
| 9 | OpRaw | Server → Client | 批量消息（业务消息） |
| 1000+ | 自定义 | Server → Client | 业务自定义操作码 |

### 连接流程

1. **建立 WebSocket 连接**
   ```
   ws://127.0.0.1:3102/sub
   ```

2. **发送鉴权消息（Op=7）**
   ```json
   Body: {"mid":123, "room_id":"live://1000", "platform":"web", "accepts":[1000,1001,1002]}
   ```
   - `mid`: 用户 ID
   - `room_id`: 房间 ID（格式：`类型://房间号`）
   - `platform`: 平台标识
   - `accepts`: 接受的操作码列表

3. **接收鉴权响应（Op=8）**
   - Body 为空表示成功

4. **定时发送心跳（Op=2）**
   - 建议间隔 30 秒
   - Body 为空

5. **接收业务消息（Op=9 或自定义）**
   - Op=9: 批量消息，Body 中包含多个子消息包
   - Op>=1000: 单条业务消息

---

## 三、Logic 服务（逻辑层）

### HTTP API

**Base URL**: `http://127.0.0.1:3111`

详见 [push.md](../docs/push.md)

---

#### 1. 推送给指定用户（按 Key）

**接口**: `POST /goim/push/keys`

**Query 参数**:
- `operation` (int32, 必填): 操作码，客户端会收到此 Op
- `keys` ([]string, 必填): Key 列表（多个用重复参数）

**Body**: 原始消息内容（会原样推送给客户端）

**示例**:
```bash
# 单个 key
curl -X POST 'http://127.0.0.1:3111/goim/push/keys?operation=1000&keys=key1' \
  -d 'Hello from keys'

# 多个 key
curl -X POST 'http://127.0.0.1:3111/goim/push/keys?operation=1000&keys=key1&keys=key2' \
  -d 'Hello from keys'
```

**说明**: Key 是客户端连接时 Logic 分配的唯一标识，通常是 `{ServerID}_{ConnID}`

---

#### 2. 推送给指定用户（按 Mid）

**接口**: `POST /goim/push/mids`

**Query 参数**:
- `operation` (int32, 必填): 操作码
- `mids` ([]int64, 必填): 用户 ID 列表（多个用重复参数）

**Body**: 原始消息内容

**示例**:
```bash
# 单个 mid
curl -X POST 'http://127.0.0.1:3111/goim/push/mids?operation=1000&mids=123' \
  -d 'Hello User 123'

# 多个 mid
curl -X POST 'http://127.0.0.1:3111/goim/push/mids?operation=1000&mids=123&mids=456' \
  -d 'Hello Users'
```

**说明**: Mid 是用户 ID，鉴权时客户端传入的 `mid` 字段

---

#### 3. 推送给房间

**接口**: `POST /goim/push/room`

**Query 参数**:
- `operation` (int32, 必填): 操作码
- `type` (string, 必填): 房间类型（如 `live`）
- `room` (string, 必填): 房间号（如 `1000`）

**Body**: 原始消息内容

**示例**:
```bash
curl -X POST 'http://127.0.0.1:3111/goim/push/room?operation=1000&type=live&room=1000' \
  -d 'Hello room 1000'
```

**说明**: 房间 ID 格式为 `{type}://{room}`，如 `live://1000`

---

#### 4. 全员广播

**接口**: `POST /goim/push/all`

**Query 参数**:
- `operation` (int32, 必填): 操作码
- `speed` (int32, 可选): 推送速度限制（每秒推送数）

**Body**: 原始消息内容

**示例**:
```bash
curl -X POST 'http://127.0.0.1:3111/goim/push/all?operation=1000&speed=100' \
  -d 'Broadcast message'
```

---

#### 5. 查询在线用户（Top）

**接口**: `GET /goim/online/top`

**Query 参数**:
- `type` (string, 必填): 房间类型（如 `live`）
- `limit` (int, 必填): 返回数量限制

**示例**:
```bash
curl 'http://127.0.0.1:3111/goim/online/top?type=live&limit=10'
```

**响应**:
```json
{
    "code": 0,
    "message": "",
    "data": [
        {
            "room_id": "1000",
            "count": 100
        },
        {
            "room_id": "2000",
            "count": 200
        },
        {
            "room_id": "3000",
            "count": 300
        }
    ]
}
```

---

#### 6. 查询房间在线数

**接口**: `GET /goim/online/room`

**Query 参数**:
- `type` (string, 必填): 房间类型
- `rooms` ([]string, 必填): 房间号列表（多个用重复参数）

**示例**:
```bash
# 单个房间
curl 'http://127.0.0.1:3111/goim/online/room?type=live&rooms=1000'

# 多个房间
curl 'http://127.0.0.1:3111/goim/online/room?type=live&rooms=1000&rooms=1001'
```

**响应**:

```json
{
    "code": 0,
    "message": "",
    "data": {
        "1000": 100,
        "2000": 200,
        "3000": 300
    }
}
```

---

#### 7. 查询总在线数

**接口**: `GET /goim/online/total`

```sh
curl 'http://127.0.0.1:3111/goim/online/total'
```

**响应**:

```json
{
    "code": 0,
    "message": "",
    "data": {
        "conn_count": 1,
        "ip_count": 1
    }
}
```

---

## 四、协议细节

### 二进制协议编码

**Go 示例（发送心跳）**:
```go
// Header: 16 bytes
buf := make([]byte, 16)
binary.BigEndian.PutUint32(buf[0:4], 16)      // PackLen
binary.BigEndian.PutUint16(buf[4:6], 16)      // HeaderLen
binary.BigEndian.PutUint16(buf[6:8], 1)       // Ver
binary.BigEndian.PutUint32(buf[8:12], 2)      // Op (Heartbeat)
binary.BigEndian.PutUint32(buf[12:16], 1)     // Seq
ws.WriteMessage(websocket.BinaryMessage, buf)
```

**Go 示例（发送鉴权）**:
```go
token := `{"mid":123, "room_id":"live://1000", "platform":"web", "accepts":[1000,1001,1002]}`
body := []byte(token)
packLen := 16 + len(body)

buf := make([]byte, packLen)
binary.BigEndian.PutUint32(buf[0:4], uint32(packLen))
binary.BigEndian.PutUint16(buf[4:6], 16)
binary.BigEndian.PutUint16(buf[6:8], 1)
binary.BigEndian.PutUint32(buf[8:12], 7)      // Op (Auth)
binary.BigEndian.PutUint32(buf[12:16], 1)
copy(buf[16:], body)
ws.WriteMessage(websocket.BinaryMessage, buf)
```

### 接收消息解析

**Go 示例**:
```go
_, data, err := ws.ReadMessage()
if err != nil {
    return err
}

packLen := binary.BigEndian.Uint32(data[0:4])
headerLen := binary.BigEndian.Uint16(data[4:6])
ver := binary.BigEndian.Uint16(data[6:8])
op := binary.BigEndian.Uint32(data[8:12])
seq := binary.BigEndian.Uint32(data[12:16])

var body []byte
if packLen > 16 {
    body = data[headerLen:packLen]
}

switch op {
case 8:  // OpAuthReply
    fmt.Println("Auth success")
case 3:  // OpHeartbeatReply
    fmt.Println("Heartbeat reply")
case 9:  // OpRaw (batch messages)
    // 解析批量消息
    offset := 16
    for offset < len(data) {
        subPackLen := binary.BigEndian.Uint32(data[offset:offset+4])
        subHeaderLen := binary.BigEndian.Uint16(data[offset+4:offset+6])
        subOp := binary.BigEndian.Uint32(data[offset+8:offset+12])
        subBody := data[offset+subHeaderLen:offset+int(subPackLen)]
        fmt.Printf("Op=%d, Body=%s\n", subOp, string(subBody))
        offset += int(subPackLen)
    }
default:
    fmt.Printf("Op=%d, Body=%s\n", op, string(body))
}
```

---

## 五、常见问题

### 1. 客户端如何获取 Key？

Key 是 Comet 内部生成的，格式为 `{ServerID}_{ConnID}`。外部系统通常不直接使用 Key，而是用 Mid（用户 ID）或 Room 来推送。

### 2. 鉴权 Token 格式是什么？

当前 demo 中是 JSON 字符串，包含 `mid`、`room_id`、`platform`、`accepts` 字段。实际项目中可以改为 JWT 或其他加密 Token。

### 3. 如何实现离线消息？

GoIM 本身不存储消息。需要在 Logic 或 Job 层增加持久化逻辑，将消息写入 MySQL/MongoDB，用户上线后主动拉取。

### 4. 如何实现单聊/群聊？

- **单聊**: 用 `push/mids` 推送给指定用户
- **群聊**: 用 `push/room` 推送给房间，客户端连接时加入对应房间

### 5. Operation 操作码如何定义？

- 0-999: 系统保留（如鉴权、心跳）
- 1000+: 业务自定义（如 1000=文本消息，1001=图片消息）
