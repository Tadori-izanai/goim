# 阶段六：离线消息与 ACK 实现计划

## Context

阶段四五已完成单聊 + 群聊的消息落库与实时推送。当前问题：

- 用户离线期间收到的消息，push 到 Logic 后找不到 Comet 连接，消息被静默丢弃
- 消息虽然已落库 MySQL，但客户端重连后没有主动同步机制
- Comet 的 `channel.Push()` 在 signal 缓冲区满时丢消息，即使用户在线也可能漏收
- 没有 ACK 确认，服务端不知道客户端是否真正收到消息

**目标**：
1. 客户端断线重连后，主动拉取离线消息（Pull 模式）
2. 单聊消息增加 ACK 机制：Comet 推送成功后回调 Gateway 确认，超时未 ACK 则重推
3. 群聊消息不做逐条 ACK，依赖客户端定期拉取兜底

## 整体架构

```
一、单聊消息 ACK 流程:

  Alice POST /goim/chat → Gateway
    1. 落库 MySQL（得到 msg_id）
    2. push Logic /goim/push/mids
    3. 记入 pending map: pending[msg_id] = {mid, body, timer, retries}
    4. 立刻返回 Alice {"code":0, "msg_id":"xxx"}

  推送链路:
    Gateway → Logic（查 Redis 找 mid 的 Comet 连接）
      → 找到: Job → Comet → channel.Push() → Bob WebSocket 收到
               Comet POST Gateway /goim/internal/ack {"msg_id":"xxx"}  ← 新增
               → Gateway 从 pending map 移除，取消定时器
      → 未找到: Logic POST Gateway /goim/internal/ack {"msg_id":"xxx"}  ← 复用同一接口
               → Gateway 从 pending map 移除（消息在 DB，等 Bob Pull）

  超时重试:
    5s 未收到 ACK → Gateway 重新 push 同一条消息 → 最多 3 次
    → 3 次后放弃，从 pending 移除（消息在 DB，等 Bob Pull）

  用户下线时:
    Logic.Disconnect() → POST Gateway /goim/internal/offline {"mid":bob}
    → Gateway 批量清除该 mid 的所有 pending（不再重试）
    → Gateway 更新 users.last_online_at

二、离线消息 Pull 流程:

  Bob 断线重连后:
    Bob → GET /goim/sync?since=<last_ack_at>&limit=200
      → Gateway 查询单聊 + 群聊离线消息
      → 返回消息列表
    Bob → POST /goim/sync/ack {"ack_at": <最新消息时间戳>}
      → Gateway 更新 users.last_ack_at

三、群聊消息兜底:

  群聊不做逐条 ACK（N 个成员 × 每条消息 = pending 膨胀太大）
  客户端定期 GET /goim/sync 拉取，补全可能漏掉的群消息
```

**设计要点**：

- **单聊 ACK**：Gateway pending map 跟踪每条单聊消息的送达状态，Comet 推送成功后直接回调 Gateway
- **离线消息 Pull**：客户端重连后主动拉取，不走 Logic/Comet 推送链路，路径短、可靠
- **混合同步锚点**：`last_online_at`（断线时刻）+ `last_ack_at`（Pull 确认位点）
- **ACK 粒度**：全局单一 `last_ack_at`，单聊 + 群聊共用
- **消息去重**：客户端按 `msg_id` 去重（重试可能导致重复推送）
- **群聊不做 ACK**：靠客户端定期 Pull 兜底，避免 pending map 膨胀
- **接口简化**：`/ack` 和 `/undelivered` 合并为一个 `/ack` 接口，Comet 和 Logic 都调用它
- **解耦设计**：ackService 通过 `messagePusher` 接口依赖 Gateway，避免循环依赖
- **性能优化**：midMap 反向索引加速 UserOffline 批量清除，O(k) 复杂度

## 数据库变更

### users 表新增字段

