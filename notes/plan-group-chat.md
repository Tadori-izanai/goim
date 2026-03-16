# 群聊功能实现计划

## Context

阶段四已完成单聊功能。现在要在此基础上实现群聊功能。

**核心设计思路**：Gateway 作为业务代理，goim 本体（Comet/Logic/Job）零改动。复用 goim 的房间推送机制。

## 整体架构

```
发送群消息:
Client A ── POST /goim/group/:group_id/chat ──→ Gateway
                                                  │ 1. 验证 JWT → 提取 sender mid
                                                  │ 2. 校验是否为群成员（查 MySQL）
                                                  │ 3. 消息落库（group_messages 表）
                                                  │ 4. 调 Logic HTTP POST /goim/push/room
                                                  │    ?operation=2002&type=group&room=<group_id>
                                                  ↓
                                                Logic
                                                  │ 5. 按房间广播 → MQ
                                                  ↓
                                                 Job
                                                  │ 6. 消费 → gRPC 推送到 Comet
                                                  ↓
                                                Comet ──→ 房间内所有 Client（WebSocket 收到消息）

客户端加入群房间:
Client ── WebSocket OpChangeRoom ──→ Comet
          body: "group://<group_id>"
          Comet 把该连接移入对应 Bucket/Room
```

**为什么 goim 本体不需要改动？**
- Logic 已有 `POST /goim/push/room?operation=<op>&type=<type>&room=<room>` 接口
- Gateway 调用此接口即可将消息推送给该房间的所有用户
- 客户端通过 `OpChangeRoom` 加入/切换房间，Comet 原生支持
- operation=2002 区分群聊消息，客户端 `accepts` 包含 2002

**群消息推送 vs 单聊推送的区别**：
- 单聊：`POST /goim/push/mids?mids=<to_id>` — 点对点推送
- 群聊：`POST /goim/push/room?type=group&room=<group_id>` — 房间广播
- 群聊不需要查询所有成员再逐个推送，goim 的房间机制天然支持广播

**客户端进群流程**：
1. 调 Gateway API 加入群（`POST /goim/group/:group_id/join`）→ 写入 group_members 表
2. WebSocket 发送 `OpChangeRoom`，body 为 `group://<group_id>` → Comet 将连接加入房间
3. 之后该房间的所有推送消息都能收到

## 数据库设计

### groups 表

```go
type Group struct {
    ID        int64     `gorm:"primaryKey;autoIncrement"`
    Name      string    `gorm:"size:128;not null"`
    OwnerID   int64     `gorm:"not null;index"`
    CreatedAt time.Time
}
```

### group_members 表

```go
type GroupMember struct {
    ID         int64     `gorm:"primaryKey;autoIncrement"`
    GroupID    int64     `gorm:"not null;uniqueIndex:idx_group_user"`
    UserID     int64     `gorm:"not null;uniqueIndex:idx_group_user;index:idx_user"`
    JoinedAt   time.Time `gorm:"not null"`
    LastReadAt time.Time `gorm:"not null"` // 用户在该群的已读位置，用于离线消息
}
```

索引说明：
- `(group_id, user_id)` 唯一索引：防止重复加入，校验是否为成员
- `(user_id)` 索引：查询用户加入了哪些群

`LastReadAt` 用于后续离线消息：用户进群时，查 `WHERE group_id=? AND created_at > last_read_at`。

### group_messages 表

```go
type GroupMessage struct {
    ID          int64         `gorm:"primaryKey;autoIncrement"`
    MsgID       string        `gorm:"size:36;uniqueIndex;not null"`
    GroupID     int64         `gorm:"not null;index:idx_group_created"`
    FromID      int64         `gorm:"not null"`
    ContentType int8          `gorm:"not null;default:1"`
    Content     string        `gorm:"type:text;not null"`
    CreatedAt   UnixMilliTime `gorm:"not null;index:idx_group_created"`
}
```

索引说明：
- `(group_id, created_at)` 联合索引：按群拉取历史/离线消息

## Gateway 新增接口

所有 `/goim/group/*` 接口需要 JWT 鉴权中间件。

### 群组管理接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/goim/group` | POST | 创建群 |
| `/goim/group/:group_id/join` | POST | 加入群 |
| `/goim/group/:group_id/quit` | POST | 退出群 |
| `/goim/group/:group_id/members` | GET | 群成员列表 |

**POST /goim/group**

请求：
```json
{
  "name": "技术交流群"
}
```

响应：
```json
{
  "code": 0,
  "data": {"id": 1, "name": "技术交流群"}
}
```

处理流程：
1. 创建群记录，owner_id = 当前用户
2. 自动将创建者加入 group_members
3. 返回群信息

**POST /goim/group/:group_id/join**

无需 request body。

响应：`{"code": 0}`

处理流程：
1. 校验群是否存在
2. 插入 group_members（已存在则忽略，幂等）
3. `LastReadAt` 设为当前时间（加入后不需要看之前的消息）

**POST /goim/group/:group_id/quit**

无需 request body。

响应：`{"code": 0}`

处理流程：
1. 删除 group_members 记录
2. 群主不能退群（需先转让或解散）

**GET /goim/group/:group_id/members**

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

### 群聊消息接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/goim/group/:group_id/chat` | POST | 发送群消息 |
| `/goim/group/:group_id/chat` | GET | 拉取群历史消息 |

**POST /goim/group/:group_id/chat**

请求：
```json
{
  "content_type": 1,
  "content": "大家好"
}
```

响应：
```json
{
  "code": 0,
  "data": "550e8400-e29b-41d4-a716-446655440000"
}
```

