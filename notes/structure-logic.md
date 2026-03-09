# Logic 数据结构详解

## 概览

```
Logic (逻辑服务)
├── c *conf.Config                    // 配置
├── dis *naming.Discovery             // 服务发现客户端
├── dao *dao.Dao                      // 数据访问层 (Redis)
│
├── 在线统计 ──────────────────────────
│   ├── totalIPs int64                // 所有 comet 的去重 IP 总数
│   ├── totalConns int64              // 所有 comet 的连接总数
│   └── roomCount map[string]int32    // 全局房间人数 (roomID → 跨所有 comet 的总人数)
│
└── 负载均衡 ──────────────────────────
    ├── nodes []*naming.Instance      // 所有在线 comet 节点的服务发现实例
    ├── loadBalancer *LoadBalancer     // 加权负载均衡器
    └── regions map[string]string     // 省份→区域 映射 (如 "江苏" → "sh")
```

---

## 一、Logic（逻辑服务主体）

**定义**：`internal/logic/logic.go:21`

```go
type Logic struct {
    c   *conf.Config
    dis *naming.Discovery
    dao *dao.Dao
    // online
    totalIPs   int64
    totalConns int64
    roomCount  map[string]int32
    // load balancer
    nodes        []*naming.Instance
    loadBalancer *LoadBalancer
    regions      map[string]string
}
```

### 成员详解

| 成员 | 类型 | 说明 |
|------|------|------|
| `c` | `*conf.Config` | 全局配置 |
| `dis` | `*naming.Discovery` | bilibili discovery 服务发现客户端，用于监听 comet 节点变化 |
| `dao` | `*dao.Dao` | 数据访问层，封装 Redis 操作 |
| `totalIPs` | `int64` | 所有 comet 上报的去重 IP 总数 |
| `totalConns` | `int64` | 所有 comet 上报的连接总数 |
| `roomCount` | `map[string]int32` | **全局房间在线人数汇总**，key 为 roomID，value 为跨所有 comet 的总人数。由 `onlineproc` 每 10 秒从 Redis 汇总 |
| `nodes` | `[]*naming.Instance` | 当前所有在线 comet 节点的服务发现实例列表 |
| `loadBalancer` | `*LoadBalancer` | 加权负载均衡器，为客户端分配最优 comet 节点 |
| `regions` | `map[string]string` | 省份到区域的映射表，如 `{"江苏": "sh", "浙江": "sh", "广东": "gz"}`，用于地域亲和调度 |

### 后台协程

Logic 启动时会开启一个后台协程 `onlineproc`，每 10 秒执行 `loadOnline()`：
==异步地每 10 秒汇总所有房间==

```go
func (l *Logic) onlineproc() {
    for {
        time.Sleep(10s)
        l.loadOnline()  // 从 Redis 读所有 comet 的房间数据，汇总到 l.roomCount
    }
}
```

### 节点发现

`initNodes()` 通过 discovery 监听 `"goim.comet"` 服务，当 comet 节点上下线时触发 `newNodes()`：

```go
func (l *Logic) newNodes(res naming.Resolver) {
    // 1. 从 discovery 拉取所有 comet 实例
    // 2. 过滤掉 offline=true 的节点
    // 3. 从实例 metadata 中读取 conn_count、ip_count
    // 4. 更新 l.totalConns、l.totalIPs、l.nodes
    // 5. 调用 l.loadBalancer.Update(allIns) 更新负载均衡器
}
```

每个 comet 实例在 discovery 中注册时携带的 metadata：

| metadata key | 含义 | 示例 |
|:---|:---|:---|
| `"weight"` | 负载均衡权重（管理员配置） | `"10"` |
| `"offline"` | 是否下线 | `"false"` |
| `"addrs"` | 公网地址列表（逗号分隔） | `"1.2.3.4:3101,1.2.3.4:3102"` |
| `"conn_count"` | 当前连接数 | `"240590"` |
| `"ip_count"` | 当前去重 IP 数 | `"180000"` |

---

## 二、LoadBalancer（加权负载均衡器）

**定义**：`internal/logic/balancer.go:78`

```go
type LoadBalancer struct {
    totalConns  int64                      // 所有节点的连接总数
    totalWeight int64                      // 所有节点的 fixedWeight 之和
    nodes       map[string]*weightedNode   // hostname → 加权节点
    nodesMutex  sync.Mutex                 // 保护并发访问
}
```

### 用途

==客户端首次连接时，需要知道应该连哪个 comet。Logic 提供 HTTP 接口== `NodesWeighted()`，通过 `LoadBalancer` 返回一个**按权重排序**的 comet 节点列表（最多 5 个），排在前面的是最应该连接的节点。

```
客户端 → HTTP 请求 Logic → LoadBalancer 计算排序 → 返回 comet 地址列表
```

