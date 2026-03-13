package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Terry-Mao/goim/api/logic"
	"io"
	"net/http"

	"github.com/Terry-Mao/goim/internal/gateway/dao"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"github.com/Terry-Mao/goim/pkg/auth"
	"golang.org/x/crypto/bcrypt"
)

// ErrDuplicateUsername is exposed for the HTTP layer to distinguish business errors.
var ErrDuplicateUsername = dao.ErrDuplicateUsername

// ErrInvalidCredentials is returned when username not found or password mismatch.
var ErrInvalidCredentials = errors.New("invalid username or password")

// LoginResponse is the response body for POST /goim/auth/login.
type LoginResponse struct {
	ID    int64             `json:"id"`
	Token string            `json:"token"`
	Nodes *logic.NodesReply `json:"nodes"`
}

func (g *Gateway) Register(ctx context.Context, username, password string) (any, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &model.User{
		Username: username,
		Password: string(hash),
	}
	return nil, g.dao.CreateUser(ctx, user)
}

func (g *Gateway) Login(ctx context.Context, username, password string) (any, error) {
	user, err := g.dao.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	token, err := auth.GenerateToken(g.c.JWT.Secret, user.ID, g.c.JWT.ExpireHours)
	if err != nil {
		return nil, err
	}
	nodes, err := g.getNodes()
	if err != nil {
		return nil, err
	}
	return &LoginResponse{ID: user.ID, Token: token, Nodes: nodes}, nil
}

// getNodes calls Logic's GET /goim/nodes/weighted to get comet node list.
func (g *Gateway) getNodes() (*logic.NodesReply, error) {
	resp, err := http.Get(g.c.Logic.Addr + "/goim/nodes/weighted")
	if err != nil {
		return nil, fmt.Errorf("get nodes from logic: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read logic response: %w", err)
	}
	// Logic returns {"code":0,"data":{...}}
	var result struct {
		Code int               `json:"code"`
		Data *logic.NodesReply `json:"data"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal logic response: %w", err)
	}
	if result.Code != 0 || result.Data == nil {
		return nil, fmt.Errorf("logic nodes error, code: %d", result.Code)
	}
	return result.Data, nil
}