```go
// internal/gateway/model/user.go
type User struct {
    ID           int64         `gorm:"primaryKey;autoIncrement" json:"id"`
    Username     string        `gorm:"uniqueIndex;size:64;not null" json:"username"`
    Password     string        `gorm:"size:255;not null" json:"-"`
    LastOnlineAt time.Time     `json:"last_online_at,omitempty"`  // 断线时间（Logic 回调更新）
    LastAckAt    UnixMilliTime `json:"last_ack_at,omitempty"`     // ACK 位点（客户端 Pull 后更新）
    CreatedAt    time.Time     `json:"created_at,omitempty"`
    UpdatedAt    time.Time     `json:"updated_at,omitempty"`
}
```

- `LastOnlineAt`：Logic 断连回调时更新为 `NOW()`
- `LastAckAt`：客户端 ACK 时更新，作为离线消息查询的起点
- 注册时 `LastAckAt` 初始化为当前时间（避免新用户拉到全量历史）

## Comet 改动（1 个文件）

### internal/comet/grpc/server.go（或 internal/comet/operation.go）

Comet 推送消息给客户端成功后，异步 POST Gateway 确认送达。

改动点：在 `PushMsg` 处理中，`channel.Push(proto)` 成功后，异步通知 Gateway。

```go
// 推送成功后回调 Gateway
go notifyGatewayAck(gatewayAddr, msgID)

func notifyGatewayAck(addr, msgID string) {
    body, _ := json.Marshal(map[string]string{"msg_id": msgID})
    resp, err := http.Post(addr+"/goim/internal/ack", "application/json", bytes.NewReader(body))
    if err != nil {
        log.Errorf("notify gateway ack msg_id:%s error(%v)", msgID, err)
        return
    }
    resp.Body.Close()
}
```

需要从 push body 中提取 msg_id。Comet 配置需新增 Gateway 地址。

### internal/comet/conf/conf.go

```go
type Config struct {
    // ... 现有字段
    Gateway *Gateway
}

type Gateway struct {
    Addr string
}
```

### cmd/comet/comet-example.toml

```toml
[gateway]
    addr = "http://localhost:3200"
```

## Logic 改动（2 个文件）

### internal/logic/push.go

`PushMids` 中，当 `KeysByMids` 查不到目标 mid 时（用户不在线），POST Gateway `/goim/internal/ack` 通知：

```go
func (l *Logic) PushMids(c context.Context, op int32, mids []int64, msg []byte) error {
    keysByMid, _, err := l.dao.KeysByMids(c, mids)
    // ...
    // 新增：找出不在线的 mid，通知 Gateway（复用 /ack 接口）
    // 需要从 msg 中提取 msg_id
    for _, mid := range mids {
        if !isOnline(mid, keysByMid) {
            msgID := extractMsgID(msg)  // 从 push body 提取 msg_id
            go l.notifyGatewayAck(msgID)
        }
    }
    // ... 原有推送逻辑
}
```

### internal/logic/conn.go

Disconnect() 新增通知 Gateway 用户下线：

```go
func (l *Logic) Disconnect(...) (...) {
    if has, err = l.dao.DelMapping(c, mid, key, server); err != nil { ... }

    if has {
        go l.notifyGateway("/goim/internal/offline", mid)
    }
    return
}

func (l *Logic) notifyGateway(path string, mid int64) {
    if l.c.Gateway == nil || l.c.Gateway.Addr == "" {
        return
    }
    body, _ := json.Marshal(map[string]int64{"mid": mid})
    resp, err := http.Post(l.c.Gateway.Addr+path, "application/json", bytes.NewReader(body))
    if err != nil {
        log.Errorf("notify gateway %s mid:%d error(%v)", path, mid, err)
        return
    }
    resp.Body.Close()
}
```

### internal/logic/conf/conf.go + cmd/logic/logic-example.toml

同前：Config 新增 `Gateway.Addr`。

## Gateway 新增组件

### 1. ACK 服务 — `internal/gateway/ack.go`

