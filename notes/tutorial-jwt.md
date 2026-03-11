# JWT 入门指南（GoIM 项目）

## 什么是 JWT

JWT（JSON Web Token）是一种**自包含的令牌格式**，用于在两个系统之间安全地传递信息。

传统的鉴权方式是 Session：用户登录后，==服务端生成一个 session ID 存在内存/Redis 里，每次请求都带上这个 ID，服务端去查表验证==。

JWT 的思路不同：==**把用户信息直接编码在令牌里，用签名防篡改**。服务端不需要存任何东西==，只要能验签就行。

```
传统 Session:
  Client ──cookie: sid=abc123──→ Server ──→ 查 Redis: abc123 → {uid: 1} ──→ 通过

JWT:
  Client ──token: eyJ...──→ Server ──→ 验证签名 + 解码 → {mid: 1} ──→ 通过
                            （不需要查数据库）
```

---

## JWT 的结构

一个 JWT 由三部分组成，用 `.` 分隔：

```
eyJhbGciOiJIUzI1NiJ9.eyJtaWQiOjEsImV4cCI6MTcxMH0.K8s2f3...
├── Header ──────────┤├── Payload ─────────────────┤├── Signature ┤
```

### 1. Header（头部）

声明签名算法：

```json
{"alg": "HS256", "typ": "JWT"}
```

`HS256` = HMAC-SHA256，对称加密 — 签发和验证用同一个密钥。

### 2. Payload（载荷）

实际携带的数据。在本项目中：

```json
{
  "mid": 1,
  "exp": 1710000000,
  "iat": 1709913600
}
```

| 字段 | 含义 | 来源 |
|------|------|------|
| `mid` | 用户 ID | 自定义字段，登录时从 MySQL 查到 |
| `exp` | 过期时间（Unix 时间戳） | JWT 标准字段（RegisteredClaims） |
| `iat` | 签发时间 | JWT 标准字段 |

Header 和 Payload 都只做 **Base64URL 编码**，**不是加密** — 任何人都能解码看到内容。安全性靠第三部分保证。

### 3. Signature（签名）

```
HMAC-SHA256(base64(header) + "." + base64(payload), secret)
```

用服务端的 `secret` 密钥对前两部分计算哈希。作用：

- 如果有人改了 payload（比如把 `mid` 从 1 改成 2），签名就对不上，验证失败
- 不知道 `secret` 就无法伪造合法签名
- 所以 **secret 绝对不能泄露**

---

## 为什么适合 GoIM

GoIM 的特点是多服务：

```
API Service ──生成 JWT──→ Client ──携带 JWT──→ Comet ──转发──→ Logic 验证
```

JWT 的优势在这里体现：

1. **API 和 Logic 是不同的服务进程**，不共享内存。如果用 Session，Logic 需要访问 API 的 Session 存储（Redis），增加耦合。
2. 用 JWT，**只需共享一个 secret 字符串**，Logic 就能独立验证令牌，不需要调用 API 服务或查数据库。
3. 多个 Logic 实例部署时，每个实例都配置同一个 secret，天然支持分布式。

---

## Go 中怎么用

使用 `github.com/golang-jwt/jwt/v4` 库。核心就两步：**签发** 和 **验证**。

### 签发（生成 token）

```go
import "github.com/golang-jwt/jwt/v4"

// 1. 定义 Claims（载荷结构）
type Claims struct {
    Mid int64 `json:"mid"`
    jwt.RegisteredClaims          // 内嵌标准字段：exp, iat 等
}

// 2. 填充 Claims
claims := Claims{
    Mid: 1,
    RegisteredClaims: jwt.RegisteredClaims{
        ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
        IssuedAt:  jwt.NewNumericDate(time.Now()),
    },
}

// 3. 创建 token 对象，指定签名算法
token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

// 4. 用 secret 签名，得到 token 字符串
tokenStr, err := token.SignedString([]byte("my-secret"))
// tokenStr = "eyJhbGciOiJIUzI1NiJ9.eyJtaWQiOjEsImV4cCI6..."
```

### 验证（解析 token）

```go
// 1. 解析 + 验签一步完成
token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
    return []byte("my-secret"), nil   // 提供 secret 用于验签
})

// 2. err != nil 的情况：
//    - 签名不匹配（被篡改）
//    - token 已过期（exp < now）
//    - 格式错误
if err != nil {
    // 拒绝请求
}

// 3. 提取 Claims
claims := token.Claims.(*Claims)
fmt.Println(claims.Mid) // 1
```

