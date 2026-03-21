# Docker 化部署指南（GoIM 项目）

## 它是什么

Docker 把你的应用和它的依赖打包成一个**镜像（Image）**，然后从镜像启动**容器（Container）**。容器是隔离的进程，自带文件系统，不依赖宿主机环境。

```
你的笔记本（macOS）
├── Docker Engine
│   ├── 容器: mysql        (端口 3306)
│   ├── 容器: redis        (端口 6379)
│   ├── 容器: nats         (端口 4222)
│   ├── 容器: discovery    (端口 7171)
│   ├── 容器: comet        (端口 3101, 3102, 3109)
│   ├── 容器: logic        (端口 3111, 3119)
│   ├── 容器: job
│   ├── 容器: gateway      (端口 3200)
│   └── 容器: prometheus   (端口 9090)
```

---

## 核心概念

| 概念 | 类比 | 说明 |
|------|------|------|
| **Image（镜像）** | 安装光盘 | 只读模板，包含代码 + 依赖 + 运行环境 |
| **Container（容器）** | 运行中的虚拟机（但更轻量） | 从镜像启动的进程，可以停止/删除/重建 |
| **Dockerfile** | 安装脚本 | 描述如何构建镜像的文本文件 |
| **docker-compose** | 批量启动脚本 | 一个 YAML 文件定义多个容器，一键启动整套服务 |
| **Volume（卷）** | 外挂硬盘 | 容器删除后数据不丢失（如 MySQL 数据） |
| **Network（网络）** | 局域网 | 同一网络内的容器可以用**服务名**互相访问 |

### 关键理解：容器间的网络

你之前本地开发时，所有服务都跑在 `localhost`，所以配置文件里写 `127.0.0.1:6379`。

Docker 化后，每个容器有自己的网络空间。`127.0.0.1` 指的是容器自己，不是别的容器。

**docker-compose 的解决方案**：同一个 `docker-compose.yml` 里的容器自动在同一个网络中，可以用**服务名**当主机名：

```
# 本地开发                    # Docker 化
127.0.0.1:6379       →       redis:6379
127.0.0.1:3306       →       mysql:3306
127.0.0.1:4222       →       nats:4222
127.0.0.1:7171       →       discovery:7171
```

---

## 第一步：Dockerfile（构建 Go 服务镜像）

Go 服务用**多阶段构建（Multi-stage Build）**：第一阶段编译，第二阶段只拷贝二进制文件，最终镜像很小。

在项目根目录创建 `Dockerfile`：

```dockerfile
# ── 阶段 1：编译 ──
FROM golang:1.24-alpine AS builder

# 安装 git（部分依赖需要）
RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# 编译四个服务
RUN CGO_ENABLED=0 go build -o /bin/comet   cmd/comet/main.go
RUN CGO_ENABLED=0 go build -o /bin/logic   cmd/logic/main.go
RUN CGO_ENABLED=0 go build -o /bin/job     cmd/job/main.go
RUN CGO_ENABLED=0 go build -o /bin/gateway cmd/gateway/main.go

# ── 阶段 2：运行 ──
# 每个服务单独一个 target，共享编译阶段

FROM alpine:3.20 AS comet
COPY --from=builder /bin/comet /bin/comet
ENTRYPOINT ["/bin/comet"]

FROM alpine:3.20 AS logic
COPY --from=builder /bin/logic /bin/logic
ENTRYPOINT ["/bin/logic"]

FROM alpine:3.20 AS job
COPY --from=builder /bin/job /bin/job
ENTRYPOINT ["/bin/job"]

FROM alpine:3.20 AS gateway
COPY --from=builder /bin/gateway /bin/gateway
ENTRYPOINT ["/bin/gateway"]
```

### 解读

