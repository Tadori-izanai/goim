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
	OpSingleChatMsg  = 2001
)

// chat-demo: 完整的单聊端到端测试
//
// 启动两个用户 (alice, bob)，测试:
//  1. 注册 + 登录
//  2. 添加好友
//  3. Bob WebSocket 连接 Comet
//  4. Alice 发消息给 Bob → Bob WebSocket 实时收到
//  5. Alice 拉取历史消息
//
// 用法: go run examples/chat-demo/main.go
func main() {
	gatewayAddr := flag.String("gateway", "http://127.0.0.1:3200", "Gateway HTTP 地址")
	flag.Parse()

	gw := *gatewayAddr

	// 1. 注册
	log.Println("=== 注册用户 ===")
	register(gw, "alice", "123456")
	register(gw, "bob", "123456")

	// 2. 登录
	log.Println("=== 登录 ===")
	aliceToken, aliceID, _ := login(gw, "alice", "123456")
	bobToken, bobID, wsAddr := login(gw, "bob", "123456")
	log.Printf("Alice ID=%d, Bob ID=%d", aliceID, bobID)

	// 3. Alice 添加 Bob 为好友
	log.Println("=== 添加好友 ===")
	addFriend(gw, aliceToken, bobID)
	log.Println("Alice 添加 Bob 为好友成功")

	// 验证好友列表
	friends := listFriends(gw, aliceToken)
	log.Printf("Alice 的好友列表: %s", friends)

	// 4. Bob 连接 WebSocket
	log.Println("=== Bob 连接 Comet ===")
	conn := connectComet(wsAddr, bobToken)
	defer conn.Close()

	// 启动 Bob 的心跳
	go heartbeatLoop(conn)

	// 启动 Bob 的消息接收（异步）
	msgCh := make(chan string, 10)
	go func() {
		receiveLoop(conn, msgCh)
	}()

	// 5. Alice 发消息给 Bob
	log.Println("=== Alice 发消息 ===")
	sendMessage(gw, aliceToken, bobID, "你好 Bob！")
	sendMessage(gw, aliceToken, bobID, "第二条消息")
	log.Println("发送完成")

	// 6. 等待 Bob 收到 WebSocket 推送
	log.Println("=== 等待 Bob 收到推送 ===")
	timeout := time.After(5 * time.Second)
	received := 0
	for received < 2 {
		select {
		case msg := <-msgCh:
			received++
			log.Printf("Bob 收到推送 [%d/2]: %s", received, msg)
		case <-timeout:
			log.Printf("超时，共收到 %d 条推送", received)
			goto history
		}
	}

history:
	// 7. Alice 拉取历史消息
	log.Println("=== Alice 拉取历史 ===")
	messages := getHistory(gw, aliceToken, 0, 50)
	log.Printf("历史消息: %s", messages)

	// 8. 查询用户信息
	log.Println("=== 查询用户信息 ===")
	userInfo := getUserInfo(gw, aliceToken, fmt.Sprintf("%d,%d", aliceID, bobID))
	log.Printf("用户信息: %s", userInfo)

	// 9. 删除好友
	log.Println("=== 删除好友 ===")
	removeFriend(gw, aliceToken, bobID)
	log.Println("Alice 删除 Bob 好友关系成功")

	// 验证不能再发消息
	log.Println("=== 验证非好友不能发消息 ===")
	err := sendMessageRaw(gw, aliceToken, bobID, "should fail")
	log.Printf("非好友发消息结果: %s", err)

	log.Println("=== 测试完成 ===")
}

// --- Gateway HTTP helpers ---

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func apiPost(url, token string, body any) *apiResponse {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("POST %s 失败: %v", url, err)
	}
	defer resp.Body.Close()
	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	return &result
}

func apiGet(url, token string) *apiResponse {
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("GET %s 失败: %v", url, err)
	}
	defer resp.Body.Close()
	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	return &result
}

func apiDelete(url, token string) *apiResponse {
	req, _ := http.NewRequest("DELETE", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("DELETE %s 失败: %v", url, err)
	}
	defer resp.Body.Close()
	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	return &result
}

func register(gw, username, password string) {
	result := apiPost(gw+"/goim/auth/register", "", map[string]string{
		"username": username, "password": password,
	})
	if result.Code != 0 {
		log.Printf("注册 %s: %s (可能已存在)", username, result.Message)
	} else {
		log.Printf("注册 %s 成功", username)
	}
}

