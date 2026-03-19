package gateway

import (
	"context"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"time"
)

type SyncResult struct {
	Messages      []*model.Message      `json:"messages"`
	GroupMessages []*model.GroupMessage `json:"group_messages"`
}

// SyncMessages 客户端 Pull 离线消息
func (g *Gateway) SyncMessages(c context.Context, userID int64, since time.Time, limit int) (*SyncResult, error) {
	messages, err := g.dao.ListMessagesSince(c, userID, since, limit)
	if err != nil {
		return nil, err
	}
	groupMessages, err := g.dao.ListOfflineGroupMessages(c, userID, since, limit)
	if err != nil {
		return nil, err
	}
	return &SyncResult{Messages: messages, GroupMessages: groupMessages}, nil
}

// SyncAck 客户端确认已收到，更新 last_ack_at
func (g *Gateway) SyncAck(c context.Context, userID int64, ackAt time.Time) error {
	return g.dao.UpdateLastAckAt(c, userID, ackAt)
}