```go
// messagePusher 接口解耦循环依赖（ackService 不直接依赖 Gateway）
type messagePusher interface {
    pushToMids(op int32, mids []int64, body []byte) error
}

type ackService struct {
    mu      sync.Mutex
    pending map[string]*pendingMsg  // msgID → pending state
    midMap  map[int64][]string      // mid → msgIDs (反向索引，加速 UserOffline)
    pusher  messagePusher           // 依赖接口而非具体类型
    conf    *conf.ACK
}

type pendingMsg struct {
    mid     int64       // 接收者
    op      int32       // OpSingleChatMsg (2001)
    body    []byte      // push body（重发用）
    timer   *time.Timer
    retries int
}
```

核心方法：

```go
// Track 发送单聊消息后调用，记入 pending 并启动超时定时器
func (s *ackService) Track(msgID string, mid int64, op int32, body []byte)

// Ack 确认消息已处理（Comet 推送成功 OR Logic 发现不在线），从 pending 移除
// 合并了原 Ack 和 Undelivered 两个方法，简化接口
func (s *ackService) Ack(msgID string)

// UserOffline Logic 断连回调，批量清除该 mid 的所有 pending
func (s *ackService) UserOffline(mid int64)
```

**Track 流程**：
1. 存入 pending map 和 midMap 反向索引
2. 启动定时器（默认 5s）
3. 超时触发：retries++ → 重新 push → 重置定时器
4. 达到 MaxRetries → 移除 pending

**Ack 流程**（统一处理成功送达和用户离线两种情况）：
1. 从 pending map 查找 msg_id
2. 取消定时器
3. 从 pending 和 midMap 移除

**UserOffline 流程**（O(k) 复杂度，k 为该 mid 的消息数）：
1. 从 midMap 查找该 mid 的所有 msgID（O(1)）
2. 逐个取消定时器，从 pending 和 midMap 移除（O(k)）

### 2. 离线同步 — `internal/gateway/sync.go`

```go
// SyncMessages 客户端 Pull 离线消息
func (g *Gateway) SyncMessages(ctx context.Context, userID int64, since time.Time, limit int) (*SyncResult, error)

// SyncAck 客户端确认已收到，更新 last_ack_at
func (g *Gateway) SyncAck(ctx context.Context, userID int64, ackAt time.Time) error

type SyncResult struct {
    Messages      []*model.Message      `json:"messages"`
    GroupMessages []*model.GroupMessage  `json:"group_messages"`
}
```

**SyncMessages 流程**：
1. 查询单聊离线消息：`ListMessagesSince(userID, since, limit)`（已有方法）
2. 查询群聊离线消息：`ListOfflineGroupMessages(userID, since, limit)`（新增）
3. 合并返回

### 3. 配置扩展 — `internal/gateway/conf/conf.go`

```go
type Config struct {
    // ... 现有字段
    ACK *ACK
}

type ACK struct {
    RetryInterval int  // 重试间隔秒数，默认 5
    MaxRetries    int  // 最大重试次数，默认 3
}
```

### 4. Gateway 结构体 — `internal/gateway/gateway.go`

```go
type Gateway struct {
    c      *conf.Config
    dao    *dao.Dao
    client *http.Client
    ack    *ackService  // 新增
}
```

### 5. chat.go 改动 — `internal/gateway/chat.go`

`SendMessage` 在 push 后调用 `ack.Track()`：

```go
func (g *Gateway) SendMessage(...) (string, error) {
    // ... 校验好友、落库（得到 msgID）
    // ... 构造 pushBody、push Logic

    // 新增：记入 pending，启动 ACK 跟踪
    g.ack.Track(msgID, toID, protocol.OpSingleChatMsg, pushBody)

    return msgID, err
}
```

群聊 `SendGroupMessage` 不调用 Track（不做 ACK）。

### 6. DAO 新增方法

**`internal/gateway/dao/user.go`**：
```go
func (d *Dao) UpdateLastOnlineAt(ctx context.Context, userID int64, t time.Time) error
func (d *Dao) UpdateLastAckAt(ctx context.Context, userID int64, t time.Time) error
func (d *Dao) GetLastAckAt(ctx context.Context, userID int64) (time.Time, error)
```

