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

	msgID, err := g.dao.CreateMessage(c, fromID, toID, contentType, content)
	if err != nil {
		return "", err
	}

	pushBody, _ := json.Marshal(map[string]any{
		"msg_id":       msgID,
		"from":         fromID,
		"to":           toID,
		"content_type": contentType,
		"content":      content,
		"timestamp":    time.Now().UnixMilli(),
	})
	err = g.postMessageToLogic(toID, pushBody)
	g.track(msgID, toID, pushBody)
	return msgID, err
}

func (g *Gateway) postMessageToLogic(toID int64, msg []byte) error {
	return g.pushToMids(protocol.OpSingleChatMsg, []int64{toID}, msg)
}

func (g *Gateway) track(msgID string, mid int64, msg []byte) {
	if !g.c.ACK.Enabled {
		return
	}
	push := func(op int32, mid int64, msg []byte) {
		_ = g.pushToMids(op, []int64{mid}, msg)
	}
	go g.ack.Track(msgID, mid, protocol.OpSingleChatMsg, msg, push)
}

func (g *Gateway) pushToMids(op int32, mids []int64, msg []byte) error {
	baseURL := g.c.Logic.Addr + "/goim/push/mids"
	params := url.Values{}
	params.Set("operation", strconv.Itoa(int(op)))
	for _, mid := range mids {
		params.Add("mids", strconv.FormatInt(mid, 10))
	}
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

func (g *Gateway) SendGroupMessage(c context.Context, groupID, fromID int64, contentType int8, content string) (string, error) {
	if isMember, err := g.dao.IsInGroup(c, groupID, fromID); err != nil {
		return "", err
	} else if !isMember {
		return "", ErrNotGroupMember
	}

	msgID, err := g.dao.CreateGroupMessage(c, groupID, fromID, contentType, content)
	if err != nil {
		return "", err
	}

	memberIDs, err := g.dao.ListMemberIDs(c, groupID)
	if err != nil {
		return "", err
	}
	pushBody, _ := json.Marshal(map[string]any{
		"msg_id":       msgID,
		"group_id":     groupID,
		"from":         fromID,
		"content_type": contentType,
		"content":      content,
		"timestamp":    time.Now().UnixMilli(),
	})
	err = g.postGroupMessageToLogic(memberIDs, pushBody)
	return msgID, err
}

func (g *Gateway) postGroupMessageToLogic(memberIDs []int64, msg []byte) error {
	return g.pushToMids(protocol.OpGroupChatMsg, memberIDs, msg)
}

func (g *Gateway) HistoryGroupMessage(c context.Context, groupID, userID int64, since time.Time, limit int) ([]*model.GroupMessage, error) {
	if isMember, err := g.dao.IsInGroup(c, groupID, userID); err != nil {
		return nil, err
	} else if !isMember {
		return nil, ErrNotGroupMember
	}
	return g.dao.ListGroupMessagesSince(c, groupID, since, limit)
}