```
FROM golang:1.24-alpine AS builder    ← 用 Go 1.24 镜像作为编译环境
COPY go.mod go.sum ./                 ← 先拷贝依赖文件（利用 Docker 缓存）
RUN go mod download                   ← 下载依赖（只要 go.mod 没变就走缓存）
COPY . .                              ← 拷贝全部源码
RUN CGO_ENABLED=0 go build ...        ← 静态编译，不依赖 C 库

FROM alpine:3.20 AS comet             ← 最终镜像只有 ~10MB 的 Alpine Linux
COPY --from=builder /bin/comet ...    ← 从编译阶段拷贝二进制
ENTRYPOINT ["/bin/comet"]             ← 容器启动时执行的命令
```

`CGO_ENABLED=0` 确保纯静态编译，这样可以在最小的 Alpine 镜像上运行。

### 构建命令

```bash
# 构建单个服务（--target 指定阶段）
docker build --target comet   -t goim-comet   .
docker build --target logic   -t goim-logic   .
docker build --target job     -t goim-job     .
docker build --target gateway -t goim-gateway .
```

但实际上你不需要手动执行这些，docker-compose 会自动构建。

---

## 第二步：Discovery 镜像

bilibili/discovery 没有官方 Docker 镜像，需要自己构建。

创建 `Dockerfile.discovery`：

```dockerfile
FROM golang:1.24-alpine AS builder
RUN apk add --no-cache git
RUN git clone https://github.com/bilibili/discovery.git /src
WORKDIR /src
# discovery 用的是旧版 Go modules，需要处理
RUN GO111MODULE=on go build -o /bin/discovery cmd/discovery/main.go

FROM alpine:3.20
COPY --from=builder /bin/discovery /bin/discovery
EXPOSE 7171
ENTRYPOINT ["/bin/discovery"]
CMD ["-conf", "/etc/discovery/discovery-example.toml"]
```

> 如果 discovery 编译有问题，也可以在本机编译好后用更简单的 Dockerfile：
> ```dockerfile
> FROM alpine:3.20
> COPY target/discovery /bin/discovery
> ENTRYPOINT ["/bin/discovery"]
> ```

---

## 第三步：Docker 化配置文件

容器间用服务名通信，所以需要一套 Docker 专用配置。

创建 `docker/` 目录存放：

```
docker/
├── comet.toml
├── logic.toml
├── job.toml
├── gateway.toml
└── prometheus.yml
```

### docker/comet.toml

和本地配置的区别：`discovery.nodes` 从 `127.0.0.1` 改为 `discovery`（服务名）。

```toml
[discovery]
    nodes = ["discovery:7171"]

[rpcServer]
    addr = ":3109"
    timeout = "1s"

[rpcClient]
    dial = "1s"
    timeout = "1s"

[tcp]
    bind = [":3101"]
    sndbuf = 4096
    rcvbuf = 4096
    keepalive = false
    reader = 32
    readBuf = 1024
    readBufSize = 8192
    writer = 32
    writeBuf = 1024
    writeBufSize = 8192

[websocket]
    bind = [":3102"]
    tlsOpen = false
    tlsBind = [":3103"]
    certFile = "../../cert.pem"
    privateFile = "../../private.pem"

[protocol]
    timer = 32
    timerSize = 2048
    svrProto = 10
    cliProto = 5
    handshakeTimeout = "8s"

[whitelist]
    Whitelist = [123]
    WhiteLog  = "/tmp/white_list.log"

[bucket]
    size = 32
    channel = 1024
    room = 1024
    routineAmount = 32
    routineSize = 1024

[jwt]
    secret = "your-jwt-secret-change-me"
    expireHours = 24
```

### docker/logic.toml

改动点：`discovery`、`redis`、`nats`、`kafka` 的地址全部改为服务名。新增 `gateway` 地址。

