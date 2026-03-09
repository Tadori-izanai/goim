package main

import (
	"log"
	"net"

	"github.com/Terry-Mao/goim/pkg/bufio"
	"github.com/Terry-Mao/goim/pkg/websocket"
)

func main() {
	// 监听 TCP 端口
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	defer listener.Close()

	log.Println("WebSocket 服务器启动在 :8080")

	for {
		// 接受连接
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("接受连接失败: %v", err)
			continue
		}

		// 处理连接（新协程）
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	log.Printf("客户端已连接: %s", conn.RemoteAddr())

	// 创建 bufio Reader 和 Writer
	rb := bufio.NewReaderSize(conn, 4096)
	wb := bufio.NewWriterSize(conn, 4096)

	// 1. 读取 HTTP 请求
	req, err := websocket.ReadRequest(rb)
	if err != nil {
		log.Printf("读取请求失败: %v", err)
		return
	}

	log.Printf("请求: %s %s", req.Method, req.RequestURI)

	// 2. 升级为 WebSocket
	ws, err := websocket.Upgrade(conn, rb, wb, req)
	if err != nil {
		log.Printf("升级失败: %v", err)
		return
	}

	log.Printf("WebSocket 连接已建立")

	// 3. 循环读取消息
	for {
		// 读取消息
		msgType, msg, err := ws.ReadMessage()
		if err != nil {
			log.Printf("读取消息失败: %v", err)
			break
		}

		log.Printf("收到消息 (type=%d): %s", msgType, string(msg))

		// 4. 回复消息
		response := []byte("服务器收到: " + string(msg))
		err = ws.WriteMessage(msgType, response)
		if err != nil {
			log.Printf("发送消息失败: %v", err)
			break
		}

		// 刷新缓冲区
		err = ws.Flush()
		if err != nil {
			log.Printf("刷新失败: %v", err)
			break
		}
	}

	log.Printf("客户端断开: %s", conn.RemoteAddr())
}
