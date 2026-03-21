package pkg

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// WsClient connects to Comet via WebSocket. Used for bench-chat and bench-fanout (JWT auth).
type WsClient struct {
	conn   *websocket.Conn
	closed atomic.Bool
}

type wsAuthBody struct {
	Token    string  `json:"token"`
	RoomID   string  `json:"room_id"`
	Platform string  `json:"platform"`
	Accepts  []int32 `json:"accepts"`
}

// NewWsClient connects to Comet WebSocket and authenticates with the given JWT token.
func NewWsClient(wsAddr, token string, accepts []int32) (*WsClient, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsAddr, nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", wsAddr, err)
	}
	c := &WsClient{conn: conn}
	// auth
	authBody, _ := json.Marshal(&wsAuthBody{
		Token:    token,
		RoomID:   "",
		Platform: "bench",
		Accepts:  accepts,
	})
	pkt := encodePacket(1, OpAuth, 1, authBody)
	if err := conn.WriteMessage(websocket.BinaryMessage, pkt); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write auth: %w", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read auth reply: %w", err)
	}
	if decodeOp(data) != OpAuthReply {
		conn.Close()
		return nil, fmt.Errorf("unexpected auth reply op: %d", decodeOp(data))
	}
	// start heartbeat
	go c.heartbeatLoop()
	return c, nil
}

// Receive blocks and calls handler for each received message (excluding heartbeat/auth replies).
// Handles OpRaw batched sub-packets.
func (c *WsClient) Receive(handler func(op int32, body []byte)) {
	for !c.closed.Load() {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if len(data) < headerLen {
			continue
		}
		op := decodeOp(data)
		switch op {
		case OpAuthReply, OpHeartbeatReply:
			// skip
		case OpRaw:
			// batched sub-packets after the outer 16-byte header
			offset := headerLen
			for offset < len(data) {
				if offset+4 > len(data) {
					break
				}
				subPackLen := int(binary.BigEndian.Uint32(data[offset : offset+4]))
				if subPackLen < headerLen || offset+subPackLen > len(data) {
					break
				}
				subHdrLen := int(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
				subOp := int32(binary.BigEndian.Uint32(data[offset+8 : offset+12]))
				subBody := data[offset+subHdrLen : offset+subPackLen]
				handler(subOp, subBody)
				offset += subPackLen
			}
		default:
			packLen := int(binary.BigEndian.Uint32(data[0:4]))
			hdrLen := int(binary.BigEndian.Uint16(data[4:6]))
			if packLen > hdrLen {
				handler(op, data[hdrLen:packLen])
			}
		}
	}
}

func (c *WsClient) Close() {
	if c.closed.CompareAndSwap(false, true) {
		c.conn.Close()
	}
}

func (c *WsClient) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if c.closed.Load() {
			return
		}
		pkt := encodePacket(1, OpHeartbeat, 1, nil)
		if err := c.conn.WriteMessage(websocket.BinaryMessage, pkt); err != nil {
			return
		}
	}
}

func encodePacket(ver uint16, op int32, seq uint32, body []byte) []byte {
	packLen := headerLen + len(body)
	buf := make([]byte, packLen)
	binary.BigEndian.PutUint32(buf[0:4], uint32(packLen))
	binary.BigEndian.PutUint16(buf[4:6], uint16(headerLen))
	binary.BigEndian.PutUint16(buf[6:8], ver)
	binary.BigEndian.PutUint32(buf[8:12], uint32(op))
	binary.BigEndian.PutUint32(buf[12:16], seq)
	if len(body) > 0 {
		copy(buf[16:], body)
	}
	return buf
}

func decodeOp(data []byte) int32 {
	if len(data) < 12 {
		return 0
	}
	return int32(binary.BigEndian.Uint32(data[8:12]))
}
