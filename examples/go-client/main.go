package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

// 协议操作码
const (
	OpAuth           = 7 // 鉴权
	OpAuthReply      = 8 // 鉴权响应
	OpHeartbeat      = 2 // 心跳
	OpHeartbeatReply = 3 // 心跳响应
	OpRaw            = 9 // 批量消息
)

// 鉴权请求体
type AuthToken struct {
	Mid      int64   `json:"mid"`
	RoomID   string  `json:"room_id"`
	Platform string  `json:"platform"`
	Accepts  []int32 `json:"accepts"`
}

func main() {
	mid := flag.Int64("mid", 123, "用户 ID")
	room := flag.String("room", "1000", "房间号")
	flag.Parse()

	// 连接 Comet WebSocket 服务
	url := "ws://127.0.0.1:3102/sub"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer conn.Close()

	log.Println("✓ WebSocket 连接成功")

	// 1. 发送鉴权消息
	if err := sendAuth(conn, *mid, *room); err != nil {
		log.Fatalf("鉴权失败: %v", err)
	}

	// 2. 启动心跳协程
	go heartbeatLoop(conn)

	// 3. 接收消息循环
	receiveLoop(conn)
}

// 发送鉴权消息
func sendAuth(conn *websocket.Conn, mid int64, room string) error {
	token := AuthToken{
		Mid:      mid,
		RoomID:   "live://" + room,
		Platform: "web",
		Accepts:  []int32{1000, 1001, 1002},
	}

	body, _ := json.Marshal(token)
	packet := encodePacket(1, OpAuth, 1, body)

	if err := conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
		return err
	}

	log.Printf("→ 发送鉴权: %s", string(body))

	// 等待鉴权响应
	_, data, err := conn.ReadMessage()
	if err != nil {
		return err
	}

	op := decodeOp(data)
	if op == OpAuthReply {
		log.Println("✓ 鉴权成功")
		return nil
	}

	return fmt.Errorf("鉴权失败，收到 Op=%d", op)
}

// 心跳循环
func heartbeatLoop(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		packet := encodePacket(1, OpHeartbeat, 1, nil)
		if err := conn.WriteMessage(websocket.BinaryMessage, packet); err != nil {
			log.Printf("心跳发送失败: %v", err)
			return
		}
		log.Println("→ 发送心跳")
	}
}

// 接收消息循环
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

// 处理收到的消息
func handleMessage(data []byte) {
	if len(data) < 16 {
		return
	}

	packLen := binary.BigEndian.Uint32(data[0:4])
	headerLen := binary.BigEndian.Uint16(data[4:6])
	ver := binary.BigEndian.Uint16(data[6:8])
	op := binary.BigEndian.Uint32(data[8:12])
	seq := binary.BigEndian.Uint32(data[12:16])

	log.Printf("← 收到消息: PackLen=%d, Ver=%d, Op=%d, Seq=%d", packLen, ver, op, seq)

	switch op {
	case OpAuthReply:
		log.Println("  [鉴权响应]")

	case OpHeartbeatReply:
		log.Println("  [心跳响应]")

	case OpRaw:
		// 批量消息，需要解析多个子包
		log.Println("  [批量消息]")
		offset := 16
		for offset < len(data) {
			subPackLen := binary.BigEndian.Uint32(data[offset : offset+4])
			subHeaderLen := binary.BigEndian.Uint16(data[offset+4 : offset+6])
			subOp := binary.BigEndian.Uint32(data[offset+8 : offset+12])
			subBody := data[offset+int(subHeaderLen) : offset+int(subPackLen)]

			log.Printf("    子消息 Op=%d, Body=%s", subOp, string(subBody))
			offset += int(subPackLen)
		}

	default:
		// 单条业务消息
		if packLen > 16 {
			body := data[headerLen:packLen]
			log.Printf("  [业务消息] Op=%d, Body=%s", op, string(body))
		}
	}
}

// 编码协议包
func encodePacket(ver uint16, op uint32, seq uint32, body []byte) []byte {
	packLen := 16 + len(body)
	buf := make([]byte, packLen)

	binary.BigEndian.PutUint32(buf[0:4], uint32(packLen)) // PackLen
	binary.BigEndian.PutUint16(buf[4:6], 16)              // HeaderLen
	binary.BigEndian.PutUint16(buf[6:8], ver)             // Ver
	binary.BigEndian.PutUint32(buf[8:12], op)             // Op
	binary.BigEndian.PutUint32(buf[12:16], seq)           // Seq

	if len(body) > 0 {
		copy(buf[16:], body)
	}

	return buf
}

// 解码操作码
func decodeOp(data []byte) uint32 {
	if len(data) < 16 {
		return 0
	}
	return binary.BigEndian.Uint32(data[8:12])
}
