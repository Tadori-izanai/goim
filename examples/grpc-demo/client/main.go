package main

import (
	"context"
	"log"
	"time"

	pb "grpc-demo/hello"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// 1. 连接到 gRPC 服务器
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer conn.Close()

	// 2. 创建客户端
	client := pb.NewGreeterClient(conn)

	// 3. 调用 SayHello 方法
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	resp1, err := client.SayHello(ctx, &pb.HelloRequest{
		Name: "张三",
	})
	if err != nil {
		log.Fatalf("调用失败: %v", err)
	}
	log.Printf("收到响应: %s", resp1.Message)

	// 4. 调用 Auth 方法（类似 goim 的 Connect）
	resp2, err := client.Auth(ctx, &pb.AuthRequest{
		Token:  "abc123",
		UserId: 123,
	})
	if err != nil {
		log.Fatalf("鉴权失败: %v", err)
	}
	log.Printf("鉴权结果: success=%v, session_key=%s", resp2.Success, resp2.SessionKey)
}
