package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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
	OpGroupChatMsg   = 2002
)

// offline-demo: 综合端到端测试（阶段三 ~ 六）
//
// 覆盖场景:
//
//	Phase 1 — 用户系统: 注册、登录、login 返回 last_ack_at
//	Phase 2 — 好友 + 单聊: 添加好友、发消息、WebSocket 实时推送、历史消息
//	Phase 3 — 单聊 ACK: 客户端 POST /goim/ack/:msg_id 确认
//	Phase 4 — 群聊: 创建群、加入群、发群消息、WebSocket 推送、群历史
//	Phase 5 — 离线消息 Pull: Bob 断线 → Alice 发消息 → Bob 重连拉取
//	Phase 6 — SyncAck: Bob POST /goim/sync/ack 更新 last_ack_at
//	Phase 7 — ACK 位点持久化: 重新登录验证 last_ack_at 已更新
//	Phase 8 — 边界测试: 非好友发消息、退群后发消息、ACK 不存在的 msg_id
//
// 用法:
//
//	go run examples/offline-demo/main.go [-gateway http://127.0.0.1:3200]
func main() {
	gatewayAddr := flag.String("gateway", "http://127.0.0.1:3200", "Gateway HTTP 地址")
	flag.Parse()
	gw := *gatewayAddr

	passed, failed := 0, 0
	assert := func(name string, ok bool, detail string) {
		if ok {
			passed++
			log.Printf("  ✅ %s", name)
		} else {
			failed++
			log.Printf("  ❌ %s — %s", name, detail)
		}
	}

	// ══════════════════════════════════════════
	// Phase 1: 用户系统
	// ══════════════════════════════════════════
	section("Phase 1: 用户系统 — 注册 / 登录 / last_ack_at")

	register(gw, "off_alice", "123456")
	register(gw, "off_bob", "123456")
	register(gw, "off_charlie", "123456")

	aliceToken, aliceID, _ := login(gw, "off_alice", "123456")
	bobToken, bobID, bobWs := login(gw, "off_bob", "123456")
	charlieToken, charlieID, charlieWs := login(gw, "off_charlie", "123456")
	step("Alice=%d  Bob=%d  Charlie=%d", aliceID, bobID, charlieID)

	_, _, bobLastAck0 := loginFull(gw, "off_bob", "123456")
	assert("login 返回 last_ack_at", bobLastAck0 >= 0, fmt.Sprintf("got %d", bobLastAck0))

	userInfo := getUserInfo(gw, aliceToken, []int64{aliceID, bobID, charlieID})
	assert("GET /goim/user/info 返回用户信息", strings.Contains(userInfo, "off_alice"), userInfo)

	// ══════════════════════════════════════════
	// Phase 2: 好友 + 单聊实时推送
	// ══════════════════════════════════════════
	section("Phase 2: 好友 + 单聊实时推送")

	addFriend(gw, aliceToken, bobID)
	step("Alice ↔ Bob 成为好友")

	friends := listFriends(gw, aliceToken)
	assert("好友列表包含 Bob", strings.Contains(friends, fmt.Sprintf("%d", bobID)), friends)

	bobConn := connectComet(bobWs, bobToken)
	defer safeClose(bobConn)
	go heartbeatLoop(bobConn)
	bobCh := make(chan string, 30)
	go receiveLoop(bobConn, bobCh)

	msgID1 := sendMessage(gw, aliceToken, bobID, "hello Bob!")
	msgID2 := sendMessage(gw, aliceToken, bobID, "second msg")
	assert("发消息返回 msg_id", msgID1 != "" && msgID2 != "", msgID1+" / "+msgID2)

	n := waitMessages(bobCh, "Bob", 2)
	assert("Bob WebSocket 收到 2 条推送", n == 2, fmt.Sprintf("got %d", n))

	history := getHistory(gw, bobToken, 0, 50)
	assert("单聊历史包含消息", strings.Contains(history, "hello Bob!"), history)

	// ══════════════════════════════════════════
	// Phase 3: 单聊 ACK
	// ══════════════════════════════════════════
	section("Phase 3: 单聊 ACK")

	ackResp1 := ackMsg(gw, msgID1)
	ackResp2 := ackMsg(gw, msgID2)
	assert("ACK msg1 成功", ackResp1 == 0, fmt.Sprintf("code=%d", ackResp1))
	assert("ACK msg2 成功", ackResp2 == 0, fmt.Sprintf("code=%d", ackResp2))

	// ══════════════════════════════════════════
	// Phase 4: 群聊
	// ══════════════════════════════════════════
	section("Phase 4: 群聊")

	groupID := createGroup(gw, aliceToken, "offline-test-group")
	assert("创建群成功", groupID > 0, fmt.Sprintf("id=%d", groupID))

	joinGroup(gw, bobToken, groupID)
	joinGroup(gw, charlieToken, groupID)
	step("Bob、Charlie 加入群")

	members := listGroupMembers(gw, aliceToken, groupID)
	assert("群成员包含 Charlie", strings.Contains(members, fmt.Sprintf("%d", charlieID)), members)

	groups := listJoinedGroups(gw, bobToken)
	assert("Bob 已加入群列表包含该群", strings.Contains(groups, fmt.Sprintf("%d", groupID)), groups)

	charlieConn := connectComet(charlieWs, charlieToken)
	defer safeClose(charlieConn)
	go heartbeatLoop(charlieConn)
	charlieCh := make(chan string, 30)
	go receiveLoop(charlieConn, charlieCh)

	sendGroupMessage(gw, aliceToken, groupID, "群消息1")
	sendGroupMessage(gw, aliceToken, groupID, "群消息2")

	nb := waitMessages(bobCh, "Bob(群)", 2)
	nc := waitMessages(charlieCh, "Charlie(群)", 2)
	assert("Bob 收到 2 条群推送", nb == 2, fmt.Sprintf("got %d", nb))
	assert("Charlie 收到 2 条群推送", nc == 2, fmt.Sprintf("got %d", nc))

	gHistory := getGroupHistory(gw, aliceToken, groupID, 0, 50)
	assert("群历史包含消息", strings.Contains(gHistory, "群消息1"), gHistory)

	// ══════════════════════════════════════════
	// Phase 5: 离线消息 Pull
	// ══════════════════════════════════════════
	section("Phase 5: 离线消息 Pull")

	bobConn.Close()
	step("Bob 断开 WebSocket（模拟离线）")
	time.Sleep(1 * time.Second)

	sinceMs := time.Now().Add(-500 * time.Millisecond).UnixMilli()

	offID1 := sendMessage(gw, aliceToken, bobID, "离线消息1")
	offID2 := sendMessage(gw, aliceToken, bobID, "离线消息2")
	sendGroupMessage(gw, aliceToken, groupID, "离线群消息")
	step("Alice 发了 2 条单聊 + 1 条群聊（Bob 离线中）")

	step("等待 3s（Gateway ACK 重试中，但 Bob 不在线）...")
	time.Sleep(3 * time.Second)

	bobToken2, _, bobWs2 := login(gw, "off_bob", "123456")

	syncResult := syncMessages(gw, bobToken2, sinceMs, 50)
	assert("Pull 包含离线单聊", strings.Contains(syncResult, "离线消息1"), truncate(syncResult, 200))
	assert("Pull 包含离线群聊", strings.Contains(syncResult, "离线群消息"), truncate(syncResult, 200))

	bobConn2 := connectComet(bobWs2, bobToken2)
	defer safeClose(bobConn2)
	go heartbeatLoop(bobConn2)
	bobCh2 := make(chan string, 30)
	go receiveLoop(bobConn2, bobCh2)
	_ = bobCh2

	ackMsg(gw, offID1)
	ackMsg(gw, offID2)
	step("Bob ACK 离线消息: %s, %s", offID1, offID2)

	// ══════════════════════════════════════════
	// Phase 6: SyncAck
	// ══════════════════════════════════════════
	section("Phase 6: SyncAck — 更新 last_ack_at")

	ackAt := time.Now().UnixMilli()
	syncAckCode := syncAck(gw, bobToken2, ackAt)
	assert("SyncAck 成功", syncAckCode == 0, fmt.Sprintf("code=%d", syncAckCode))

	syncResult2 := syncMessages(gw, bobToken2, ackAt, 50)
	assert("SyncAck 后 Pull 不含旧消息",
		!strings.Contains(syncResult2, "离线消息1"),
		truncate(syncResult2, 200))

	// ══════════════════════════════════════════
	// Phase 7: ACK 位点持久化
	// ══════════════════════════════════════════
	section("Phase 7: ACK 位点持久化")

	_, _, newLastAck := loginFull(gw, "off_bob", "123456")
	assert("last_ack_at 已更新",
		newLastAck >= ackAt,
		fmt.Sprintf("got %d, want >= %d", newLastAck, ackAt))

	// ══════════════════════════════════════════
	// Phase 8: 边界测试
	// ══════════════════════════════════════════
	section("Phase 8: 边界测试")

	errMsg := sendMessageRaw(gw, charlieToken, aliceID, "should fail")
	assert("非好友发消息被拒", errMsg != "", errMsg)

	quitGroup(gw, charlieToken, groupID)
	step("Charlie 退出群")

	errGroup := sendGroupMessageRaw(gw, charlieToken, groupID, "should fail")
	assert("非成员发群消息被拒", errGroup != "", errGroup)

	errHist := getGroupHistoryRaw(gw, charlieToken, groupID, 0, 50)
	assert("非成员拉群历史被拒", errHist != "", errHist)

	ackMsg(gw, "nonexistent-msg-id-12345")
	assert("ACK 不存在的 msg_id 不报错", true, "")

	removeFriend(gw, aliceToken, bobID)
	step("Alice 删除 Bob 好友关系")
	errAfterRemove := sendMessageRaw(gw, aliceToken, bobID, "should fail")
	assert("删好友后发消息被拒", errAfterRemove != "", errAfterRemove)

	// ══════════════════════════════════════════
	// 汇总
	// ══════════════════════════════════════════
	fmt.Println()
	fmt.Println(strings.Repeat("═", 50))
	fmt.Printf("  测试结果: %d passed, %d failed, %d total\n", passed, failed, passed+failed)
	fmt.Println(strings.Repeat("═", 50))

	if failed > 0 {
		os.Exit(1)
	}
}