func login(gw, username, password string) (token string, userID int64, wsAddr string) {
	result := apiPost(gw+"/goim/auth/login", "", map[string]string{
		"username": username, "password": password,
	})
	if result.Code != 0 {
		log.Fatalf("登录 %s 失败: %s", username, result.Message)
	}
	var data struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
		Nodes struct {
			WsPort int      `json:"ws_port"`
			Nodes  []string `json:"nodes"`
		} `json:"nodes"`
	}
	json.Unmarshal(result.Data, &data)

	host := "127.0.0.1"
	if len(data.Nodes.Nodes) > 0 {
		host = data.Nodes.Nodes[0]
	}
	wsAddr = fmt.Sprintf("ws://%s:%d/sub", host, data.Nodes.WsPort)
	log.Printf("登录 %s 成功 (ID=%d)", username, data.ID)
	return data.Token, data.ID, wsAddr
}

func addFriend(gw, token string, friendID int64) {
	result := apiPost(fmt.Sprintf("%s/goim/friend/%d", gw, friendID), token, nil)
	if result.Code != 0 {
		log.Fatalf("添加好友失败: %s", result.Message)
	}
}

func removeFriend(gw, token string, friendID int64) {
	result := apiDelete(fmt.Sprintf("%s/goim/friend/%d", gw, friendID), token)
	if result.Code != 0 {
		log.Fatalf("删除好友失败: %s", result.Message)
	}
}

func listFriends(gw, token string) string {
	result := apiGet(gw+"/goim/friend", token)
	return string(result.Data)
}

func sendMessage(gw, token string, toID int64, content string) {
	result := sendMessageRaw(gw, token, toID, content)
	if result != "" {
		log.Fatalf("发消息失败: %s", result)
	}
}

func sendMessageRaw(gw, token string, toID int64, content string) string {
	result := apiPost(gw+"/goim/chat", token, map[string]any{
		"to": toID, "content_type": 1, "content": content,
	})
	if result.Code != 0 {
		return fmt.Sprintf("code=%d, message=%s", result.Code, result.Message)
	}
	return ""
}

func getHistory(gw, token string, sinceMs int64, limit int) string {
	result := apiGet(fmt.Sprintf("%s/goim/chat?since=%d&limit=%d", gw, sinceMs, limit), token)
	return string(result.Data)
}

func getUserInfo(gw, token, ids string) string {
	result := apiGet(fmt.Sprintf("%s/goim/user/info?ids=%s", gw, ids), token)
	return string(result.Data)
}

// --- Comet WebSocket ---

func connectComet(wsAddr, token string) *websocket.Conn {
	conn, _, err := websocket.DefaultDialer.Dial(wsAddr, nil)
	if err != nil {
		log.Fatalf("WebSocket 连接失败: %v", err)
	}

	// 发送 JWT 鉴权，accepts 包含 OpSingleChatMsg
	authBody, _ := json.Marshal(map[string]any{
		"token":    token,
		"room_id":  "",
		"platform": "web",
		"accepts":  []int32{1000, 1001, 1002, OpSingleChatMsg},
	})
	packet := encodePacket(1, OpAuth, 1, authBody)
	conn.WriteMessage(websocket.BinaryMessage, packet)

	// 等待鉴权响应
	_, data, err := conn.ReadMessage()
	if err != nil || decodeOp(data) != OpAuthReply {
		log.Fatalf("Comet 鉴权失败")
	}
	log.Println("Comet 鉴权成功")
	return conn
}

func heartbeatLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		packet := encodePacket(1, OpHeartbeat, 1, nil)
		if err := conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
			return
		}
	}
}

func receiveLoop(conn *websocket.Conn, msgCh chan<- string) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if len(data) < 16 {
			continue
		}
		op := decodeOp(data)
		switch op {
		case OpHeartbeatReply:
			// ignore
		case OpRaw:
			// 批量消息：解析子包
			offset := 16
			for offset < len(data) {
				subPackLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
				subHeaderLen := int(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
				subBody := string(data[offset+subHeaderLen : offset+subPackLen])
				msgCh <- subBody
				offset += subPackLen
			}
		default:
			packLen := int(binary.BigEndian.Uint32(data[0:4]))
			headerLen := int(binary.BigEndian.Uint16(data[4:6]))
			if packLen > headerLen {
				body := string(data[headerLen:packLen])
				msgCh <- body
			}
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
