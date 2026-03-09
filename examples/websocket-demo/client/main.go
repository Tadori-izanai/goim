package main

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	// 1. 连接到 WebSocket 服务器
	url := "ws://localhost:8080/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer conn.Close()

	log.Println("✓ 已连接到服务器")

	// 2. 发送消息
	messages := []string{"Hello", "World", "WebSocket"}
	for _, msg := range messages {
		err := conn.WriteMessage(websocket.TextMessage, []byte(msg))
		if err != nil {
			log.Printf("发送失败: %v", err)
			break
		}
		log.Printf("→ 发送: %s", msg)

		// 3. 接收响应
		_, response, err := conn.ReadMessage()
		if err != nil {
			log.Printf("接收失败: %v", err)
			break
		}
		log.Printf("← 收到: %s", string(response))

		time.Sleep(1 * time.Second)
	}

	log.Println("客户端退出")
}
