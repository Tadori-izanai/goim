package dao

import (
	"context"
	"time"

	"github.com/Terry-Mao/goim/internal/gateway/model"
	"github.com/google/uuid"
)

// CreateMessage inserts a message and returns the generated msg_id.
func (d *Dao) CreateMessage(ctx context.Context, fromID, toID int64, contentType int8, content string) (string, error) {
	msg := &model.Message{
		MsgID:       uuid.New().String(),
		FromID:      fromID,
		ToID:        toID,
		ContentType: contentType,
		Content:     content,
		CreatedAt:   model.UnixMilliTime(time.Now()),
	}
	err := d.db.WithContext(ctx).Create(msg).Error
	return msg.MsgID, err
}

// ListMessagesSince returns messages sent to toID after since, ordered by created_at ASC.
// Hits the (to_id, created_at) index directly.
// Client groups messages by from_id into conversations.
func (d *Dao) ListMessagesSince(ctx context.Context, toID int64, since time.Time, limit int) ([]*model.Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var messages []*model.Message
	err := d.db.WithContext(ctx).
		Where("to_id = ? AND created_at > ?", toID, since).
		Order("created_at").
		Limit(limit).
		Find(&messages).Error
	return messages, err
}

// CreateGroupMessage inserts a group message and returns the generated msg_id.
func (d *Dao) CreateGroupMessage(ctx context.Context, groupID, fromID int64, contentType int8, content string) (string, error) {
	msg := &model.GroupMessage{
		MsgID:       uuid.New().String(),
		GroupID:     groupID,
		FromID:      fromID,
		ContentType: contentType,
		Content:     content,
		CreatedAt:   model.UnixMilliTime(time.Now()),
	}
	err := d.db.WithContext(ctx).Create(msg).Error
	return msg.MsgID, err
}

// ListGroupMessagesSince returns messages in groupID after since, ordered by created_at ASC.
// Hits the (group_id, created_at) index.
func (d *Dao) ListGroupMessagesSince(ctx context.Context, groupID int64, since time.Time, limit int) ([]*model.GroupMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var messages []*model.GroupMessage
	err := d.db.WithContext(ctx).
		Where("group_id = ? AND created_at > ?", groupID, since).
		Order("created_at").
		Limit(limit).
		Find(&messages).Error
	return messages, err
}

// ListOfflineGroupMessages 查询用户在所有已加入群的离线消息
// JOIN group_members 确保只返回用户当前所在群、加入后的消息
func (d *Dao) ListOfflineGroupMessages(ctx context.Context, userID int64, since time.Time, limit int) ([]*model.GroupMessage, error) {
	var messages []*model.GroupMessage
	err := d.db.WithContext(ctx).
		Table("group_messages AS msg").
		Select("msg.*").
		Joins("JOIN group_members AS mem ON msg.group_id = mem.group_id").
		Where("mem.user_id = ?", userID).
		Where("msg.created_at > ?", since).
		Where("msg.created_at > mem.joined_at"). // 核心：过滤入群前的消息
		Order("msg.created_at ASC").             // 通常离线消息按时间正序排列
		Limit(limit).
		Find(&messages).Error
	return messages, err
}