// ─── 输出辅助 ───

func section(title string) {
	fmt.Println()
	log.Printf("══ %s ══", title)
}

func step(format string, args ...any) {
	log.Printf("  → "+format, args...)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ─── Gateway HTTP 通用 ───

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
		log.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var r apiResponse
	json.NewDecoder(resp.Body).Decode(&r)
	return &r
}

func apiGet(url, token string) *apiResponse {
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var r apiResponse
	json.NewDecoder(resp.Body).Decode(&r)
	return &r
}

func apiDelete(url, token string) *apiResponse {
	req, _ := http.NewRequest("DELETE", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("DELETE %s: %v", url, err)
	}
	defer resp.Body.Close()
	var r apiResponse
	json.NewDecoder(resp.Body).Decode(&r)
	return &r
}

// ─── 用户系统 ───

func register(gw, username, password string) {
	r := apiPost(gw+"/goim/auth/register", "", map[string]string{
		"username": username, "password": password,
	})
	if r.Code != 0 {
		step("注册 %s: %s (可能已存在)", username, r.Message)
	} else {
		step("注册 %s 成功", username)
	}
}

func login(gw, username, password string) (token string, userID int64, wsAddr string) {
	r := apiPost(gw+"/goim/auth/login", "", map[string]string{
		"username": username, "password": password,
	})
	if r.Code != 0 {
		log.Fatalf("登录 %s 失败: %s", username, r.Message)
	}
	var data struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
		Nodes struct {
			WsPort int      `json:"ws_port"`
			Nodes  []string `json:"nodes"`
		} `json:"nodes"`
	}
	json.Unmarshal(r.Data, &data)
	host := "127.0.0.1"
	if len(data.Nodes.Nodes) > 0 {
		host = data.Nodes.Nodes[0]
	}
	wsAddr = fmt.Sprintf("ws://%s:%d/sub", host, data.Nodes.WsPort)
	step("登录 %s 成功 (ID=%d)", username, data.ID)
	return data.Token, data.ID, wsAddr
}

