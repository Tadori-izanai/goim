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


# ↑ 编译
# ↓ 运行
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