`ParseWithClaims` 内部自动做了：
- Base64 解码 Header + Payload
- 用 secret 重新计算签名，和 token 中的签名比对
- 检查 `exp` 是否过期

---

## pkg/auth/jwt.go 代码解读

这是本项目的共享 JWT 工具包，API 服务和 Logic 服务都引用它。

```go
package auth

import (
    "time"
    "github.com/golang-jwt/jwt/v4"
)

// Claims — JWT 的载荷
// 只放 Mid（用户 ID），保持简洁。
// room_id、platform、accepts 是连接级参数，不放进 JWT。
type Claims struct {
    Mid int64 `json:"mid"`
    jwt.RegisteredClaims           // 提供 exp, iat 等标准字段
}
```

**为什么 Claims 只有 Mid？**

`room_id` 和 `accepts` 是「每次连接」可能不同的值（用户可能先加入房间 A，断开后加入房间 B），不适合固化在 JWT 里。JWT 代表的是「你是谁」，不是「你要干什么」。

```go
// GenerateToken — API 服务登录成功后调用
// 输入：secret 密钥、用户 ID、过期小时数
// 输出：签名后的 token 字符串
func GenerateToken(secret string, mid int64, expireHours int) (string, error) {
    claims := Claims{
        Mid: mid,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(
                time.Duration(expireHours) * time.Hour)),
            IssuedAt: jwt.NewNumericDate(time.Now()),
        },
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(secret))
}
```

```go
// ParseToken — Logic 服务验证连接时调用
// 输入：secret 密钥、客户端传来的 token 字符串
// 输出：解析后的 Claims（包含 mid），或错误（过期/篡改/格式错误）
func ParseToken(secret string, tokenStr string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenStr, &Claims{},
        func(token *jwt.Token) (interface{}, error) {
            return []byte(secret), nil
        })
    if err != nil {
        return nil, err
    }
    return token.Claims.(*Claims), nil
}
```

---

## 在 GoIM 中的使用流程

### 登录阶段（API 服务）

```
Client                          API Service
  │                                │
  │── POST /api/login ───────────→│
  │   {"username":"test",          │ 1. 查 MySQL，验证密码
  │    "password":"123456"}        │ 2. auth.GenerateToken(secret, user.ID, 24)
  │                                │ 3. 返回 JWT
  │←── {"token":"eyJ..."} ────────│
```

### 连接阶段（Comet + Logic）

```
Client                    Comet                     Logic
  │                         │                         │
  │── OpAuth ──────────────→│                         │
  │   body: {               │── gRPC Connect() ─────→│
  │     "token":"eyJ...",   │   token = body bytes    │ 1. JSON 解析外层
  │     "room_id":"test://1"│                         │ 2. auth.ParseToken(secret, token)
  │     "platform":"web",   │                         │    → 验签 + 提取 mid
  │     "accepts":[1000]    │                         │ 3. UUID 生成 key
  │   }                     │                         │ 4. Redis AddMapping(mid, key, server)
  │                         │←── ConnectReply ────────│
  │←── OpAuthReply ─────────│     {mid, key, roomID}  │
```

### 关键点

| 问题 | 答案 |
|------|------|
| secret 放在哪？ | API 的 `api.toml` 和 Logic 的 `logic.toml` 中，值必须相同 |
| token 过期了怎么办？ | 连接会被 Logic 拒绝，客户端需要重新登录获取新 token |
| 已连接的用户 token 过期？ | 不影响。JWT 只在连接建立时验证一次，之后靠心跳维持 |
| 能不能伪造 mid？ | 不能。mid 在 JWT payload 里，改了签名就对不上 |
| 为什么不用 RSA？ | HS256（对称）足够。RSA 适合签发方和验证方是不同组织的场景 |

---

## 常见误区

1. **JWT 不是加密**。Payload 是 Base64 编码，任何人都能解码看到 `mid`。JWT 保证的是**完整性**（不被篡改），不是**机密性**。所以不要在 JWT 里放密码。

2. **JWT 无法主动失效**。签发后，在过期之前一直有效，服务端无法"撤销"。如果需要强制下线，需要额外机制（如 Redis 黑名单）。对 IM 场景来说，直接删 Redis 中的连接映射即可踢人。

3. **Secret 泄露 = 全部令牌可伪造**。Secret 不能写在代码里、不能提交到 Git、不能出现在日志中。