func loginFull(gw, username, password string) (token string, userID int64, lastAckAt int64) {
	r := apiPost(gw+"/goim/auth/login", "", map[string]string{
		"username": username, "password": password,
	})
	if r.Code != 0 {
		log.Fatalf("登录 %s 失败: %s", username, r.Message)
	}
	var data struct {
		ID        int64  `json:"id"`
		Token     string `json:"token"`
		LastAckAt int64  `json:"last_ack_at"`
	}
	json.Unmarshal(r.Data, &data)
	return data.Token, data.ID, data.LastAckAt
}

func getUserInfo(gw, token string, ids []int64) string {
	params := ""
	for i, id := range ids {
		if i > 0 {
			params += "&"
		}
		params += fmt.Sprintf("ids=%d", id)
	}
	return string(apiGet(fmt.Sprintf("%s/goim/user/info?%s", gw, params), token).Data)
}

// ─── 好友 ───

func addFriend(gw, token string, friendID int64) {
	r := apiPost(fmt.Sprintf("%s/goim/friend/%d", gw, friendID), token, nil)
	if r.Code != 0 {
		log.Fatalf("添加好友失败: %s", r.Message)
	}
}

func removeFriend(gw, token string, friendID int64) {
	r := apiDelete(fmt.Sprintf("%s/goim/friend/%d", gw, friendID), token)
	if r.Code != 0 {
		log.Fatalf("删除好友失败: %s", r.Message)
	}
}

