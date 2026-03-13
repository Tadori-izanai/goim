# 单聊功能实现计划

## Context

阶段三已完成用户系统与 JWT 鉴权。现在要在此基础上实现单聊功能。

**核心设计思路**：Gateway 作为业务代理，goim 本体（Comet/Logic/Job）零改动。

## 整体架构

```
发送消息:
Client A ── POST /goim/chat/send ──→ Gateway
                                       │ 1. 验证 JWT → 提取 sender mid
                                       │ 2. 校验好友关系（查 MySQL）
                                       │ 3. 消息落库（messages 表，全量存储）
                                       │ 4. 调 Logic HTTP POST /goim/push/mids（实时推送）
                                       ↓
                                     Logic
                                       │ 5. Redis 查 mid → key/server 映射
                                       │ 6. 序列化 → MQ
                                       ↓
                                      Job
                                       │ 7. 消费 → gRPC 推送
                                       ↓
                                     Comet ──→ Client B（WebSocket 收到消息）

拉取历史 / 离线消息:
Client ── GET /goim/chat/history ──→ Gateway ──→ MySQL 查询 ──→ 返回
```

**为什么 goim 本体不需要改动？**
- Logic 已有 `POST /goim/push/mids?operation=<op>&mids=<mid>` 接口
- Gateway 调用此接口即可将消息推送给指定用户
- operation 可以传任意整数（如 2001），客户端通过 op 区分消息类型
- MQ → Job → Comet 全链路对 operation 值透明，原样传递

**消息持久化策略**：发送时同步落库全量消息。这样做的好处：
- 历史消息查询直接查 messages 表
- 离线消息 = 查 `to_id = ? AND created_at > last_online_at`，不需要单独的离线表
- 一张表同时满足历史和离线两个需求

## 数据库设计

### friends 表

```go
type Friend struct {
    ID        int64     `gorm:"primaryKey;autoIncrement"`
    UserID    int64     `gorm:"not null;index:idx_user_friend,unique"`
    FriendID  int64     `gorm:"not null;index:idx_user_friend,unique"`
    CreatedAt time.Time
}
```

好友关系**双向存储**：A 和 B 互为好友时，插入两行 `(A,B)` 和 `(B,A)`。
查好友列表只需 `WHERE user_id = ?`，不需要 OR 条件，查询简单高效。

### messages 表

```go
type Message struct {
    ID          int64     `gorm:"primaryKey;autoIncrement"`
    MsgID       string    `gorm:"size:36;uniqueIndex;not null"` // UUID
    FromID      int64     `gorm:"not null"`
    ToID        int64     `gorm:"not null;index:idx_to_created"`
    ContentType int8      `gorm:"not null;default:1"`           // 1=文本
    Content     string    `gorm:"type:text;not null"`
    CreatedAt   time.Time `gorm:"not null;index:idx_to_created"`
}
```

索引说明：
- `(to_id, created_at)` 联合索引：高效查询用户的离线消息（`WHERE to_id=? AND created_at > ?`）
- 历史消息查询（A 和 B 之间的对话）：`(from=A AND to=B) OR (from=B AND to=A) ORDER BY created_at DESC`

## Gateway 新增接口

所有 `/goim/chat/*` 和 `/goim/friend/*` 接口需要 JWT 鉴权中间件。

### JWT 鉴权中间件

```go
// internal/gateway/http/middleware.go 新增
func (s *Server) authMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 从 Authorization: Bearer <token> 中提取 token
        // auth.ParseToken() 验证 → 提取 mid
        // c.Set("mid", claims.Mid)
    }
}
```

### 好友接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/goim/friend/add` | POST | 添加好友 |
| `/goim/friend/remove` | POST | 删除好友 |
| `/goim/friend/list` | GET | 好友列表 |

**POST /goim/friend/add**

请求：`{"friend_id": 456}`
响应：`{"code": 0}`

- 双向插入 `(mid, friend_id)` 和 `(friend_id, mid)`
- 已存在则忽略（幂等）
- 需要校验 friend_id 用户是否存在

**POST /goim/friend/remove**

请求：`{"friend_id": 456}`
响应：`{"code": 0}`

- 双向删除

**GET /goim/friend/list**

响应：
```json
{
  "code": 0,
  "data": [
    {"id": 456, "username": "bob"},
    {"id": 789, "username": "charlie"}
  ]
}
```

### 聊天接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/goim/chat/send` | POST | 发送单聊消息 |
| `/goim/chat/history` | GET | 拉取离线/历史消息 |

**POST /goim/chat/send**

请求：
```json
{
  "to": 456,
  "content_type": 1,
  "content": "你好"
}
```

响应：
```json
{
  "code": 0,
  "data": {
    "msg_id": "550e8400-e29b-41d4-a716-446655440000",
    "timestamp": 1710000000
  }
}
```

处理流程：
1. JWT 中间件提取 sender mid
2. 校验好友关系（`SELECT 1 FROM friends WHERE user_id=? AND friend_id=?`）
3. 生成 msg_id（UUID）
4. 消息落库 messages 表
5. 构造推送 body：
```json
{
  "msg_id": "550e8400-...",
  "from": 123,
  "to": 456,
  "content_type": 1,
  "content": "你好",
  "timestamp": 1710000000
}
```
6. 调用 Logic HTTP：
```
POST http://logic:3111/goim/push/mids?operation=2001&mids=456
Body: <上面的 JSON>
```
7. 返回 msg_id 给发送方