```toml
MQType = "nats"

[discovery]
    nodes = ["discovery:7171"]

[regions]
    "bj" = ["北京","天津","河北","山东","山西","内蒙古","辽宁","吉林","黑龙江","甘肃","宁夏","新疆"]
    "sh" = ["上海","江苏","浙江","安徽","江西","湖北","重庆","陕西","青海","河南","台湾"]
    "gz" = ["广东","福建","广西","海南","湖南","四川","贵州","云南","西藏","香港","澳门"]

[node]
    defaultDomain = "conn.goim.io"
    hostDomain = ".goim.io"
    heartbeat = "4m"
    heartbeatMax = 2
    tcpPort = 3101
    wsPort = 3102
    wssPort = 3103
    regionWeight = 1.6

[backoff]
    maxDelay = 300
    baseDelay = 3
    factor = 1.8
    jitter = 0.3

[rpcServer]
    network = "tcp"
    addr = ":3119"
    timeout = "1s"

[rpcClient]
    dial = "1s"
    timeout = "1s"

[httpServer]
    network = "tcp"
    addr = ":3111"
    readTimeout = "1s"
    writeTimeout = "1s"

[kafka]
    topic = "goim-push-topic"
    brokers = ["kafka:9092"]

[Nats]
    Subject = "goim-push"
    Addr = "nats://nats:4222"

[redis]
    network = "tcp"
    addr = "redis:6379"
    active = 60000
    idle = 1024
    dialTimeout = "200ms"
    readTimeout = "500ms"
    writeTimeout = "500ms"
    idleTimeout = "120s"
    expire = "30m"

[gateway]
    addr = "http://gateway:3200"
```

### docker/job.toml

```toml
MQType = "nats"

[discovery]
    nodes = ["discovery:7171"]

[kafka]
    topic = "goim-push-topic"
    group = "goim-push-group-job"
    brokers = ["kafka:9092"]

[Nats]
    Subject = "goim-push"
    Addr = "nats://nats:4222"
```

### docker/gateway.toml

```toml
[httpServer]
    addr = ":3200"

[mysql]
    dsn = "root:password@tcp(mysql:3306)/goim?charset=utf8mb4&parseTime=True&loc=Local"

[jwt]
    secret = "your-jwt-secret-change-me"
    expireHours = 24

[logic]
    addr = "http://logic:3111"

[ack]
    retryInterval = 5
    maxRetries = 3
```

### docker/prometheus.yml

```yaml
global:
  scrape_interval: 5s

scrape_configs:
  - job_name: 'goim-comet'
    static_configs:
      - targets: ['comet:9100']

  - job_name: 'goim-job'
    static_configs:
      - targets: ['job:9101']
```

### 配置对比总结

| 配置项 | 本地 | Docker |
|--------|------|--------|
| Discovery | `127.0.0.1:7171` | `discovery:7171` |
| Redis | `127.0.0.1:6379` | `redis:6379` |
| MySQL | `127.0.0.1:3306` | `mysql:3306` |
| NATS | `localhost:4222` | `nats:4222` |
| Kafka | `127.0.0.1:9092` | `kafka:9092` |
| Logic HTTP | `localhost:3111` | `logic:3111` |
| Gateway | `localhost:3200` | `gateway:3200` |

规律：**把 `127.0.0.1` / `localhost` 替换为 docker-compose 中的服务名**。

---

## 第四步：docker-compose.yml

这是核心文件。在项目根目录创建 `docker-compose.yml`：

