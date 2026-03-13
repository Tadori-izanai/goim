package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	OpAuth           = 7
	OpAuthReply      = 8
	OpHeartbeat      = 2
	OpHeartbeatReply = 3
	OpRaw            = 9
)

// go run examples/jwt-client/main.go -user alice -pass 123456 -room "live://1000"
func main() {
	gatewayAddr := flag.String("gateway", "http://127.0.0.1:3200", "Gateway HTTP 地址")
	username := flag.String("user", "alice", "用户名")
	password := flag.String("pass", "123456", "密码")
	room := flag.String("room", "live://1000", "房间 ID")
	flag.Parse()

	// 1. 注册
	log.Printf("=== 注册用户 %s ===", *username)
	if err := register(*gatewayAddr, *username, *password); err != nil {
		log.Printf("注册: %v (可能已存在，继续登录)", err)
	} else {
		log.Println("注册成功")
	}

	// 2. 登录 → 拿到 JWT + comet 地址
	log.Printf("=== 登录 %s ===", *username)
	token, wsAddr, err := login(*gatewayAddr, *username, *password)
	if err != nil {
		log.Fatalf("登录失败: %v", err)
	}
	log.Printf("登录成功, token: %s...%s", token[:10], token[len(token)-10:])
	log.Printf("Comet WS 地址: %s", wsAddr)

	// 3. WebSocket 连接 Comet
	log.Printf("=== 连接 Comet ===")
	conn, _, err := websocket.DefaultDialer.Dial(wsAddr, nil)
	if err != nil {
		log.Fatalf("WebSocket 连接失败: %v", err)
	}
	defer conn.Close()
	log.Println("WebSocket 连接成功")

	// 4. 发送 JWT 鉴权
	if err := sendAuth(conn, token, *room); err != nil {
		log.Fatalf("鉴权失败: %v", err)
	}

	// 5. 心跳 + 收消息
	go heartbeatLoop(conn)
	receiveLoop(conn)
}

// --- Gateway HTTP API ---

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func register(gateway, username, password string) error {
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	resp, err := http.Post(gateway+"/goim/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Code != 0 {
		return fmt.Errorf("code=%d, message=%s", result.Code, result.Message)
	}
	return nil
}

func login(gateway, username, password string) (token string, wsAddr string, err error) {
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	resp, err := http.Post(gateway+"/goim/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Code != 0 {
		return "", "", fmt.Errorf("code=%d, message=%s", result.Code, result.Message)
	}
	var loginData struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
		Nodes struct {
			Domain  string   `json:"domain"`
			WsPort  int      `json:"ws_port"`
			WssPort int      `json:"wss_port"`
			Nodes   []string `json:"nodes"`
		} `json:"nodes"`
	}
	json.Unmarshal(result.Data, &loginData)
	token = loginData.Token

	log.Printf("(userID: %d)", loginData.ID)

	// 构造 ws 地址：优先用 nodes 列表中的第一个 IP
	host := "127.0.0.1"
	if len(loginData.Nodes.Nodes) > 0 {
		host = loginData.Nodes.Nodes[0]
	}
	wsAddr = fmt.Sprintf("ws://%s:%d/sub", host, loginData.Nodes.WsPort)
	return
}

// --- Comet WebSocket ---

func sendAuth(conn *websocket.Conn, token, roomID string) error {
	authBody := struct {
		Token    string  `json:"token"`
		RoomID   string  `json:"room_id"`
		Platform string  `json:"platform"`
		Accepts  []int32 `json:"accepts"`
	}{
		Token:    token,
		RoomID:   roomID,
		Platform: "web",
		Accepts:  []int32{1000, 1001, 1002},
	}

	body, _ := json.Marshal(authBody)
	packet := encodePacket(1, OpAuth, 1, body)

	if err := conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
		return err
	}
	log.Printf("-> 发送 JWT 鉴权 (room=%s)", roomID)

	// 等待鉴权响应
	_, data, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	if decodeOp(data) == OpAuthReply {
		log.Println("鉴权成功")
		return nil
	}
	return fmt.Errorf("鉴权失败, Op=%d", decodeOp(data))
}

func heartbeatLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		packet := encodePacket(1, OpHeartbeat, 1, nil)
		if err := conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
			log.Printf("心跳发送失败: %v", err)
			return
		}
		log.Println("-> 心跳")
	}
}

func receiveLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("连接断开: %v", err)
			return
		}
		handleMessage(data)
	}
}

func handleMessage(data []byte) {
	if len(data) < 16 {
		return
	}
	packLen := binary.BigEndian.Uint32(data[0:4])
	headerLen := binary.BigEndian.Uint16(data[4:6])
	op := binary.BigEndian.Uint32(data[8:12])

	switch op {
	case OpHeartbeatReply:
		log.Println("<- 心跳响应")
	case OpRaw:
		log.Println("<- 批量消息:")
		offset := 16
		for offset < len(data) {
			subPackLen := binary.BigEndian.Uint32(data[offset : offset+4])
			subHeaderLen := binary.BigEndian.Uint16(data[offset+4 : offset+6])
			subBody := data[offset+int(subHeaderLen) : offset+int(subPackLen)]
			log.Printf("   %s", string(subBody))
			offset += int(subPackLen)
		}
	default:
		if packLen > 16 {
			body := data[headerLen:packLen]
			log.Printf("<- 消息 Op=%d: %s", op, string(body))
		}
	}
}

func encodePacket(ver uint16, op uint32, seq uint32, body []byte) []byte {
	packLen := 16 + len(body)
	buf := make([]byte, packLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(packLen))
	binary.BigEndian.PutUint16(buf[4:6], 16)
	binary.BigEndian.PutUint16(buf[6:8], ver)
	binary.BigEndian.PutUint32(buf[8:12], op)
	binary.BigEndian.PutUint32(buf[12:16], seq)
	if len(body) > 0 {
		copy(buf[16:], body)
	}
	return buf
}

func decodeOp(data []byte) uint32 {
	if len(data) < 16 {
		return 0
	}
	return binary.BigEndian.Uint32(data[8:12])
}