**`internal/gateway/dao/message.go`**：
```go
// ListOfflineGroupMessages 查询用户在所有已加入群的离线消息
// JOIN group_members 确保只返回用户当前所在群、加入后的消息
func (d *Dao) ListOfflineGroupMessages(ctx context.Context, userID int64, since time.Time, limit int) ([]*model.GroupMessage, error)
```

SQL:
```sql
SELECT gm.* FROM group_messages gm
JOIN group_members gme ON gm.group_id = gme.group_id
WHERE gme.user_id = ? AND gm.created_at > ? AND gm.created_at > gme.joined_at
ORDER BY gm.created_at LIMIT ?
```

## Gateway 新增 HTTP 接口

### 内部接口（Comet/Logic → Gateway 回调，无需 JWT）

| 接口 | 方法 | 说明 |
|------|------|------|
| `/goim/internal/ack` | POST | Comet 推送成功 OR Logic 发现不在线 |
| `/goim/internal/offline` | POST | Logic 用户断连通知 |

**POST /goim/internal/ack**

请求：`{"msg_id": "xxx"}`
处理：`ackService.Ack(msgID)` → 从 pending 移除，取消定时器
调用方：
- Comet：推送成功后调用
- Logic：`PushMids` 发现目标 mid 不在线时调用（消息在 DB，等 Pull）

**POST /goim/internal/offline**

请求：`{"mid": 12345}`
处理：
1. `ackService.UserOffline(mid)` → 批量清除该 mid 的 pending
2. `dao.UpdateLastOnlineAt(mid, now)`

### 外部接口（客户端调用，需 JWT）

| 接口 | 方法 | 说明 |
|------|------|------|
| `/goim/sync` | GET | 拉取离线消息 |
| `/goim/sync/ack` | POST | 确认已收到，更新 last_ack_at |

**GET /goim/sync**

参数：`since` (Unix 毫秒时间戳)、`limit` (默认 200)
响应：
```json
{
  "code": 0,
  "data": {
    "messages": [...],
    "group_messages": [...]
  }
}
```

**POST /goim/sync/ack**

请求：`{"ack_at": 1710000000000}`
响应：`{"code": 0}`

### Login 响应扩展

`POST /goim/auth/login` 的 data 新增 `last_ack_at`：

```json
{
  "code": 0,
  "data": {
    "id": 1,
    "token": "eyJ...",
    "last_ack_at": 1710000000000,
    "nodes": { ... }
  }
}
```

客户端用此值作为首次 `GET /goim/sync?since=` 的参数。

## 新增文件

```
internal/gateway/
    ack.go                      # ACK 服务（pending map、重试、回调处理）
    ack_test.go                 # ACK 服务测试
    sync.go                     # 离线同步业务逻辑
    sync_test.go                # 离线同步测试

internal/gateway/http/
    sync.go                     # GET /goim/sync + POST /goim/sync/ack handler
    internal.go                 # /goim/internal/* handler

examples/
    offline-demo/main.go        # 端到端测试 demo
```

## 修改文件

| 文件 | 改动 |
|------|------|
| `internal/gateway/model/user.go` | User 新增 LastOnlineAt、LastAckAt |
| `internal/gateway/dao/user.go` | 新增 UpdateLastOnlineAt、UpdateLastAckAt、GetLastAckAt |
| `internal/gateway/dao/message.go` | 新增 ListOfflineGroupMessages |
| `internal/gateway/conf/conf.go` | Config 新增 ACK 配置 |
| `internal/gateway/gateway.go` | Gateway 新增 ackService |
| `internal/gateway/chat.go` | SendMessage 后调用 ack.Track() |
| `internal/gateway/auth.go` | LoginResponse 加 LastAckAt；Register 初始化 LastAckAt |
| `internal/gateway/http/server.go` | 新增 /goim/sync 和 /goim/internal 路由 |
| `cmd/gateway/gateway-example.toml` | 新增 [ack] 配置段 |
| `internal/comet/grpc/server.go` | PushMsg 成功后 POST Gateway /goim/internal/ack |
| `internal/comet/conf/conf.go` | Config 新增 Gateway 地址 |
| `cmd/comet/comet-example.toml` | 新增 [gateway] 配置段 |
| `internal/logic/push.go` | PushMids 中不在线的 mid POST Gateway /goim/internal/undelivered |
| `internal/logic/conn.go` | Disconnect 新增 POST Gateway /goim/internal/offline |
| `internal/logic/conf/conf.go` | Config 新增 Gateway 地址 |
| `cmd/logic/logic-example.toml` | 新增 [gateway] 配置段 |

