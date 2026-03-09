package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// 升级器：将 HTTP 连接升级为 WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源（生产环境需要严格检查）
	},
}

// 处理 WebSocket 连接
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 1. 升级 HTTP 连接为 WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("升级失败: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("客户端已连接: %s", conn.RemoteAddr())

	// 2. 循环读取客户端消息
	for {
		// 读取消息
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("读取失败: %v", err)
			break
		}

		log.Printf("收到消息: %s", string(message))

		// 3. 回复消息
		response := fmt.Sprintf("服务器收到: %s", string(message))
		err = conn.WriteMessage(messageType, []byte(response))
		if err != nil {
			log.Printf("发送失败: %v", err)
			break
		}
	}

	log.Printf("客户端断开: %s", conn.RemoteAddr())
}

func main() {
	// 注册 WebSocket 路由
	http.HandleFunc("/ws", handleWebSocket)

	log.Println("WebSocket 服务器启动在 :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
