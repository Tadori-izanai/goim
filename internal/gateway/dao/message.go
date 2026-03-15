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