### 常量

```go
const (
    _minWeight = 1        // 节点最低权重下限
    _maxWeight = 1 << 20  // 节点最高权重上限 (1048576)
    _maxNodes  = 5        // 最多返回 5 个节点
)
```

---

## 三、weightedNode（加权节点）

**定义**：`internal/logic/balancer.go:22`

```go
type weightedNode struct {
    region        string   // 所属区域，如 "sh"、"bj"、"gz"
    hostname      string   // 主机名，如 "comet-01"
    addrs         []string // 公网地址列表
    fixedWeight   int64    // 固定权重（管理员配置，不变）
    currentWeight int64    // 当前动态权重（每次选择时重新计算）
    currentConns  int64    // 当前连接数（discovery 初始值 + 每次被选中 +1）
    updated       int64    // 上次更新时间戳（用于判断是否需要刷新）
}
```

### 成员详解

| 成员 | 说明 |
|------|------|
| `region` | 节点所在地域，来自 discovery 注册时的 `Region` 字段 |
| `hostname` | 节点主机名，作为 map 的 key 唯一标识 |
| `addrs` | 公网 IP 地址列表，由 metadata 中的 `"addrs"` 按逗号拆分 |
| `fixedWeight` | **固定权重**。管理员为每个 comet 配置的静态权重值，代表该节点的期望承载能力比例 |
| `currentWeight` | **动态权重**。每次调用 `NodeAddrs` 时根据算法重新计算，用于排序选出最优节点 |
| `currentConns` | **当前连接数**。初始值从 discovery metadata 读取，之后每次该节点被选中就 +1，用于跟踪分配情况 |
| `updated` | discovery 实例的 `LastTs`，用于判断实例信息是否有变化。无变化时沿用旧的 `weightedNode`（保留 `currentConns` 等运行时状态） |

---

## 四、负载均衡算法详解

### 核心思想

**让每个节点的实际连接占比趋近于其权重占比，同时支持地域亲和。**

具体来说，如果节点 A 的 `fixedWeight` 占总权重的 30%，那么算法会尝试让它承载约 30% 的连接。如果它当前的连接占比低于 30%（承载不足），就提高它的 `currentWeight` 让它更容易被选中；如果高于 30%（承载过多），就降低。

### 调用入口：`NodeAddrs`

```go
func (lb *LoadBalancer) NodeAddrs(region, domain string, regionWeight float64) (domains, addrs []string)
```

由 `Logic.NodesWeighted()` → `Logic.nodeAddrs()` 调用，用于客户端获取 comet 节点列表。

参数：
- `region`：客户端所在区域（通过 IP 定位省份，再查 `regions` 映射得到）
- `domain`：域名后缀，如 `".bilibili.com"`
- `regionWeight`：地域亲和加成系数（配置项 `Node.RegionWeight`，如 `1.6`）

==返回 (internal/logic/nodes.go:NodesWeighted)==：

- `domains`：域名列表，如 `["comet-01.bilibili.com", "comet-02.bilibili.com"]`，给 web 平台用
- `addrs`：IP 地址列表，给非 web 平台用

**最多返回 `_maxNodes = 5` 个节点，按 `currentWeight` 从高到低排序。**

### 选择流程：`weightedNodes`

```go
func (lb *LoadBalancer) weightedNodes(region string, regionWeight float64) (nodes []*weightedNode) {
    for _, n := range lb.nodes {
        // 1. 同区域节点获得加权倍率
        gainWeight := 1.0
        if n.region == region {
            gainWeight *= regionWeight  // 例如 1.6 倍
        }
        // 2. 计算每个节点的动态权重
        n.calculateWeight(lb.totalWeight, lb.totalConns, gainWeight)
        nodes = append(nodes, n)
    }
    // 3. 按 currentWeight 降序排列
    sort.Slice(nodes, func(i, j int) bool {
        return nodes[i].currentWeight > nodes[j].currentWeight
    })
    // 4. 权重最高的节点被选中，连接数 +1
    if len(nodes) > 0 {
        nodes[0].chosen()    // currentConns++
        lb.totalConns++
    }
    return
}
```

**每次调用 `NodeAddrs` 都会让排名第一的节点的 `currentConns` +1**，模拟一个新连接分配给了它。这使得连续调用时，排名第一的节点会因为 `currentConns` 增加而权重逐渐下降，从而实现轮转效果。

### 权重计算算法：`calculateWeight`

