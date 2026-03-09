> 在我理解了 POST /goim/push/mids 的数据流之后, 我想要继续理解 POST /goim/push/rooms 的数据流                                            其中涉及到 job 中 Room 的协程 pushproc @internal/job/room.go                                                            解读一下这个函数

`pushproc` 是每个 Room 的后台协程，负责将消息批量合并后广播，核心是一个"攒批 + 定时刷新"的机制。

逐段解读：

**初始化**

```go
buf = bytes.NewWriterSize(int(protocol.MaxBodySize))
```
创建一个写缓冲区，用于合并多条 proto 消息的二进制数据。

**定时器**

```go
td := time.AfterFunc(sigTime, func() {
    select {
    case r.proto <- roomReadyProto:
    default:
    }
})
```
设置一个定时器，到期后往 channel 塞一个哨兵值 `roomReadyProto`，作用是"时间到了，该刷缓冲区了"。

**主循环 for + select**

从 `r.proto` channel 读取消息，三种情况：

1. `p == nil` — channel 被关闭，退出协程
2. `p != roomReadyProto` — 正常业务消息：
   - 写入 buf（合并二进制）
   - `n == 1`（第一条消息）：记录时间，重置定时器，`continue` 等更多消息
   - `1 < n < batch` 且未超时：`continue` 继续攒
   - `n >= batch` 或已超时：跳出 if，执行下面的 broadcast
3. `p == roomReadyProto` — 定时器触发的哨兵：
   - `n == 0`：没有待发消息，说明房间空闲，`break` 退出协程（房间自动回收）
   - `n > 0`：有消息待发，跳出 if，执行 broadcast

**广播 + 重置**
```go
_ = r.job.broadcastRoomRawBytes(r.id, buf.Buffer())
buf = bytes.NewWriterSize(buf.Size())  // 新 buf，旧的让 GC 回收
n = 0
```
把合并后的数据广播到房间所有 comet 节点，然后重置状态。

**Idle 超时**
```go
if r.c.Idle != 0 {
    td.Reset(time.Duration(r.c.Idle))
} else {
    td.Reset(time.Minute)
}
```
广播完后，用 Idle 时间（默认1分钟）重置定时器。如果这段时间内没有新消息进来，哨兵触发时 `n == 0`，协程退出，房间被 `delRoom` 清理。

**总结**：这是一个经典的批量合并发送模式 — 消息攒够 `batch` 条或超过 `sigTime` 时间就刷一次，空闲超时则自动销毁房间协程，避免资源泄漏。