```yaml
services:

  # ── 中间件 ──

  mysql:
    image: mysql:latest
    ports:
      - "3306:3306"
    environment:
      MYSQL_ROOT_PASSWORD: password
      MYSQL_DATABASE: goim
    volumes:
      - mysql-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 5s
      timeout: 3s
      retries: 10

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 3s
      timeout: 2s
      retries: 5

  nats:
    image: nats:latest
    ports:
      - "4222:4222"
      - "6222:6222"
      - "8222:8222"
    command: ["--jetstream"]

  # 如果你想用 Kafka 而不是 NATS，取消下面的注释
  # zookeeper:
  #   image: zookeeper:latest
  #   ports:
  #     - "2181:2181"
  #
  # kafka:
  #   image: wurstmeister/kafka:latest
  #   ports:
  #     - "9092:9092"
  #   environment:
  #     KAFKA_ZOOKEEPER_CONNECT: zookeeper:2181
  #     KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
  #     KAFKA_LISTENERS: PLAINTEXT://0.0.0.0:9092
  #   depends_on:
  #     - zookeeper

  discovery:
    build:
      context: .
      dockerfile: Dockerfile.discovery
    ports:
      - "7171:7171"

  # ── GoIM 服务 ──

  comet:
    build:
      context: .
      dockerfile: Dockerfile
      target: comet
    ports:
      - "3101:3101"
      - "3102:3102"
      - "3109:3109"
      - "9100:9100"
    volumes:
      - ./docker/comet.toml:/etc/goim/comet.toml
    command: [
      "-conf", "/etc/goim/comet.toml",
      "-region", "sh",
      "-zone", "sh001",
      "-deploy.env", "dev",
      "-weight", "10",
      "-addrs", "comet",
      "-debug", "true",
      "-alsologtostderr"
    ]
    depends_on:
      - discovery
      - nats

  logic:
    build:
      context: .
      dockerfile: Dockerfile
      target: logic
    ports:
      - "3111:3111"
      - "3119:3119"
    volumes:
      - ./docker/logic.toml:/etc/goim/logic.toml
    command: [
      "-conf", "/etc/goim/logic.toml",
      "-region", "sh",
      "-zone", "sh001",
      "-deploy.env", "dev",
      "-weight", "10",
      "-alsologtostderr"
    ]
    depends_on:
      - discovery
      - redis
      - nats

  job:
    build:
      context: .
      dockerfile: Dockerfile
      target: job
    ports:
      - "9101:9101"
    volumes:
      - ./docker/job.toml:/etc/goim/job.toml
    command: [
      "-conf", "/etc/goim/job.toml",
      "-region", "sh",
      "-zone", "sh001",
      "-deploy.env", "dev",
      "-alsologtostderr"
    ]
    depends_on:
      - discovery
      - nats

  gateway:
    build:
      context: .
      dockerfile: Dockerfile
      target: gateway
    ports:
      - "3200:3200"
    volumes:
      - ./docker/gateway.toml:/etc/goim/gateway.toml
    command: ["-conf", "/etc/goim/gateway.toml", "-alsologtostderr"]
    depends_on:
      mysql:
        condition: service_healthy
      logic:
        condition: service_started

  # ── 监控 ──

  prometheus:
    image: prom/prometheus
    ports:
      - "9090:9090"
    volumes:
      - ./docker/prometheus.yml:/etc/prometheus/prometheus.yml

volumes:
  mysql-data:
```

### 解读关键字段

```yaml
services:
  mysql:
    image: mysql:latest          # 使用官方镜像，不需要自己构建
    ports:
      - "3306:3306"              # 宿主机端口:容器端口（方便本机调试）
    environment:                 # 环境变量（MySQL 初始化用）
      MYSQL_ROOT_PASSWORD: password
      MYSQL_DATABASE: goim
    volumes:
      - mysql-data:/var/lib/mysql  # 命名卷，容器删除后数据不丢
    healthcheck:                 # 健康检查，其他服务可以等它就绪
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]

  comet:
    build:
      context: .                 # 构建上下文（Dockerfile 所在目录）
      dockerfile: Dockerfile
      target: comet              # 多阶段构建中选择 comet 阶段
    volumes:
      - ./docker/comet.toml:/etc/goim/comet.toml  # 挂载配置文件
    command: ["-conf", "/etc/goim/comet.toml", ...]  # 覆盖 ENTRYPOINT 的参数
    depends_on:                  # 启动顺序（不保证就绪，只保证启动）
      - discovery
      - nats

  gateway:
    depends_on:
      mysql:
        condition: service_healthy  # 等 MySQL 健康检查通过后再启动
```

