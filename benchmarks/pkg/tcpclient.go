package pkg

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

const (
	headerLen = 16
	heartbeat = 120 * time.Second

	OpHeartbeat      = int32(2)
	OpHeartbeatReply = int32(3)
	OpAuth           = int32(7)
	OpAuthReply      = int32(8)
	OpRaw            = int32(9)
)

// TcpClient connects to Comet via TCP. Used for bench-mq (no JWT, direct mid auth).
type TcpClient struct {
	conn   net.Conn
	wr     *bufio.Writer
	rd     *bufio.Reader
	seq    atomic.Int32
	closed atomic.Bool
}

type authToken struct {
	Mid      int64   `json:"mid"`
	Key      string  `json:"key"`
	RoomID   string  `json:"room_id"`
	Platform string  `json:"platform"`
	Accepts  []int32 `json:"accepts"`
}

// NewTcpClient connects to Comet TCP and authenticates with the given mid.
func NewTcpClient(addr string, mid int64, roomID string, accepts []int32) (*TcpClient, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	c := &TcpClient{
		conn: conn,
		wr:   bufio.NewWriter(conn),
		rd:   bufio.NewReader(conn),
	}
	// auth
	body, _ := json.Marshal(&authToken{
		Mid:      mid,
		Key:      "",
		RoomID:   roomID,
		Platform: "bench",
		Accepts:  accepts,
	})
	if err := c.writeProto(OpAuth, body); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write auth: %w", err)
	}
	op, _, err := c.readProto()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read auth reply: %w", err)
	}
	if op != OpAuthReply {
		conn.Close()
		return nil, fmt.Errorf("unexpected auth reply op: %d", op)
	}
	// start heartbeat
	go c.heartbeatLoop()
	return c, nil
}

// Receive blocks and calls handler for each received message (excluding heartbeat/auth replies).
func (c *TcpClient) Receive(handler func(op int32, body []byte)) {
	for !c.closed.Load() {
		op, body, err := c.readProto()
		if err != nil {
			return
		}
		switch op {
		case OpAuthReply, OpHeartbeatReply:
			// skip
		default:
			handler(op, body)
		}
	}
}

func (c *TcpClient) Close() {
	if c.closed.CompareAndSwap(false, true) {
		c.conn.Close()
	}
}

func (c *TcpClient) heartbeatLoop() {
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for range ticker.C {
		if c.closed.Load() {
			return
		}
		if err := c.writeProto(OpHeartbeat, nil); err != nil {
			return
		}
	}
}

func (c *TcpClient) writeProto(op int32, body []byte) error {
	packLen := uint32(headerLen) + uint32(len(body))
	seq := c.seq.Add(1)
	if err := binary.Write(c.wr, binary.BigEndian, packLen); err != nil {
		return err
	}
	if err := binary.Write(c.wr, binary.BigEndian, uint16(headerLen)); err != nil {
		return err
	}
	if err := binary.Write(c.wr, binary.BigEndian, uint16(1)); err != nil { // ver
		return err
	}
	if err := binary.Write(c.wr, binary.BigEndian, op); err != nil {
		return err
	}
	if err := binary.Write(c.wr, binary.BigEndian, seq); err != nil {
		return err
	}
	if len(body) > 0 {
		if _, err := c.wr.Write(body); err != nil {
			return err
		}
	}
	return c.wr.Flush()
}

func (c *TcpClient) readProto() (op int32, body []byte, err error) {
	var packLen int32
	var hdrLen int16
	var ver int16
	var seq int32
	if err = binary.Read(c.rd, binary.BigEndian, &packLen); err != nil {
		return
	}
	if err = binary.Read(c.rd, binary.BigEndian, &hdrLen); err != nil {
		return
	}
	if err = binary.Read(c.rd, binary.BigEndian, &ver); err != nil {
		return
	}
	if err = binary.Read(c.rd, binary.BigEndian, &op); err != nil {
		return
	}
	if err = binary.Read(c.rd, binary.BigEndian, &seq); err != nil {
		return
	}
	bodyLen := int(packLen - int32(hdrLen))
	if bodyLen > 0 {
		body = make([]byte, bodyLen)
		n := 0
		for n < bodyLen {
			var t int
			if t, err = c.rd.Read(body[n:]); err != nil {
				return
			}
			n += t
		}
	}
	return
}
