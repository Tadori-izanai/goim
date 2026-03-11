# 用户系统与 JWT 鉴权实现计划

## Context

当前 goim 的 `Connect()` 认证是**无校验的** — 客户端发什么 JSON 就信什么（`mid`、`key` 都由客户端指定）。需要加入真正的用户系统和 JWT 鉴权，使 `mid` 来自服务端验证而非客户端自报。

## 整体架构

```
Client                     API Service (:3200)             MySQL
  │── POST /api/register ──→ 创建用户 ──────────────────→ users 表
  │── POST /api/login ────→ 验证密码 → 生成 JWT ─────→ 返回 token + comet 地址
  │                          │
  │                          │── GET Logic /goim/nodes/weighted
  │                          │   (API 内部调用，获取 comet 节点列表)
  │
  │  拿到 JWT + comet 地址后:
  │── WebSocket/TCP ──→ Comet ── gRPC ──→ Logic.Connect()
  │   (发送 OpAuth)                        │ 验证 JWT
  │                                        │ 提取 mid
  │                                        │ Redis AddMapping
  │←─ OpAuthReply ─────────────────────────┘
```

## API 与 Logic 的通信

API 服务需要调用 Logic 的 HTTP 接口（获取节点列表、后续发消息等）。
采用**配置直连**方式：在 API 配置中写 Logic HTTP 地址（如 `logicAddr = "http://localhost:3111"`）。

- 单机部署：直连即可
- 多机部署：Logic 前面挂 Nginx/LB，API 配置指向 LB 地址
- 不使用 discovery：避免引入额外复杂度，Logic 本身无状态

## 新增文件结构

```
pkg/auth/
    jwt.go                          # 共享 JWT 工具（API 生成 + Logic 验证）

cmd/api/
    main.go                         # 入口
    api-example.toml                # 配置

internal/api/
    conf/conf.go                    # 配置结构
    model/user.go                   # User GORM 模型
    dao/dao.go                      # MySQL + Redis 初始化
    dao/user.go                     # 用户 CRUD
    service/service.go              # Service 结构
    service/auth.go                 # 注册/登录业务逻辑
    http/server.go                  # Gin 路由
    http/auth.go                    # Handler: register, login
    http/middleware.go              # 复用 logic 的 logger/recover
    http/result.go                  # 复用 logic 的响应格式
```

## 修改文件

| 文件 | 改动 |
|------|------|
| `internal/logic/conf/conf.go` | Config 加 `JWTSecret string` |
| `cmd/logic/logic-example.toml` | 加 `jwtSecret = "..."` |
| `internal/logic/conn.go` | `Connect()` 改为 JWT 验证 |
| `Makefile` | 加 api 构建/运行目标 |

## 实现细节

### 1. pkg/auth/jwt.go — 共享 JWT 工具

依赖 `github.com/golang-jwt/jwt/v4`

```go
type Claims struct {
    Mid      int64   `json:"mid"`
    RoomID   string  `json:"room_id,omitempty"`
    Platform string  `json:"platform,omitempty"`
    Accepts  []int32 `json:"accepts,omitempty"`
    jwt.RegisteredClaims
}

func GenerateToken(secret string, mid int64, expireHours int) (string, error)
func ParseToken(secret string, tokenStr string) (*Claims, error)
```

### 2. 数据库 — users 表

```go
// internal/api/model/user.go
type User struct {
    ID        int64  `gorm:"primaryKey;autoIncrement"`
    Username  string `gorm:"uniqueIndex;size:64;not null"`
    Password  string `gorm:"size:255;not null"` // bcrypt hash
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

GORM AutoMigrate 自动建表。

### 3. API 配置 — cmd/api/api-example.toml

```toml
[httpServer]
    addr = ":3200"

[mysql]
    dsn = "root:password@tcp(127.0.0.1:3306)/goim?charset=utf8mb4&parseTime=True&loc=Local"

[jwt]
    secret = "your-jwt-secret-change-me"
    expireHours = 24

[logic]
    addr = "http://localhost:3111"
```

初期不需要 Redis，配置预留但不初始化。

### 4. API 接口

| 接口 | 请求 | 响应 |
|------|------|------|
| `POST /api/register` | `{"username":"xxx","password":"xxx"}` | `{"code":0}` |
| `POST /api/login` | `{"username":"xxx","password":"xxx"}` | `{"code":0,"data":{"token":"eyJ...","nodes":["10.0.0.1"],"ws_port":3102,"heartbeat":240}}` |

注册：bcrypt 加密密码 → 存 MySQL
登录：查用户 → bcrypt 比对 → 生成 JWT → 调 Logic `/goim/nodes/weighted` 获取 comet 节点 → 一起返回

### 5. Logic Connect() 改造

客户端 auth body 从原来的：
```json
{"mid": 123, "key": "", "room_id": "test://1", "platform": "web", "accepts": [1000]}
```

改为：
```json
{"token": "eyJ...", "room_id": "test://1", "platform": "web", "accepts": [1000]}
```

Logic `Connect()` 改为：
1. JSON 解析外层 → 拿到 `token` 字符串 + `room_id`/`platform`/`accepts`
2. `auth.ParseToken(secret, token)` → 验证 JWT、提取 `mid`
3. `key` 始终服务端生成（UUID），防止客户端伪造
4. 其余逻辑不变（AddMapping 等）

参考文件：`internal/logic/conn.go`（当前实现），已用 `github.com/google/uuid`。

### 6. Makefile

```makefile
# build 目标追加：
cp cmd/api/api-example.toml target/api.toml
$(GOBUILD) -o target/api cmd/api/main.go

# 新增 api 单独运行目标：
api:
	target/api -conf=target/api.toml -alsologtostderr 2>&1 | tee target/api.log
```

## 实现顺序

1. `pkg/auth/jwt.go` — 无外部依赖，先写好
2. `go get` 新依赖：`golang-jwt/jwt/v4`、`gorm.io/gorm`、`gorm.io/driver/mysql`
3. `internal/api/model/user.go`
4. `internal/api/conf/conf.go`
5. `internal/api/dao/dao.go` + `dao/user.go`
6. `internal/api/service/service.go` + `service/auth.go`
7. `internal/api/http/` (server, auth, middleware, result)
8. `cmd/api/api-example.toml` + `cmd/api/main.go`
9. `internal/logic/conf/conf.go` — 加 JWTSecret
10. `cmd/logic/logic-example.toml` — 加 jwtSecret
11. `internal/logic/conn.go` — Connect() JWT 验证
12. `Makefile` 更新

## 验证方式

```bash
# 1. 启动 MySQL、确保 goim 全套运行
# 2. 启动 API 服务
make api

# 3. 注册用户
curl -X POST http://localhost:3200/api/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"test","password":"123456"}'

# 4. 登录获取 token
curl -X POST http://localhost:3200/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"test","password":"123456"}'
# 返回: {"code":0,"data":{"token":"eyJ..."}}

# 5. 用 token 连接 WebSocket，发送 OpAuth：
# body: {"token":"eyJ...","room_id":"test://1","platform":"web","accepts":[1000]}
# 预期: 收到 OpAuthReply，连接建立成功

# 6. 用错误/过期 token 连接 → 预期被拒绝
```
