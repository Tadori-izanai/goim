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
	OpGroupChatMsg   = 2002
)

// group-chat-demo: 完整的群聊端到端测试
//
// 启动三个用户 (alice, bob, charlie)，测试:
//  1. 注册 + 登录
//  2. Alice 创建群
//  3. Bob、Charlie 加入群
//  4. 查看群成员列表
//  5. Bob、Charlie WebSocket 连接 Comet
//  6. Alice 发群消息 → Bob、Charlie WebSocket 实时收到
//  7. 拉取群历史消息
//  8. 查询已加入的群列表
//  9. Charlie 退出群
// 10. 验证非成员不能发消息
//
// 用法: go run examples/group-chat-demo/main.go
func main() {
	gatewayAddr := flag.String("gateway", "http://127.0.0.1:3200", "Gateway HTTP 地址")
	flag.Parse()

	gw := *gatewayAddr

	// 1. 注册
	log.Println("=== 注册用户 ===")
	register(gw, "alice", "123456")
	register(gw, "bob", "123456")
	register(gw, "charlie", "123456")

	// 2. 登录
	log.Println("=== 登录 ===")
	aliceToken, aliceID, _ := login(gw, "alice", "123456")
	bobToken, bobID, bobWs := login(gw, "bob", "123456")
	charlieToken, charlieID, charlieWs := login(gw, "charlie", "123456")
	log.Printf("Alice ID=%d, Bob ID=%d, Charlie ID=%d", aliceID, bobID, charlieID)

	// 3. Alice 创建群
	log.Println("=== Alice 创建群 ===")
	groupID := createGroup(gw, aliceToken, "测试群")
	log.Printf("群创建成功, ID=%d", groupID)

	// 4. Bob、Charlie 加入群
	log.Println("=== Bob、Charlie 加入群 ===")
	joinGroup(gw, bobToken, groupID)
	log.Println("Bob 加入群成功")
	joinGroup(gw, charlieToken, groupID)
	log.Println("Charlie 加入群成功")

	// 5. 查看群成员
	log.Println("=== 查看群成员 ===")
	members := listGroupMembers(gw, aliceToken, groupID)
	log.Printf("群成员: %s", members)

	// 6. 查看已加入的群列表
	log.Println("=== 查看 Bob 已加入的群 ===")
	groups := listJoinedGroups(gw, bobToken)
	log.Printf("Bob 的群列表: %s", groups)

	// 7. Bob、Charlie 连接 WebSocket
	log.Println("=== Bob、Charlie 连接 Comet ===")
	bobConn := connectComet(bobWs, bobToken)
	defer bobConn.Close()
	charlieConn := connectComet(charlieWs, charlieToken)
	defer charlieConn.Close()

	go heartbeatLoop(bobConn)
	go heartbeatLoop(charlieConn)

	bobMsgCh := make(chan string, 10)
	charlieMsgCh := make(chan string, 10)
	go receiveLoop(bobConn, bobMsgCh)
	go receiveLoop(charlieConn, charlieMsgCh)

	// 8. Alice 发群消息
	log.Println("=== Alice 发群消息 ===")
	sendGroupMessage(gw, aliceToken, groupID, "大家好！")
	sendGroupMessage(gw, aliceToken, groupID, "第二条群消息")
	log.Println("发送完成")

	// 9. 等待 Bob 和 Charlie 收到推送
	log.Println("=== 等待 Bob 收到推送 ===")
	waitGroupMessages(bobMsgCh, "Bob", 2)

	log.Println("=== 等待 Charlie 收到推送 ===")
	waitGroupMessages(charlieMsgCh, "Charlie", 2)

	// 10. 拉取群历史消息
	log.Println("=== 拉取群历史消息 ===")
	history := getGroupHistory(gw, aliceToken, groupID, 0, 50)
	log.Printf("群历史消息: %s", history)

	// 11. Charlie 退出群
	log.Println("=== Charlie 退出群 ===")
	quitGroup(gw, charlieToken, groupID)
	log.Println("Charlie 退出群成功")

	// 12. 验证退群后不能发消息
	log.Println("=== 验证非成员不能发群消息 ===")
	err := sendGroupMessageRaw(gw, charlieToken, groupID, "should fail")
	log.Printf("非成员发群消息结果: %s", err)

	// 13. 验证退群后不能拉历史
	log.Println("=== 验证非成员不能拉群历史 ===")
	errHist := getGroupHistoryRaw(gw, charlieToken, groupID, 0, 50)
	log.Printf("非成员拉群历史结果: %s", errHist)

	log.Println("=== 测试完成 ===")
}

