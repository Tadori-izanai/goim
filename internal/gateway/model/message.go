package model

import "time"

// Message 单聊消息表，全量存储。
// 索引 (to_id, created_at) 用于高效查询离线消息: WHERE to_id=? AND created_at > ?
type Message struct {
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	MsgID       string    `gorm:"size:36;uniqueIndex;not null"`
	FromID      int64     `gorm:"not null"`
	ToID        int64     `gorm:"not null;index:idx_to_created"`
	ContentType int8      `gorm:"not null;default:1"`
	Content     string    `gorm:"type:text;not null"`
	CreatedAt   time.Time `gorm:"not null;index:idx_to_created"`
}