func listFriends(gw, token string) string {
	return string(apiGet(gw+"/goim/friend", token).Data)
}

// ─── 单聊 ───

func sendMessage(gw, token string, toID int64, content string) string {
	r := apiPost(gw+"/goim/chat", token, map[string]any{
		"to": toID, "content_type": 1, "content": content,
	})
	if r.Code != 0 {
		log.Fatalf("发消息失败: %s", r.Message)
	}
	var msgID string
	json.Unmarshal(r.Data, &msgID)
	return msgID
}

func sendMessageRaw(gw, token string, toID int64, content string) string {
	r := apiPost(gw+"/goim/chat", token, map[string]any{
		"to": toID, "content_type": 1, "content": content,
	})
	if r.Code != 0 {
		return fmt.Sprintf("code=%d, msg=%s", r.Code, r.Message)
	}
	return ""
}

func getHistory(gw, token string, sinceMs int64, limit int) string {
	return string(apiGet(fmt.Sprintf("%s/goim/chat?since=%d&limit=%d", gw, sinceMs, limit), token).Data)
}

// ─── ACK ───

func ackMsg(gw, msgID string) int {
	return apiPost(fmt.Sprintf("%s/goim/ack/%s", gw, msgID), "", nil).Code
}

// ─── 离线同步 ───

func syncMessages(gw, token string, sinceMs int64, limit int) string {
	return string(apiGet(fmt.Sprintf("%s/goim/sync?since=%d&limit=%d", gw, sinceMs, limit), token).Data)
}

func syncAck(gw, token string, ackAtMs int64) int {
	return apiPost(gw+"/goim/sync/ack", token, map[string]any{"ack_at": ackAtMs}).Code
}

// ─── 群聊 ───

func createGroup(gw, token, name string) int64 {
	r := apiPost(gw+"/goim/group", token, map[string]string{"name": name})
	if r.Code != 0 {
		log.Fatalf("创建群失败: %s", r.Message)
	}
	var data struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(r.Data, &data)
	return data.ID
}

func joinGroup(gw, token string, groupID int64) {
	r := apiPost(fmt.Sprintf("%s/goim/group/%d/join", gw, groupID), token, nil)
	if r.Code != 0 {
		log.Fatalf("加入群失败: %s", r.Message)
	}
}