处理流程：
1. JWT 中间件提取 sender mid
2. 校验发送者是否为群成员
3. 生成 msg_id（UUID）
4. 消息落库 group_messages 表
5. 构造推送 body：
```json
{
  "msg_id": "550e8400-...",
  "group_id": 1,
  "from": 123,
  "content_type": 1,
  "content": "大家好",
  "timestamp": 1710000000000
}
```
6. 调用 Logic HTTP：
```
POST http://logic:3111/goim/push/room?operation=2002&type=group&room=1
Body: <上面的 JSON>
```
7. 返回 msg_id 给发送方

**GET /goim/group/:group_id/chat**

查询群内某个时间点之后的消息，命中 `(group_id, created_at)` 索引。

参数：
- `since` — Unix 毫秒时间戳，返回此时间之后的消息（必填）
- `limit` — 条数（可选，默认 50）

响应：
```json
{
  "code": 0,
  "data": [
    {
      "msg_id": "...",
      "group_id": 1,
      "from": 123,
      "content_type": 1,
      "content": "大家好",
      "timestamp": 1710000000000
    }
  ]
}
```

按 `created_at` 正序返回。

## 客户端消息格式

客户端通过 WebSocket 收到的群聊推送消息：
- `Op = 2002`（群聊消息）
- `Body` = 上面的 JSON

客户端需要在 `accepts` 中包含 `2002` 才能收到。
连接时的 auth body：`{"token":"eyJ...", "room_id":"", "accepts":[1000,1001,1002,2001,2002]}`

客户端进入群聊时，发送 `OpChangeRoom`（op=12），body 为 `group://<group_id>`。

**注意**：goim 原生的 Room 机制是每个连接只能在一个 Room 中。如果需要同时接收多个群的消息，需要评估是否改用 `POST /goim/push/mids` 逐个推送给群成员。但目前先使用 Room 机制，一次只接收一个群的实时消息，其他群的消息通过拉取历史获得。

## 新增错误定义

```go
// internal/gateway/errors.go 新增
var (
    ErrGroupNotFound   = errors.New("group not found")
    ErrNotGroupMember  = errors.New("not a group member")
    ErrOwnerCannotQuit = errors.New("group owner cannot quit")
)
```

## 新增文件结构

```
internal/gateway/
    model/group.go              # Group、GroupMember、GroupMessage GORM 模型
    dao/group.go                # 群组 CRUD
    group.go                    # 群组管理业务逻辑
    group_chat.go               # 群聊消息业务逻辑
    http/group.go               # 群组 + 群聊 handler
```

## 修改文件

| 文件 | 改动 |
|------|------|
| `internal/gateway/dao/dao.go` | AutoMigrate 加 Group、GroupMember、GroupMessage |
| `internal/gateway/errors.go` | 新增 ErrGroupNotFound、ErrNotGroupMember、ErrOwnerCannotQuit |
| `internal/gateway/http/server.go` | 新增 `/goim/group` 路由组 |
| `api/protocol/operation.go` | 新增 `OpGroupChatMsg = int32(2002)` |

**goim 本体零改动**（Logic / Job / Comet 不改）

## 实现顺序

1. `api/protocol/operation.go` — 新增 `OpGroupChatMsg = int32(2002)`
2. `internal/gateway/model/group.go` — Group、GroupMember、GroupMessage 模型
3. `internal/gateway/dao/dao.go` — AutoMigrate 加新表
4. `internal/gateway/dao/group.go` — 群组 CRUD + 测试
5. `internal/gateway/errors.go` — 新增错误定义
6. `internal/gateway/group.go` — 群组管理业务逻辑（创建、加入、退出、成员列表）
7. `internal/gateway/group_chat.go` — 群聊消息业务逻辑（发消息、历史）
8. `internal/gateway/http/group.go` — HTTP handler
9. `internal/gateway/http/server.go` — 注册路由
10. 测试：`group_test.go`、`group_chat_test.go`
11. 端到端 demo

## 验证方式

```bash
# 前置：启动 goim 全套 + gateway

# 1. 注册用户
curl -X POST http://localhost:3200/goim/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"123456"}'

curl -X POST http://localhost:3200/goim/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"123456"}'

# 2. 登录拿 token
TOKEN_A=$(curl -s -X POST http://localhost:3200/goim/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"123456"}' | jq -r '.data.token')

TOKEN_B=$(curl -s -X POST http://localhost:3200/goim/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"bob","password":"123456"}' | jq -r '.data.token')

# 3. Alice 创建群
curl -X POST http://localhost:3200/goim/group \
  -H "Authorization: Bearer $TOKEN_A" \
  -H 'Content-Type: application/json' \
  -d '{"name":"测试群"}'
# 返回: {"code":0,"data":{"id":1,"name":"测试群"}}

# 4. Bob 加入群
curl -X POST http://localhost:3200/goim/group/1/join \
  -H "Authorization: Bearer $TOKEN_B"

# 5. 查看群成员
curl http://localhost:3200/goim/group/1/members \
  -H "Authorization: Bearer $TOKEN_A"

# 6. Bob 通过 WebSocket 切换到群房间
#    发送 OpChangeRoom (op=12), body: "group://1"

# 7. Alice 发群消息
curl -X POST http://localhost:3200/goim/group/1/chat \
  -H "Authorization: Bearer $TOKEN_A" \
  -H 'Content-Type: application/json' \
  -d '{"content_type":1,"content":"大家好"}'
# 预期：Bob 的 WebSocket 收到 Op=2002 的消息

# 8. 拉取群历史消息
curl "http://localhost:3200/goim/group/1/chat?since=0&limit=50" \
  -H "Authorization: Bearer $TOKEN_A"

# 9. Bob 退出群
curl -X POST http://localhost:3200/goim/group/1/quit \
  -H "Authorization: Bearer $TOKEN_B"
```