### depends_on vs healthcheck

`depends_on` 只控制**启动顺序**，不等服务就绪。比如 MySQL 容器启动了但还在初始化，Gateway 连接会失败。

解决方案：给 MySQL 加 `healthcheck`，Gateway 用 `condition: service_healthy` 等它真正就绪。

---

## 第五步：常用命令

```bash
# ── 构建 + 启动 ──

docker compose up -d --build
#   up        = 创建并启动容器
#   -d        = 后台运行（detach）
#   --build   = 强制重新构建镜像（代码改了之后要加这个）

# ── 查看状态 ──

docker compose ps                    # 查看所有容器状态
docker compose logs -f comet         # 实时查看 comet 日志（-f = follow）
docker compose logs -f --tail=50     # 查看所有服务最近 50 行日志

# ── 停止 / 清理 ──

docker compose stop                  # 停止所有容器（保留数据）
docker compose down                  # 停止并删除容器（保留 volume）
docker compose down -v               # 停止并删除容器 + volume（清空 MySQL 数据）

# ── 单个服务操作 ──

docker compose restart comet         # 重启单个服务
docker compose up -d --build gateway # 只重新构建并启动 gateway
docker compose exec mysql mysql -uroot -ppassword goim  # 进入 MySQL 命令行

# ── 调试 ──

docker compose exec comet sh         # 进入 comet 容器的 shell
docker compose logs comet 2>&1 | grep "error"  # 搜索错误日志
```

### 命令速查

| 场景 | 命令 |
|------|------|
| 第一次启动 | `docker compose up -d --build` |
| 改了 Go 代码 | `docker compose up -d --build` |
| 改了配置文件 | `docker compose restart <服务名>` |
| 查看日志 | `docker compose logs -f <服务名>` |
| 全部停止 | `docker compose down` |
| 清空重来 | `docker compose down -v && docker compose up -d --build` |
| 进 MySQL | `docker compose exec mysql mysql -uroot -ppassword goim` |

---

## 第六步：启动顺序与验证

### 启动

```bash
cd /path/to/goim
docker compose up -d --build
```

首次构建会比较慢（下载 Go 依赖），后续构建利用缓存会快很多。

### 验证各服务

```bash
# 1. 检查所有容器是否 running
docker compose ps

# 2. MySQL 就绪
docker compose exec mysql mysql -uroot -ppassword -e "SHOW DATABASES;"
# 应该看到 goim 数据库

# 3. Redis 就绪
docker compose exec redis redis-cli ping
# 应该返回 PONG

# 4. NATS 就绪
curl http://localhost:8222/varz
# 应该返回 JSON 状态信息

# 5. Discovery 就绪
curl http://localhost:7171/discovery/polls
# 应该返回 JSON

# 6. Gateway 就绪
curl http://localhost:3200/goim/auth/login
# 应该返回 JSON（即使是错误响应也说明服务在运行）

# 7. Prometheus 就绪
open http://localhost:9090
# 浏览器打开 Prometheus UI

# 8. 端到端测试
go run examples/offline-demo/main.go -gateway http://localhost:3200
```

### 常见问题排查

**容器启动后立刻退出**：
```bash
docker compose logs <服务名>    # 看错误日志
```

**Gateway 连不上 MySQL**：

```bash
# MySQL 可能还没就绪，等几秒再试
docker compose logs mysql       # 看 MySQL 是否初始化完成
docker compose restart gateway  # 重启 gateway
```

**Comet/Logic 注册 Discovery 失败**：
```bash
docker compose logs discovery   # 看 discovery 是否正常
docker compose restart comet logic job  # 重启 goim 服务
```