```go
func (w *weightedNode) calculateWeight(totalWeight, totalConns int64, gainWeight float64) {
    // 1. 应用地域加权
    fixedWeight := float64(w.fixedWeight) * gainWeight
    // 调整 totalWeight 以反映加权后的变化
    totalWeight += int64(fixedWeight) - w.fixedWeight

    if totalConns > 0 {
        // 2. 计算「权重占比」和「连接占比」
        weightRatio := fixedWeight / float64(totalWeight)     // 期望分配比例
        connRatio   := float64(w.currentConns) / float64(totalConns) * 0.5  // 实际承载比例 (×0.5 衰减)

        // 3. 计算差值：正值表示该节点承载不足，应多分配
        diff := weightRatio - connRatio

        // 4. 映射到具体权重值
        multiple := diff * float64(totalConns)
        // 四舍五入
        floor := math.Floor(multiple)
        if floor - multiple >= -0.5 {
            w.currentWeight = int64(fixedWeight + floor)
        } else {
            w.currentWeight = int64(fixedWeight + math.Ceil(multiple))
        }

        // 5. 边界裁剪
        if diff < 0 {
            // 过载节点：权重不低于 _minWeight (1)，保证还有机会被选
            w.currentWeight = max(w.currentWeight, _minWeight)
        } else {
            // 欠载节点：权重不超过 _maxWeight (1<<20)，防止极端值
            w.currentWeight = min(w.currentWeight, _maxWeight)
        }
    } else {
        w.currentWeight = 0  // 没有连接时重置
    }
}
```

### 用数值举例

假设 3 个 comet 节点，`fixedWeight` 均为 10，`regionWeight` = 1.6：

```
节点        region   fixedWeight   currentConns
comet-bj    bj       10            240590
comet-sh    sh       10            375420
comet-gz    gz       10            293430
```

客户端来自上海（region = "sh"），调用 `NodeAddrs("sh", ".test", 1.6)`：

**Step 1：应用地域加权**

```
comet-bj: gainWeight=1.0, fixedWeight' = 10 × 1.0 = 10
comet-sh: gainWeight=1.6, fixedWeight' = 10 × 1.6 = 16  ← 同区域加成
comet-gz: gainWeight=1.0, fixedWeight' = 10 × 1.0 = 10
```

**Step 2：计算各节点的 diff**

以 comet-sh 为例（fixedWeight' = 16）：
```
totalWeight' = 30 + (16 - 10) = 36  (调整后)
totalConns = 909440

weightRatio = 16 / 36 = 0.4444
connRatio = 375420 / 909440 × 0.5 = 0.2064

diff = 0.4444 - 0.2064 = +0.238  (正值，说明应该多分配)
```

以 comet-bj 为例（fixedWeight' = 10）：
```
totalWeight' = 30 + (10 - 10) = 30  (不变)
weightRatio = 10 / 30 = 0.3333
connRatio = 240590 / 909440 × 0.5 = 0.1323

diff = 0.3333 - 0.1323 = +0.201  (正值，但比 sh 小)
```

结果：comet-sh 因为地域加成，`currentWeight` 最高，排在列表第一位。

**Step 3：返回结果**

```
排序：comet-sh > comet-bj ≈ comet-gz
返回：["comet-sh.test", "comet-bj.test", "comet-gz.test"]
comet-sh.currentConns++, totalConns++
```

### `connRatio` 中 `× 0.5` 的作用

```go
connRatio = float64(w.currentConns) / float64(totalConns) * 0.5
```

乘以 0.5 是一个**衰减因子**，降低了当前连接数对权重计算的影响力。效果是使算法更偏向于按 `fixedWeight` 的比例分配，而不是过度修正已有连接的不均衡。这避免了节点权重的剧烈震荡。

### `Update` 方法

==当 discovery 检测到 comet 节点变化时调用==：

```go
func (lb *LoadBalancer) Update(ins []*naming.Instance) {
    // 安全检查：新节点数不能少于旧节点数的一半（防止误删）
    if len(ins) == 0 || float32(len(ins))/float32(len(lb.nodes)) < 0.5 {
        return  // 拒绝更新
    }
    for _, in := range ins {
        if old, ok := lb.nodes[in.Hostname]; ok && old.updated == in.LastTs {
            // 节点未变化：沿用旧的 weightedNode（保留 currentConns 等运行时状态）
            nodes[in.Hostname] = old
        } else {
            // 新节点或有更新：从 metadata 重新构建
            nodes[in.Hostname] = &weightedNode{
                fixedWeight:  meta["weight"],
                currentConns: meta["conn_count"],  // 用 discovery 上报的真实连接数初始化
                ...
            }
        }
    }
    lb.nodes = nodes
    lb.totalConns = totalConns
    lb.totalWeight = totalWeight
}
```

关键点：
- **节点未变化时保留运行时状态**：`currentConns` 包含了被 `chosen()` 累加的分配次数，不会丢失
- **节点有更新时用真实连接数重置**：comet 定期向 discovery 上报真实 `conn_count`，此时 `currentConns` 被校准回真实值
- **安全阈值**：如果新节点列表不到旧列表的一半，拒绝更新，防止 discovery 故障导致大量节点被误判为下线
