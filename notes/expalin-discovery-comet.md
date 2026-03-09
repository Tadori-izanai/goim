> ❯ 那么 comet 是怎么通过 discovery 发现 logic (logic 的 gprc 服务) 的                                                        
>
>  我似乎只找到 @internal/comet/server.go :30 newLogicClient:                                                            
>
>  conn, err := grpc.DialContext(ctx, "discovery://default/goim.logic"                                                        
>
> ​                                                                                          
>
>  那么 "discovery://default/goim.logic" 是什么含义, 会自动跳转连接 logic 的TCP端口吗?                                                
>
>  此外这个是否能代表 logic 是单机部署的, 或者在整个系统中, 逻辑上只有一台主机在运作? 

现在可以完整回答了。

---

## `"discovery://default/goim.logic"` 的含义

这是一个 **gRPC 自定义 resolver** 的地址格式，遵循 gRPC 的标准 URI scheme 解析规则：

```
discovery://default/goim.logic
  │          │        │
  scheme   authority  endpoint (= appID)
```

- **`discovery`**：scheme 名，对应 `bilibili/discovery/naming/grpc` 注册的 resolver builder（它的 `Scheme()` 返回 `"discovery"`）
- **`default`**：authority，这里没有实际作用
- **`goim.logic`**：endpoint，即要发现的服务 appID

### 自动发现的机制

整个链路是这样的：

**1. Logic 启动时注册自己**

```go
// cmd/logic/main.go:66
ins := &naming.Instance{
    AppID: "goim.logic",
    Addrs: []string{"grpc://10.0.0.2:3119"},  // 自己的 gRPC 地址
    ...
}
dis.Register(ins)  // 注册到 discovery 服务端
```

**2. Comet 启动时注册 discovery resolver 到 gRPC 全局**

```go
// cmd/comet/main.go:43
dis := naming.New(conf.Conf.Discovery)
resolver.Register(dis)  // 把 discovery 注册为 gRPC 的 resolver builder
```

`resolver.Register(dis)` 实际调用的是 `google.golang.org/grpc/resolver.Register()`，让 gRPC 知道 `"discovery"` 这个 scheme 该用什么 builder。

**3. gRPC Dial 时触发自动解析**

```go
grpc.DialContext(ctx, "discovery://default/goim.logic", ...)
```

gRPC 看到 scheme 是 `"discovery"`，找到对应的 Builder，调用其 `Build()` 方法：

```go
// naming/grpc/resolver.go:44
func (b *Builder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOption) (resolver.Resolver, error) {
    // target.Endpoint = "goim.logic"
    r := &Resolver{
        cc:   cc,
        nr:   b.Builder.Build("goim.logic"),  // 向 discovery 订阅 "goim.logic"
        ...
    }
    go r.watcher()  // 启动一个协程监听变化
    return r, nil
}
```

**4. watcher 持续监听并更新 gRPC 地址列表**

```go
// naming/grpc/resolver.go:98
func (r *Resolver) watcher() {
    event := r.nr.Watch()
    for {
        <-event  // 等待 discovery 通知有变化
        ins, ok := r.nr.Fetch()
        if ok {
            instances := ins.Instances[r.zone]  // 优先同 zone
            r.newAddress(instances)
        }
    }
}

func (r *Resolver) newAddress(instances []*naming.Instance) {
    for _, ins := range instances {
        // 从 Addrs 中找 scheme="grpc" 的地址
        // 例如 "grpc://10.0.0.2:3119" → 提取 "10.0.0.2:3119"
        rpcAddr, color, weight := extractAddrs(ins)
        addrs = append(addrs, resolver.Address{Addr: rpcAddr, ...})
    }
    // 告诉 gRPC：这些是 "goim.logic" 的后端地址
    r.cc.NewAddress(addrs)
}
```

gRPC 收到新地址列表后，会自动建立/更新底层 TCP 连接。

**5. 配合 round-robin 负载均衡**

```go
grpc.WithBalancerName(roundrobin.Name)
```

如果有多台 logic，gRPC 会对所有地址做 round-robin 轮询。

---

## 完整流程图

```
Logic-A 启动                          Logic-B 启动
    │                                     │
    ▼                                     ▼
dis.Register({                       dis.Register({
  AppID: "goim.logic",                 AppID: "goim.logic",
  Addrs: ["grpc://10.0.0.2:3119"]     Addrs: ["grpc://10.0.0.3:3119"]
})                                   })
    │                                     │
    └──────────┬──────────────────────────┘
               ▼
        Discovery 服务端
        记录: "goim.logic" → [10.0.0.2:3119, 10.0.0.3:3119]
               │
               │  long poll 推送变更
               ▼
        Comet 内部 gRPC resolver
        grpc.Dial("discovery://default/goim.logic")
               │
               ▼
        gRPC 自动连接:
          → 10.0.0.2:3119  (Logic-A)
          → 10.0.0.3:3119  (Logic-B)
        每次 RPC 调用 round-robin 轮询
```

---

## Logic 是单机还是多机？

**Logic 支持多机部署，不是单机限制。** 证据：

1. **gRPC 配置了 `roundrobin` 负载均衡**——如果只有一台，没必要配这个
2. **discovery 的 `InstancesInfo.Instances` 是列表**——同一个 appID 可以注册多个实例
3. ==**Logic 本身是无状态的**==——它的运行时数据（`l.roomCount`）每台独立从 Redis 汇总，结果一样；负载均衡器状态（`currentConns`）每台独立维护，彼此不需要同步

不过有一个**细微差异**：每台 logic 各自维护一个 `LoadBalancer`，其中的 `currentConns` 是由 discovery 初始值 + 本机被调用 `NodeAddrs` 的次数累加而来。多台 logic 之间不共享这个计数，所以在多 logic 部署时，每台 logic 的负载均衡决策是独立的，但由于 `currentConns` 初始值来自 discovery 的真实连接数（每 10 秒同步），偏差不会太大。