**goim 改动范围**：Comet 2 文件 + Logic 3 文件 + 2 配置文件，均为追加式改动

## 实现顺序

1. `internal/gateway/model/user.go` — User 模型加字段 ✅
2. `internal/gateway/dao/user.go` — 新增 DAO 方法 + 测试 ✅
3. `internal/gateway/dao/message.go` — 新增 ListOfflineGroupMessages + 测试 ✅
4. `internal/gateway/conf/conf.go` — 新增 ACK 配置 ✅
5. `cmd/gateway/gateway-example.toml` — 新增配置段 ✅
6. `internal/gateway/ack.go` — ACK 服务 + 测试 ✅
7. `internal/gateway/sync.go` — 离线同步逻辑 + 测试 ✅
8. `internal/gateway/gateway.go` — 集成 ackService ✅
9. `internal/gateway/chat.go` — SendMessage 后 Track ✅
10. `internal/gateway/auth.go` — Login 响应加 LastAckAt，Register 初始化 LastAckAt ✅
11. `internal/gateway/http/internal.go` — 内部回调 handler ✅
12. `internal/gateway/http/sync.go` — Pull + ACK handler ✅
13. `internal/gateway/http/server.go` — 注册新路由 ✅
14. `internal/comet/conf/conf.go` — Config 加 Gateway 地址
15. `cmd/comet/comet-example.toml` — 加 [gateway] 配置
16. `internal/comet/grpc/server.go` — PushMsg 成功后回调 Gateway
17. `internal/logic/conf/conf.go` — Config 加 Gateway 地址
18. `cmd/logic/logic-example.toml` — 加 [gateway] 配置
19. `internal/logic/push.go` — PushMids 不在线通知 Gateway
20. `internal/logic/conn.go` — Disconnect 通知 Gateway
21. `examples/offline-demo/main.go` — 端到端 demo

## 验证方式

```bash
# 前置：启动 goim 全套 + gateway
# comet-example.toml 配置 [gateway] addr
# logic-example.toml 配置 [gateway] addr
# gateway-example.toml 配置 [ack] 段

# === 单聊 ACK 测试 ===

# 1. Alice 发消息给在线的 Bob
#    → Gateway push → Comet 推送成功 → POST /goim/internal/ack
#    → pending 清除 ✓

# 2. Alice 发消息给离线的 Bob
#    → Gateway push → Logic 查不到 Bob → POST /goim/internal/undelivered
#    → pending 清除 ✓（消息在 DB）

# 3. Alice 发消息，Comet signal 满导致丢失
#    → 无 ACK 回调 → 5s 超时 → Gateway 重推 → 最多 3 次

# === 离线 Pull 测试 ===

# 4. Bob 离线期间 Alice 发了 3 条消息
# 5. Bob 重连后拉取离线消息
curl "http://localhost:3200/goim/sync?since=0&limit=200" \
  -H "Authorization: Bearer $TOKEN_B"
# → 返回 3 条单聊消息

# 6. Bob ACK
curl -X POST http://localhost:3200/goim/sync/ack \
  -H "Authorization: Bearer $TOKEN_B" \
  -d '{"ack_at": 1710000000000}'

# 7. 再次 Pull → 返回空（已 ACK 过的不再返回）

# === 群聊 Pull 兜底测试 ===

# 8. Bob 离线期间群里有新消息
# 9. Bob 重连后 GET /goim/sync → group_messages 包含离线群消息
```

## 未来扩展项

- **送达状态异步通知**：ACK 成功/失败后，反向推送给发送方（op=2004），类似微信"已送达"
- **已读回执**：接收方阅读后通知发送方
- **群聊 ACK**：如果群规模小（<50人），可考虑逐成员 ACK
