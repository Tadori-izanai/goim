package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Terry-Mao/goim/api/protocol"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (g *Gateway) SendMessage(c context.Context, fromID, toID int64, contentType int8, content string) (string, error) {
	if exists, err := g.dao.IsFriend(c, fromID, toID); err != nil {
		return "", err
	} else if !exists {
		return "", ErrNotFriend
	}

	if err := g.postMessageToLogic(toID, []byte(content)); err != nil {
		return "", err
	}
	msgID, err := g.dao.CreateMessage(c, fromID, toID, contentType, content)
	return msgID, err
}

func (g *Gateway) postMessageToLogic(toID int64, msg []byte) error {
	baseURL := g.c.Logic.Addr + "/goim/push/mids"
	params := url.Values{}
	params.Set("operation", strconv.Itoa(int(protocol.OpSingleChatMsg)))
	params.Set("mids", strconv.FormatInt(toID, 10))
	fullURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	req, err := http.NewRequest("POST", fullURL, bytes.NewBuffer(msg))
	if err != nil {
		return fmt.Errorf("new post request: %w", err)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("post logic: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read logic response: %w", err)
	}

	var result struct {
		Code int `json:"code"`
	}
	if err = json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("unmarshal logic response: %s\n", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("logic nodes error, code: %d", result.Code)
	}
	return nil
}

func (g *Gateway) HistoryMessage(c context.Context, toID int64, since time.Time, limit int) ([]*model.Message, error) {
	messages, err := g.dao.ListMessagesSince(c, toID, since, limit)
	return messages, err
}
