package model

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"
)

// UnixMilliTime wraps time.Time, JSON serializes as millisecond timestamp (int64).
// GORM treats it as time.Time for database operations.
type UnixMilliTime time.Time

func (t *UnixMilliTime) Scan(value interface{}) error {
	if v, ok := value.(time.Time); ok {
		*t = UnixMilliTime(v)
		return nil
	}
	return fmt.Errorf("can not scan %v into UnixMilliTime", value)
}

func (t UnixMilliTime) Value() (driver.Value, error) {
	return time.Time(t), nil
}

func (t UnixMilliTime) MarshalJSON() ([]byte, error) {
	ms := time.Time(t).UnixMilli()
	return strconv.AppendInt(nil, ms, 10), nil
}

func (t *UnixMilliTime) UnmarshalJSON(data []byte) error {
	ms, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}
	*t = UnixMilliTime(time.UnixMilli(ms))
	return nil
}

// Message 单聊消息表，全量存储。
// 索引 (to_id, created_at) 用于高效查询离线消息: WHERE to_id=? AND created_at > ?
type Message struct {
	ID          int64         `gorm:"primaryKey;autoIncrement" json:"-"`
	MsgID       string        `gorm:"size:36;uniqueIndex;not null" json:"msg_id"`
	FromID      int64         `gorm:"not null" json:"from"`
	ToID        int64         `gorm:"not null;index:idx_to_created" json:"to"`
	ContentType int8          `gorm:"not null;default:1" json:"content_type"`
	Content     string        `gorm:"type:text;not null" json:"content"`
	CreatedAt   UnixMilliTime `gorm:"not null;index:idx_to_created" json:"timestamp"`
}

// GroupMessage 群聊消息表，全量存储。
// (group_id, created_at) 联合索引：按群拉取历史/离线消息
type GroupMessage struct {
	ID          int64         `gorm:"primaryKey;autoIncrement" json:"-"`
	MsgID       string        `gorm:"size:36;uniqueIndex;not null" json:"msg_id"`
	GroupID     int64         `gorm:"not null;index:idx_group_created" json:"group_id"`
	FromID      int64         `gorm:"not null" json:"from"`
	ContentType int8          `gorm:"not null;default:1" json:"content_type"`
	Content     string        `gorm:"type:text;not null" json:"content"`
	CreatedAt   UnixMilliTime `gorm:"not null;index:idx_group_created" json:"timestamp"`
}
