# GoIM 二次开发 Todo

## 阶段一：读懂源码 ✅

- [x] 理解整体架构：Comet / Logic / Job 三层职责与数据流
- [x] 读懂 Comet：WebSocket 连接管理、Bucket 结构、心跳机制
- [x] 读懂 Logic：鉴权流程、消息路由、gRPC 接口
- [x] 读懂 Job：从 MQ 消费消息并下发到 Comet 的流程
- [x] 梳理各服务配置文件（.toml）与启动入口

---

## 阶段二：goim 改造

### 替换 MQ（Kafka → NATS）
- [x] 用 interface 抽象 MQ 的 Publish / Subscribe 行为，解耦 Kafka 强依赖
- [x] 实现 NATS 适配器（Publisher + Consumer）
- [x] 替换 Logic 发布消息、Job 消费消息的实现

### 本地功能验证（替换后跑通）
- [x] 编译压测工具（client / push / push_room）
- [x] 小参数验证：500 连接 + 10 条/秒，确认 down/s > 0，链路跑通

### 接入 Prometheus 监控
- [x] Comet：在线连接数、消息推送量、消息丢弃数
- [x] Job：MQ 消费速率、推送延迟
- [x] 暴露 /metrics 端点

> **注**：保留 bilibili/discovery，不做移除。保留 TCP 协议，压测工具依赖 TCP 客户端。

---

## 阶段三：用户系统与鉴权

> 独立的 Gin 服务，不侵入 goim 核心

- [x] 搭建 Gin + GORM 骨架，连接 MySQL 和 Redis
- [x] 数据库设计：`users` 表
- [x] 注册 / 登录接口（返回 JWT Token）
- [x] Comet WebSocket 握手时读取 Token（URL 参数或 Header）
- [x] Comet 验证 JWT，解析出用户 ID

---

## 阶段四：单聊

> Gateway 作为业务代理，goim 本体零改动。消息全量落库，同时实时推送。
> 详细计划见 `notes/plan-single-chat.md`

- [x] 数据库设计：`friends` 表、`messages` 表（GORM AutoMigrate）
- [x] JWT 鉴权中间件（保护 `/goim/chat/*` 和 `/goim/friend/*`）
- [x] 好友接口：添加好友、删除好友、好友列表
- [x] 发消息接口：Gateway 落库 → 调 Logic `/goim/push/mids`（op=2001）
- [x] 历史消息查询接口（分页游标）
- [x] 端到端测试：注册 → 加好友 → 发消息 → WebSocket 收到

---

## 阶段五：群聊

> 在单聊基础上增量很小，复用消息存储和推送链路

- [x] 数据库设计：`groups` 表、`group_members` 表、`group_messages` 表
- [x] 群组接口：创建群、加入群、退出群、群成员列表
- [x] 发群消息：Gateway 落库 → 调 Logic `/goim/push/room`（op=2002）
- [x] ~~客户端切换群：发送 `OpChangeRoom` 切换到对应群房间~~

---

## 阶段六：离线消息与 ACK

- [x] users 表加 `last_online_at`，记录用户上次在线时间
- [x] 单聊离线：上线时查 `messages WHERE to_id=? AND created_at > last_online_at`
- [x] 群聊离线：同 users, 查 `group_messages`
- [x] ACK 机制：客户端确认收到，超时重发，保证可靠投递

---

## 阶段七：Docker 化部署

- [ ] 为每个服务编写 Dockerfile（Comet、Logic、Job、业务 API）
- [ ] 编写 docker-compose.yml，编排所有服务 + 中间件（MySQL、Redis、NATS、Discovery）
- [ ] 端到端测试：WebSocket 客户端收发消息验证

---

## 阶段八：云服务器压测

> 所有功能完成后，最后统一出简历可用的性能数据

### 环境准备
- [ ] 租两台云服务器（按量计费，2 核 4G 即可，约 ¥0.5/小时/台）
  - 服务器 A：goim 全套 + 中间件（docker-compose 一键部署）
  - 服务器 B：压测工具（client + push）
- [ ] 确认系统 ulimit、内核参数（文件描述符、TCP 缓冲区）

### Kafka vs NATS 对比压测
- [ ] 第一轮：Kafka 版本基线
  - 1 万连接 + 40 条/秒房间推送，记录 down/s
  - 5 万连接 + 100 条/秒，高压测试
- [ ] 第二轮：NATS 版本，同参数压测
- [ ] 记录对比结果到 `notes/bench-result.md`

### 产出简历数据
- [ ] 最大并发连接数（alive 稳定值）
- [ ] 消息吞吐量（down/s 稳定值）
- [ ] 推送延迟（如已接入 Prometheus，取 P99）
- [ ] 整理为简历描述：连接数 / 吞吐量 / 延迟
