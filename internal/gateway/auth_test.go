package gateway

import (
	"context"
	"encoding/json"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/Terry-Mao/goim/internal/gateway/dao"
	"github.com/Terry-Mao/goim/pkg/auth"
)

// mockLogicServer creates a fake Logic HTTP server that responds to /goim/nodes/weighted.
func mockLogicServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"domain":        "conn.goim.io",
				"tcp_port":      3101,
				"ws_port":       3102,
				"wss_port":      3103,
				"heartbeat":     240,
				"heartbeat_max": 2,
				"nodes":         []string{"127.0.0.1"},
			},
		})
	}))
}

// testGateway creates a Gateway connected to real MySQL + mock Logic server.
// Skips the test if MySQL is not available.
func testGateway(t *testing.T) (*Gateway, *httptest.Server) {
	t.Helper()
	logicSrv := mockLogicServer()
	c := &conf.Config{
		MySQL: &conf.MySQL{
			DSN: "root:password@tcp(127.0.0.1:3306)/goim?charset=utf8mb4&parseTime=True&loc=Local",
		},
		JWT:   &conf.JWT{Secret: "test-secret", ExpireHours: 1},
		Logic: &conf.Logic{Addr: logicSrv.URL},
		ACK:   &conf.ACK{RetryInterval: 5, MaxRetries: 3},
	}
	d := dao.New(c)
	// Clean users table
	d.Exec("DELETE FROM group_messages")
	d.Exec("DELETE FROM group_members")
	d.Exec("DELETE FROM `groups`")
	d.Exec("DELETE FROM friends")
	d.Exec("DELETE FROM messages")
	d.Exec("DELETE FROM users")

	g := &Gateway{c: c, dao: d, client: &http.Client{}, ack: newAckService(conf.Default().ACK)}
	t.Cleanup(func() {
		logicSrv.Close()
		d.Close()
	})
	return g, logicSrv
}

// mustLoginID is a test helper that logs in and returns the user's ID.
func (g *Gateway) mustLoginID(t *testing.T, ctx context.Context, username, password string) int64 {
	t.Helper()
	res, err := g.Login(ctx, username, password)
	if err != nil {
		t.Fatalf("Login(%s) failed: %v", username, err)
	}
	return res.(*LoginResponse).ID
}

func TestRegister(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	_, err := g.Register(ctx, "alice", "password123")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "bob", "pw1")
	_, err := g.Register(ctx, "bob", "pw2")
	if err != ErrDuplicateUsername {
		t.Fatalf("expected ErrDuplicateUsername, got: %v", err)
	}
}

func TestLogin(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	// Register first
	g.Register(ctx, "charlie", "secret123")

	// Login
	res, err := g.Login(ctx, "charlie", "secret123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	loginResp, ok := res.(*LoginResponse)
	if !ok {
		t.Fatal("expected *LoginResponse")
	}
	if loginResp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	// Verify token is valid
	claims, err := auth.ParseToken("test-secret", loginResp.Token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	if claims.Mid == 0 {
		t.Fatal("expected non-zero mid in token")
	}
	// Verify nodes
	if loginResp.Nodes == nil {
		t.Fatal("expected non-nil nodes")
	}
	if loginResp.Nodes.WsPort != 3102 {
		t.Fatalf("expected ws_port 3102, got %d", loginResp.Nodes.WsPort)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "dave", "correct")
	_, err := g.Login(ctx, "dave", "wrong")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	_, err := g.Login(ctx, "nobody", "pw")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestLogin_LastAckAtAfterSync(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "eve", "pw")
	eveID := g.mustLoginID(t, ctx, "eve", "pw")

	// SyncAck to set last_ack_at
	ackTime := time.Now().Truncate(time.Millisecond)
	g.SyncAck(ctx, eveID, ackTime)

	// Login again, should see updated last_ack_at
	res, err := g.Login(ctx, "eve", "pw")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	loginResp := res.(*LoginResponse)
	got := time.Time(loginResp.LastAckAt).Truncate(time.Millisecond)
	if !got.Equal(ackTime) {
		t.Fatalf("expected last_ack_at=%v, got %v", ackTime, got)
	}
}

func TestLogin_LastAckAtJSON(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "frank", "pw")
	frankID := g.mustLoginID(t, ctx, "frank", "pw")

	ackTime := time.UnixMilli(1710000000000)
	g.SyncAck(ctx, frankID, ackTime)

	res, err := g.Login(ctx, "frank", "pw")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	loginResp := res.(*LoginResponse)

	// Verify JSON serialization is millisecond timestamp
	data, _ := json.Marshal(loginResp.LastAckAt)
	if string(data) != "1710000000000" {
		t.Fatalf("expected JSON 1710000000000, got %s", data)
	}

	// Verify round-trip
	var parsed model.UnixMilliTime
	json.Unmarshal(data, &parsed)
	if !time.Time(parsed).Equal(ackTime) {
		t.Fatalf("round-trip failed: expected %v, got %v", ackTime, time.Time(parsed))
	}
}