**GET /goim/chat/history**

查询当前用户在某个时间点之后收到的所有消息，命中 `(to_id, created_at)` 索引。
客户端自行按 `from_id` 分组到各会话。

参数：
- `since` — Unix 时间戳（秒），返回此时间之后的消息（可选，默认返回最近消息）
- `limit` — 条数（可选，默认 50，最大 200）

响应：
```json
{
  "code": 0,
  "data": [
    {
      "msg_id": "...",
      "from": 123,
      "to": 456,
      "content_type": 1,
      "content": "你好",
      "timestamp": 1710000000
    },
    {
      "msg_id": "...",
      "from": 789,
      "to": 456,
      "content_type": 1,
      "content": "hi",
      "timestamp": 1710000001
    }
  ]
}
```

按 `created_at` 正序返回。客户端根据 `from` 字段自行归类到对应会话。

### 用户信息接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/goim/user/info` | GET | 批量查询用户公开信息 |

**GET /goim/user/info**

参数：`ids` — 逗号分隔的用户 ID 列表（如 `ids=1,2,3`）

响应：
```json
{
  "code": 0,
  "data": [
    {"id": 1, "username": "alice"},
    {"id": 2, "username": "bob"}
  ]
}
```

客户端拿到消息列表后，用其中的 `from` ID 批量查询用户信息，用于展示用户名等。
需要 JWT 鉴权。

## 客户端消息格式

客户端通过 WebSocket 收到的推送消息：
- `Op = 2001`（单聊消息）
- `Body` = 上面的 JSON

客户端需要在 `accepts` 中包含 `2001` 才能收到。
连接时的 auth body：`{"token":"eyJ...", "room_id":"", "accepts":[1000,1001,1002,2001]}`

## 新增文件结构

```
internal/gateway/
    model/friend.go             # Friend GORM 模型
    model/message.go            # Message GORM 模型
    dao/friend.go               # 好友 CRUD
    dao/message.go              # 消息 CRUD
    chat.go                     # 发消息、查历史业务逻辑
    friend.go                   # 好友管理业务逻辑
    http/middleware.go          # 新增 JWT 鉴权中间件（已有 logger/recover）
    http/chat.go                # 聊天 handler
    http/friend.go              # 好友 handler
    http/user.go                # 用户信息 handler
    http/server.go              # 新增路由组
```

## 修改文件

| 文件 | 改动 |
|------|------|
| `internal/gateway/dao/dao.go` | AutoMigrate 加 Friend、Message ✅ |
| `internal/gateway/dao/user.go` | 新增 GetUsersByIDs 批量查询 |
| `internal/gateway/http/server.go` | 新增路由组 + JWT 中间件 ✅ |
| `internal/gateway/http/middleware.go` | 新增 jwtHandler |
| `examples/jwt-client/main.go` | accepts 加 2001 |

**goim 本体零改动**（Logic / Job / Comet 不改）

## 实现顺序

1. ~~`internal/gateway/model/friend.go` + `model/message.go`~~ ✅
2. ~~`internal/gateway/dao/dao.go` — AutoMigrate 加新表~~ ✅
3. ~~`internal/gateway/dao/friend.go` + 测试~~ ✅
4. ~~`internal/gateway/dao/message.go` + 测试~~ ✅
5. `internal/gateway/dao/user.go` — 新增 GetUsersByIDs
6. `internal/gateway/http/middleware.go` — jwtHandler
7. `internal/gateway/friend.go` + `http/friend.go` — 好友管理
8. `internal/gateway/chat.go` + `http/chat.go` — 发消息 + 历史
9. `http/user.go` — 用户信息查询
10. `internal/gateway/http/server.go` — 注册新路由 ✅
11. 更新 `examples/jwt-client/main.go` — accepts 加 2001
12. 端到端测试

## 验证方式

```bash
# 前置：启动 goim 全套 + gateway

# 1. 注册两个用户
curl -X POST http://localhost:3200/goim/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"123456"}'

curl -X POST http://localhost:3200/goim/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"123456"}'

# 2. 登录拿 token
TOKEN_A=$(curl -s http://localhost:3200/goim/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"123456"}' | jq -r '.data.token')

TOKEN_B=$(curl -s http://localhost:3200/goim/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"123456"}' | jq -r '.data.token')

# 3. 添加好友
curl -X POST http://localhost:3200/goim/friend/add \
  -H "Authorization: Bearer $TOKEN_A" \
  -H 'Content-Type: application/json' \
  -d '{"friend_id": <bob_mid>}'

# 4. Bob 用 jwt-client 连接 WebSocket（accepts 包含 2001）
go run examples/jwt-client/main.go -user bob -pass 123456

# 5. Alice 发消息给 Bob
curl -X POST http://localhost:3200/goim/chat/send \
  -H "Authorization: Bearer $TOKEN_A" \
  -H 'Content-Type: application/json' \
  -d '{"to": <bob_mid>, "content_type": 1, "content": "hello bob"}'
# 预期：Bob 的 WebSocket 收到 Op=2001 的消息

# 6. 拉取历史（since 为 Unix 时间戳）
curl "http://localhost:3200/goim/chat/history?since=0&limit=50" \
  -H "Authorization: Bearer $TOKEN_A"

# 7. 批量查用户信息（消息中的 from ID）
curl "http://localhost:3200/goim/user/info?ids=1,2" \
  -H "Authorization: Bearer $TOKEN_A"
```

