# Prometheus 监控接入指南（GoIM 项目）

## 它是什么

Prometheus 是一个**拉取式（Pull）**监控系统。它主动定时访问你服务暴露的 `/metrics` HTTP 端点，抓取指标数据并存储为时序数据，然后你可以用 PromQL 查询或用 Grafana 做图表。

```
Comet (:3109/metrics) ──┐
                        ├──  Prometheus (定时拉取) ──→ Grafana (可视化)
Job   (:3110/metrics) ──┘
```

---

## 核心概念：四种指标类型

Prometheus 定义了四种指标类型，GoIM 中会用到其中三种：

| 类型 | 含义 | GoIM 中的例子 |
|------|------|---------------|
| **Gauge**（仪表盘） | 可增可减的瞬时值 | Comet 当前在线连接数 |
| **Counter**（计数器） | 只增不减的累计值 | 消息推送总量、消息丢弃总数、MQ 消费总条数 |
| **Histogram**（直方图） | 观测值的分布（自动算分位数） | Job 推送延迟的 P99/P95 |
| Summary | 类似 Histogram，在客户端算分位数 | （本项目不使用） |

### Gauge vs Counter 怎么选？

- 值会**下降**（如连接断开）→ Gauge
- 值只会**单调递增**（如累计推送了多少条消息）→ Counter

Counter 看起来只是一直涨的数字，但 Prometheus 通过 `rate()` 函数自动算出「每秒速率」，这才是你真正关心的值。

---

## Go 中怎么用

Go 官方客户端库是 `github.com/prometheus/client_golang`。核心就两步：**定义指标** + **暴露端点**。

### 1. 定义指标

```go
import "github.com/prometheus/client_golang/prometheus"

// Gauge —— 当前在线连接数
var onlineConnections = prometheus.NewGauge(prometheus.GaugeOpts{
    Namespace: "goim",
    Subsystem: "comet",
    Name:      "online_connections",
    Help:      "Current number of online connections",
})

// Counter —— 推送消息总数
var pushMessagesTotal = prometheus.NewCounter(prometheus.CounterOpts{
    Namespace: "goim",
    Subsystem: "comet",
    Name:      "push_messages_total",
    Help:      "Total number of pushed messages",
})

// Histogram —— 推送延迟
var pushDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
    Namespace: "goim",
    Subsystem: "job",
    Name:      "push_duration_seconds",
    Help:      "Push latency distribution",
    Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
})

func init() {
    prometheus.MustRegister(onlineConnections, pushMessagesTotal, pushDuration)
}
```

### 2. 在业务代码中打点

```go
// 连接建立时
onlineConnections.Inc()
// 连接断开时
onlineConnections.Dec()

// 每次推送消息
pushMessagesTotal.Inc()

// 测量耗时
start := time.Now()
doPush()
pushDuration.Observe(time.Since(start).Seconds())
```

### 3. 暴露 /metrics 端点

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

// 单独起一个 HTTP server，不侵入业务端口
go func() {
    mux := http.NewServeMux()
    mux.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":9090", mux)
}()
```

访问 `curl localhost:9090/metrics` 会看到类似：

```
# HELP goim_comet_online_connections Current number of online connections
# TYPE goim_comet_online_connections gauge
goim_comet_online_connections 1523

# HELP goim_comet_push_messages_total Total number of pushed messages
# TYPE goim_comet_push_messages_total counter
goim_comet_push_messages_total 84210
```

---

## 指标命名规范

```
<namespace>_<subsystem>_<name>_<unit>
  goim       comet      push_messages_total
  goim       job        consume_messages_total
  goim       job        push_duration_seconds
```

- Counter 以 `_total` 结尾
- 时间单位用秒（`_seconds`），这是 Prometheus 惯例

---

## GoIM 需要接入的指标

根据 tasks.md 中的规划：

| 服务 | 任务 | 指标类型 | 指标名 |
|------|------|---------|--------|
| Comet | 在线连接数 | Gauge | `goim_comet_online_connections` |
| Comet | 消息推送量 | Counter | `goim_comet_push_messages_total` |
| Comet | 消息丢弃数 | Counter | `goim_comet_push_messages_dropped_total` |
| Job | MQ 消费速率 | Counter | `goim_job_consume_messages_total` |
| Job | 推送延迟 | Histogram | `goim_job_push_duration_seconds` |

---

## 常用 PromQL 查询

```promql
-- Comet 当前在线连接数（直接读 Gauge）
goim_comet_online_connections

-- 最近 1 分钟每秒消息推送速率
rate(goim_comet_push_messages_total[1m])

-- 最近 1 分钟每秒 MQ 消费速率
rate(goim_job_consume_messages_total[1m])

-- Job 推送延迟 P99
histogram_quantile(0.99, rate(goim_job_push_duration_seconds_bucket[5m]))

-- Job 推送延迟 P95
histogram_quantile(0.95, rate(goim_job_push_duration_seconds_bucket[5m]))
```

---

## 依赖

```bash
go get github.com/prometheus/client_golang
```

只需要这一个依赖，它包含了指标定义（`prometheus` 包）和 HTTP handler（`promhttp` 包）。

---

---

---

---

## Prometheus 集成方案

### 整体结构

```
新增文件：
  internal/comet/metrics.go    ← 定义 Comet 指标 + 暴露 /metrics
  internal/job/metrics.go      ← 定义 Job 指标 + 暴露 /metrics

修改文件（只加一行打点调用）：
  Comet:
    internal/comet/server_tcp.go        ← 连接数 Inc/Dec
    internal/comet/server_websocket.go  ← 连接数 Inc/Dec
    internal/comet/grpc/server.go       ← 推送量 Counter
  Job:
    internal/job/job.go                 ← 消费量 Counter
    internal/job/push.go                ← 推送延迟 Histogram
```

### Comet 侧（3 个指标）

| 指标                                     | 类型    | 打点位置                                                     |
| ---------------------------------------- | ------- | ------------------------------------------------------------ |
| `goim_comet_online_connections`          | Gauge   | `server_tcp.go` / `server_websocket.go` 中连接建立时 `.Inc()`，`Disconnect` 后 `.Dec()` |
| `goim_comet_push_messages_total`         | Counter | `grpc/server.go` 的 `PushMsg`、`Broadcast`、`BroadcastRoom` handler 中 `.Inc()` |
| `goim_comet_push_messages_dropped_total` | Counter | `Channel.Push()` 失败（channel 满了丢弃）时 `.Inc()`         |

`/metrics` 端点：在 `cmd/comet/main.go` 启动时额外起一个 goroutine 监听独立端口（如 `:9100`）。

### Job 侧（2 个指标）

| 指标                              | 类型      | 打点位置                                                    |
| --------------------------------- | --------- | ----------------------------------------------------------- |
| `goim_job_consume_messages_total` | Counter   | `job.go` 的 `Consume()` handler 中每收到一条消息 `.Inc()`   |
| `goim_job_push_duration_seconds`  | Histogram | `push.go` 的 `push()` 方法首尾包裹计时，`.Observe(elapsed)` |

`/metrics` 端点：同样独立端口（如 `:9101`）。

### 代码改动量

每个 `metrics.go` 约 30-40 行（定义指标 + 启动 HTTP），业务文件每处改动只加 1 行调用。**不侵入现有逻辑**，只是在关键路径上插入计数。

---

要开始写代码吗？可以按 Comet → Job 的顺序逐个接入。
