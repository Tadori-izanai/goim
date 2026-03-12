package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	}
	d := dao.New(c)
	// Clean users table
	d.Exec("DELETE FROM users")

	g := &Gateway{c: c, dao: d}
	t.Cleanup(func() {
		logicSrv.Close()
		d.Close()
	})
	return g, logicSrv
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