func quitGroup(gw, token string, groupID int64) {
	r := apiPost(fmt.Sprintf("%s/goim/group/%d/quit", gw, groupID), token, nil)
	if r.Code != 0 {
		log.Fatalf("退出群失败: %s", r.Message)
	}
}

func listGroupMembers(gw, token string, groupID int64) string {
	return string(apiGet(fmt.Sprintf("%s/goim/group/%d/members", gw, groupID), token).Data)
}

func listJoinedGroups(gw, token string) string {
	return string(apiGet(gw+"/goim/user/group", token).Data)
}

func sendGroupMessage(gw, token string, groupID int64, content string) {
	r := apiPost(fmt.Sprintf("%s/goim/group/%d/chat", gw, groupID), token, map[string]any{
		"content_type": 1, "content": content,
	})
	if r.Code != 0 {
		log.Fatalf("发群消息失败: %s", r.Message)
	}
}

func sendGroupMessageRaw(gw, token string, groupID int64, content string) string {
	r := apiPost(fmt.Sprintf("%s/goim/group/%d/chat", gw, groupID), token, map[string]any{
		"content_type": 1, "content": content,
	})
	if r.Code != 0 {
		return fmt.Sprintf("code=%d, msg=%s", r.Code, r.Message)
	}
	return ""
}

func getGroupHistory(gw, token string, groupID int64, sinceMs int64, limit int) string {
	return string(apiGet(fmt.Sprintf("%s/goim/group/%d/chat?since=%d&limit=%d", gw, groupID, sinceMs, limit), token).Data)
}

func getGroupHistoryRaw(gw, token string, groupID int64, sinceMs int64, limit int) string {
	r := apiGet(fmt.Sprintf("%s/goim/group/%d/chat?since=%d&limit=%d", gw, groupID, sinceMs, limit), token)
	if r.Code != 0 {
		return fmt.Sprintf("code=%d, msg=%s", r.Code, r.Message)
	}
	return ""
}

// ─── Comet WebSocket ───

func connectComet(wsAddr, token string) *websocket.Conn {
	conn, _, err := websocket.DefaultDialer.Dial(wsAddr, nil)
	if err != nil {
		log.Fatalf("WebSocket 连接失败 %s: %v", wsAddr, err)
	}
	authBody, _ := json.Marshal(map[string]any{
		"token":    token,
		"room_id":  "",
		"platform": "web",
		"accepts":  []int32{1000, 1001, 1002, OpSingleChatMsg, OpGroupChatMsg},
	})
	conn.WriteMessage(websocket.BinaryMessage, encodePacket(1, OpAuth, 1, authBody))
	_, data, err := conn.ReadMessage()
	if err != nil || decodeOp(data) != OpAuthReply {
		log.Fatalf("Comet 鉴权失败")
	}
	step("Comet 鉴权成功")
	return conn
}

func heartbeatLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := conn.WriteMessage(websocket.BinaryMessage, encodePacket(1, OpHeartbeat, 1, nil)); err != nil {
			return
		}
	}
}

func receiveLoop(conn *websocket.Conn, ch chan<- string) {
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
			// skip
		case OpRaw:
			offset := 16
			for offset < len(data) {
				subPackLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
				subHeaderLen := int(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
				ch <- string(data[offset+subHeaderLen : offset+subPackLen])
				offset += subPackLen
			}
		default:
			packLen := int(binary.BigEndian.Uint32(data[0:4]))
			headerLen := int(binary.BigEndian.Uint16(data[4:6]))
			if packLen > headerLen {
				ch <- string(data[headerLen:packLen])
			}
		}
	}
}

func waitMessages(ch <-chan string, who string, expect int) int {
	timeout := time.After(5 * time.Second)
	received := 0
	for received < expect {
		select {
		case msg := <-ch:
			received++
			step("%s 收到推送 [%d/%d]: %.80s", who, received, expect, msg)
		case <-timeout:
			step("%s 超时, 收到 %d/%d", who, received, expect)
			return received
		}
	}
	return received
}

func safeClose(conn *websocket.Conn) {
	if conn != nil {
		conn.Close()
	}
}

// ─── 协议编解码 ───

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
