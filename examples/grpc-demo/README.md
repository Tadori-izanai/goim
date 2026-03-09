# gRPC Demo

这是一个简单的 gRPC 示例，帮助理解 goim 中的 gRPC 使用。

## 功能

- `SayHello` - 简单的问候方法
- `Auth` - 鉴权方法（类似 goim 的 Connect）

## 快速开始

### 方法一：使用脚本（推荐）

```bash
cd examples/grpc-demo

# 一键安装和生成代码
./setup.sh
```

### 方法二：手动步骤

#### 1. 安装 protoc

**macOS**:
```bash
brew install protobuf
```

**Linux**:
```bash
# Ubuntu/Debian
sudo apt install -y protobuf-compiler

# 或下载预编译版本
# https://github.com/protocolbuffers/protobuf/releases
```

**验证安装**:
```bash
protoc --version
# 输出: libprotoc 3.x.x 或更高
```

#### 2. 安装 Go 插件

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

#### 3. 下载依赖

```bash
go mod tidy
```

#### 4. 生成 gRPC 代码

```bash
protoc --go_out=. --go-grpc_out=. hello.proto
```

会生成两个文件：
- `hello/hello.pb.go` - 消息定义
- `hello/hello_grpc.pb.go` - 服务接口

#### 5. 启动服务端

```bash
cd server
go run main.go
```

输出：
```
gRPC 服务器启动在 :50051
```

#### 6. 运行客户端（新终端）

```bash
cd client
go run main.go
```

## 输出示例

**服务端**：
```
gRPC 服务器启动在 :50051
收到请求: name=张三
收到鉴权请求: token=abc123, user_id=123
```

**客户端**：
```
收到响应: Hello, 张三!
鉴权结果: success=true, session_key=session_abc123
```

## 目录结构

```
grpc-demo/
├── hello.proto          # Proto 定义
├── hello/               # 生成的代码（运行 protoc 后）
│   ├── hello.pb.go
│   └── hello_grpc.pb.go
├── server/
│   └── main.go          # 服务端实现
├── client/
│   └── main.go          # 客户端实现
├── go.mod
├── setup.sh             # 一键安装脚本
└── README.md
```

## 对应 goim 的使用

| Demo | goim |
|---|---|
| `hello.proto` | `api/logic/logic.proto`<br>`api/comet/comet.proto` |
| `Greeter.Auth()` | `Logic.Connect()` |
| `GreeterServer` | `LogicServer` / `CometServer` |
| `NewGreeterClient()` | `NewLogicClient()` / `NewCometClient()` |

## 常见问题

### 1. protoc: command not found

安装 protoc：
```bash
# macOS
brew install protobuf

# Linux
sudo apt install protobuf-compiler
```

### 2. protoc-gen-go: program not found

安装 Go 插件：
```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 确保 $GOPATH/bin 在 PATH 中
export PATH="$PATH:$(go env GOPATH)/bin"
```

### 3. import "grpc-demo/hello" 找不到

先生成代码：
```bash
protoc --go_out=. --go-grpc_out=. hello.proto
```

### 4. 客户端连接失败

确保服务端已启动：
```bash
# 检查端口
lsof -i :50051
```

## 下一步

详细的 gRPC 教程见 `../../notes/grpc-tutorial.md`
