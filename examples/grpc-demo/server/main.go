package main

import (
	"context"
	"log"
	"net"

	pb "grpc-demo/hello"

	"google.golang.org/grpc"
)

// 实现 Greeter 服务
type GreeterServer struct {
	pb.UnimplementedGreeterServer
}

// 实现 SayHello 方法
func (s *GreeterServer) SayHello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloReply, error) {
	log.Printf("收到请求: name=%s", req.Name)

	// 返回响应
	return &pb.HelloReply{
		Message: "Hello, " + req.Name + "!",
	}, nil
}

// 实现 Auth 方法（类似 goim 的 Connect）
func (s *GreeterServer) Auth(ctx context.Context, req *pb.AuthRequest) (*pb.AuthReply, error) {
	log.Printf("收到鉴权请求: token=%s, user_id=%d", req.Token, req.UserId)

	// 简单验证：token 不为空就通过
	if req.Token != "" {
		return &pb.AuthReply{
			Success:    true,
			SessionKey: "session_" + req.Token,
		}, nil
	}

	return &pb.AuthReply{
		Success: false,
	}, nil
}

var _ pb.GreeterServer = (*GreeterServer)(nil)

func main() {
	// 1. 监听端口
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}

	// 2. 创建 gRPC 服务器
	grpcServer := grpc.NewServer()

	// 3. 注册服务
	pb.RegisterGreeterServer(grpcServer, &GreeterServer{})

	log.Println("gRPC 服务器启动在 :50051")

	// 4. 启动服务
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}
