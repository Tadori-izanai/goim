好，现在我对整个流程有了完整的理解。

---

## Redis 中存了什么

每个 comet 服务器在 Redis 中对应 **一个 Hash key**，格式为：

```
ol_{serverHostname}
```

例如 comet 服务器 hostname 为 `comet-01`，则 Redis key 为 `ol_comet-01`。

这个 Hash 有 **最多 64 个 field**（编号 `"0"` ~ `"63"`），每个 field 的 value 是一个 JSON 字符串，结构为：

```json
{
  "server": "comet-01",
  "room_count": {"live://1000": 150, "live://2000": 80},
  "updated": 1700000000
}
```

用 Redis 视角来看，实际存储结构是这样的：

```
Key: "ol_comet-01"   (类型: Hash, 带 TTL)
├── Field "0"  → {"server":"comet-01", "room_count":{"live://1000":150}, "updated":1700000000}
├── Field "3"  → {"server":"comet-01", "room_count":{"live://2000":80, "live://5555":30}, "updated":1700000000}
├── Field "17" → {"server":"comet-01", "room_count":{"live://3000":200}, "updated":1700000000}
└── ...（最多64个field）
```

**房间被分配到哪个 field，由 `cityhash(roomID) % 64` 决定。** 哈希值相同的房间放在同一个 field 的 JSON 里。

---

## 上报流程（写入）

### 1. Comet 汇总本地数据

```go
// internal/comet/server.go:107  onlineproc
roomCount := make(map[string]int32)
for _, bucket := range s.buckets {
    for roomID, count := range bucket.RoomsCount() {
        roomCount[roomID] += count  // 合并所有 bucket
    }
}
// roomCount = {"live://1000": 150, "live://2000": 80, "live://3000": 200}
```

### 2. RPC 到 Logic → 调用 `AddServerOnline`

```go
// internal/logic/dao/redis.go:188
func (d *Dao) AddServerOnline(c context.Context, server string, online *model.Online) (err error) {
    // 第一步：按 cityhash(roomID) % 64 分桶
    roomsMap := map[uint32]map[string]int32{}
    for room, count := range online.RoomCount {
        hashKey := cityhash.CityHash32([]byte(room), uint32(len(room))) % 64
        rMap := roomsMap[hashKey]
        if rMap == nil {
            rMap = make(map[string]int32)
            roomsMap[hashKey] = rMap
        }
        rMap[room] = count
    }
    // 第二步：每个桶写一次 Redis HSET
    key := keyServerOnline(server)  // "ol_comet-01"
    for hashKey, value := range roomsMap {
        // HSET "ol_comet-01" "17" '{"server":"comet-01","room_count":{"live://3000":200},"updated":...}'
        // EXPIRE "ol_comet-01" TTL
        d.addServerOnline(c, key, strconv.FormatInt(int64(hashKey), 10),
            &model.Online{RoomCount: value, Server: online.Server, Updated: online.Updated})
    }
}
```

**为什么要分 64 个桶？** 如果一个 comet 上有几万个房间，全部序列化成一个大 JSON 会导致：
- 单个 Redis value 过大
- 每次全量写入/读取开销大

分成 64 个小 JSON 后，每个 field 的 value 体积可控。

---

## 读取流程

### 1. Logic 后台 `loadOnline`（每 10 秒执行一次）

```go
// internal/logic/logic.go:141
func (l *Logic) loadOnline() (err error) {
    roomCount := make(map[string]int32)
    // 遍历所有 comet 节点
    for _, server := range l.nodes {
        // 从 Redis 读取该 comet 的完整房间数据
        online, err = l.dao.ServerOnline(ctx, server.Hostname)
        // 过期检测（超过5分钟未更新则删除）
        if time.Since(time.Unix(online.Updated, 0)) > _onlineDeadline {
            l.dao.DelServerOnline(ctx, server.Hostname)
            continue
        }
        // 累加到全局
        for roomID, count := range online.RoomCount {
            roomCount[roomID] += count
        }
    }
    l.roomCount = roomCount  // 全局汇总结果
}
```

### 2. `ServerOnline`：读取一个 comet 的全部房间数据

```go
// internal/logic/dao/redis.go:234
func (d *Dao) ServerOnline(c context.Context, server string) (online *model.Online, err error) {
    online = &model.Online{RoomCount: map[string]int32{}}
    key := keyServerOnline(server)  // "ol_comet-01"
    // 遍历 0~63，逐个读取每个分桶
    for i := 0; i < 64; i++ {
        // HGET "ol_comet-01" "0"
        // HGET "ol_comet-01" "1"
        // ... 共 64 次
        ol, err := d.serverOnline(c, key, strconv.FormatInt(int64(i), 10))
        if err == nil && ol != nil {
            online.Server = ol.Server
            if ol.Updated > online.Updated {
                online.Updated = ol.Updated
            }
            // 合并房间数据
            for room, count := range ol.RoomCount {
                online.RoomCount[room] = count
            }
        }
    }
    return
}
```

---

## 全景图

```
┌─────────────────────────────────────────────────────────────────┐
│                         Redis                                   │
│                                                                 │
│  "ol_comet-01" (Hash)                "ol_comet-02" (Hash)       │
│  ┌──────┬──────────────────────┐     ┌──────┬────────────────┐  │
│  │ "0"  │ {room_count:{...}}   │     │ "0"  │ {room_count:{}}│  │
│  │ "1"  │ {room_count:{...}}   │     │ "5"  │ {room_count:{}}│  │
│  │ ...  │ ...                  │     │ ...  │ ...            │  │
│  │ "63" │ {room_count:{...}}   │     │ "63" │ {room_count:{}}│  │
│  └──────┴──────────────────────┘     └──────┴────────────────┘  │
└────────────────────────┬────────────────────────────────────────┘
                         │
          ┌──────────────┴──────────────┐
          │     Logic (onlineproc)       │
          │  每10秒:                      │
          │  1. 读所有 comet 的 Hash      │
          │  2. 汇总: roomCount[id]+=cnt │
          │  3. l.roomCount = 全局结果    │
          └──────────────┬──────────────┘
                         │
          ┌──────────────┴──────────────┐
          │  Comet 调用 RenewOnline      │
          │  1. 上报本地 → 写 Redis       │
          │  2. 拿回 l.roomCount(全局)   │
          │  3. 更新 room.AllOnline       │
          └─────────────────────────────┘
```