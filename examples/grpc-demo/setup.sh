#!/bin/bash

set -e

echo "=== gRPC Demo 运行指南 ==="
echo ""

# 检查 protoc
if ! command -v protoc &> /dev/null; then
    echo "❌ protoc 未安装"
    echo "请安装 protoc: https://grpc.io/docs/protoc-installation/"
    exit 1
fi

echo "✓ protoc 已安装"

# 安装 Go 插件
echo ""
echo "1. 安装 protoc 插件..."
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 下载依赖
echo ""
echo "2. 下载依赖..."
go mod tidy

# 生成代码
echo ""
echo "3. 生成 gRPC 代码..."
protoc --go_out=. --go-grpc_out=. hello.proto

echo ""
echo "✓ 代码生成完成！"
echo ""
echo "生成的文件："
ls -lh hello/

echo ""
echo "=== 运行步骤 ==="
echo ""
echo "终端 1 - 启动服务端："
echo "  cd server && go run main.go"
echo ""
echo "终端 2 - 运行客户端："
echo "  cd client && go run main.go"
echo ""