func waitGroupMessages(ch <-chan string, who string, expect int) {
	timeout := time.After(5 * time.Second)
	received := 0
	for received < expect {
		select {
		case msg := <-ch:
			received++
			var push struct {
				MsgID       string `json:"msg_id"`
				GroupID     int64  `json:"group_id"`
				From        int64  `json:"from"`
				ContentType int8   `json:"content_type"`
				Content     string `json:"content"`
				Timestamp   int64  `json:"timestamp"`
			}
			if err := json.Unmarshal([]byte(msg), &push); err != nil {
				log.Printf("%s 收到推送 [%d/%d] (raw): %s", who, received, expect, msg)
			} else {
				log.Printf("%s 收到推送 [%d/%d]: group=%d, from=%d, content=%q",
					who, received, expect, push.GroupID, push.From, push.Content)
			}
		case <-timeout:
			log.Printf("%s 超时，共收到 %d/%d 条推送", who, received, expect)
			return
		}
	}
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

func createGroup(gw, token, name string) int64 {
	result := apiPost(gw+"/goim/group", token, map[string]string{"name": name})
	if result.Code != 0 {
		log.Fatalf("创建群失败: %s", result.Message)
	}
	var data struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(result.Data, &data)
	return data.ID
}

func joinGroup(gw, token string, groupID int64) {
	result := apiPost(fmt.Sprintf("%s/goim/group/%d/join", gw, groupID), token, nil)
	if result.Code != 0 {
		log.Fatalf("加入群失败: %s", result.Message)
	}
}

func quitGroup(gw, token string, groupID int64) {
	result := apiPost(fmt.Sprintf("%s/goim/group/%d/quit", gw, groupID), token, nil)
	if result.Code != 0 {
		log.Fatalf("退出群失败: %s", result.Message)
	}
}

func listGroupMembers(gw, token string, groupID int64) string {
	result := apiGet(fmt.Sprintf("%s/goim/group/%d/members", gw, groupID), token)
	return string(result.Data)
}

func listJoinedGroups(gw, token string) string {
	result := apiGet(gw+"/goim/user/group", token)
	return string(result.Data)
}

func sendGroupMessage(gw, token string, groupID int64, content string) {
	err := sendGroupMessageRaw(gw, token, groupID, content)
	if err != "" {
		log.Fatalf("发群消息失败: %s", err)
	}
}

func sendGroupMessageRaw(gw, token string, groupID int64, content string) string {
	result := apiPost(fmt.Sprintf("%s/goim/group/%d/chat", gw, groupID), token, map[string]any{
		"content_type": 1, "content": content,
	})
	if result.Code != 0 {
		return fmt.Sprintf("code=%d, message=%s", result.Code, result.Message)
	}
	return ""
}

func getGroupHistory(gw, token string, groupID int64, sinceMs int64, limit int) string {
	result := apiGet(fmt.Sprintf("%s/goim/group/%d/chat?since=%d&limit=%d", gw, groupID, sinceMs, limit), token)
	return string(result.Data)
}

func getGroupHistoryRaw(gw, token string, groupID int64, sinceMs int64, limit int) string {
	result := apiGet(fmt.Sprintf("%s/goim/group/%d/chat?since=%d&limit=%d", gw, groupID, sinceMs, limit), token)
	if result.Code != 0 {
		return fmt.Sprintf("code=%d, message=%s", result.Code, result.Message)
	}
	return ""
}

// --- Comet WebSocket ---

func connectComet(wsAddr, token string) *websocket.Conn {
	conn, _, err := websocket.DefaultDialer.Dial(wsAddr, nil)
	if err != nil {
		log.Fatalf("WebSocket 连接失败: %v", err)
	}

	authBody, _ := json.Marshal(map[string]any{
		"token":    token,
		"room_id":  "",
		"platform": "web",
		"accepts":  []int32{1000, 1001, 1002, OpGroupChatMsg},
	})
	packet := encodePacket(1, OpAuth, 1, authBody)
	conn.WriteMessage(websocket.BinaryMessage, packet)

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