**端口冲突**：
```bash
# 如果本机已经跑了 MySQL/Redis，端口会冲突
# 方案 1：停掉本机的服务
brew services stop mysql
brew services stop redis

# 方案 2：改 docker-compose.yml 的宿主机端口
ports:
  - "13306:3306"   # 宿主机用 13306，容器内还是 3306
```

---

## 第七步：Comet 的 -addrs 参数

Comet 启动时的 `-addrs` 参数决定了客户端 WebSocket 连接的地址。

- 本地开发：`-addrs=127.0.0.1`，客户端连 `ws://127.0.0.1:3102/sub`
- Docker 内部：`-addrs=comet`，其他容器连 `ws://comet:3102/sub`
- 外部访问：客户端从宿主机连 `ws://127.0.0.1:3102/sub`（端口已映射）

docker-compose 中设置 `-addrs=comet`，Logic 注册到 Discovery 的地址就是 `comet`。Gateway 调 Logic 的 `/goim/nodes/weighted` 返回的节点地址也是 `comet`。

如果你的客户端（如 offline-demo）跑在宿主机上，需要把返回的 `comet` 解析为 `127.0.0.1`。最简单的方式是在 `/etc/hosts` 加一行：

```
127.0.0.1  comet
```

或者在 demo 代码里硬编码 `host = "127.0.0.1"`（现有 demo 已经这样做了）。

---

## 整体架构图

```
                    ┌─────────────────────────────────────────────┐
                    │              Docker Network                  │
                    │                                              │
  客户端 ──3102──→  │  ┌───────┐    gRPC     ┌───────┐            │
  (WebSocket)       │  │ Comet │◄───────────│ Logic │            │
                    │  └───┬───┘            └───┬───┘            │
                    │      │                    │                  │
                    │      │ register      register               │
                    │      ▼                    ▼                  │
                    │  ┌───────────┐    ┌───────┐  ┌───────┐     │
                    │  │ Discovery │    │ Redis │  │ MySQL │     │
                    │  └───────────┘    └───────┘  └───┬───┘     │
                    │                                    │         │
  客户端 ──3200──→  │  ┌─────────┐  HTTP   ┌───────┐   │         │
  (HTTP API)        │  │ Gateway │────────→│ Logic │   │         │
                    │  └────┬────┘         └───────┘   │         │
                    │       │ GORM                      │         │
                    │       └───────────────────────────┘         │
                    │                                              │
                    │  ┌───────┐  subscribe  ┌──────┐            │
                    │  │  Job  │◄────────────│ NATS │            │
                    │  └───┬───┘             └──────┘            │
                    │      │ gRPC push                            │
                    │      ▼                                      │
                    │  ┌───────┐                                  │
                    │  │ Comet │ → WebSocket push → 客户端        │
                    │  └───────┘                                  │
                    │                                              │
                    │  ┌────────────┐  scrape                     │
                    │  │ Prometheus │──────→ Comet:9100            │
                    │  │  :9090     │──────→ Job:9101              │
                    │  └────────────┘                              │
                    └─────────────────────────────────────────────┘
```

---

## 文件清单

完成后你的项目根目录应该新增这些文件：

```
goim/
├── Dockerfile              ← Go 服务多阶段构建
├── Dockerfile.discovery    ← Discovery 服务构建
├── docker-compose.yml      ← 编排所有服务
└── docker/
    ├── comet.toml          ← Docker 专用配置
    ├── logic.toml
    ├── job.toml
    ├── gateway.toml
    └── prometheus.yml      ← Prometheus 抓取配置
```

---

## 实施步骤

1. 创建 `docker/` 目录和配置文件
2. 创建 `Dockerfile`（Go 服务）
3. 创建 `Dockerfile.discovery`
4. 创建 `docker-compose.yml`
5. `docker compose up -d --build`
6. `docker compose ps` 确认所有容器 running
7. `go run examples/offline-demo/main.go` 端到端验证
8. `open http://localhost:9090` 查看 Prometheus
