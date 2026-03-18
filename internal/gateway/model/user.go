package model

import "time"

// User 用户表，GORM AutoMigrate 自动建表。
// bcrypt 是一种单向哈希算法，专为密码设计, 例如：
// - "123456" → bcrypt → "$2a$10$N9qo8uLOickgx2ZMRZoMye..." (60 字符)
type User struct {
	ID           int64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string        `gorm:"uniqueIndex;size:64;not null" json:"username"`
	Password     string        `gorm:"size:255;not null" json:"-"`
	LastOnlineAt time.Time     `json:"last_online_at,omitempty"` // 断线时间（Logic 回调更新）
	LastAckAt    UnixMilliTime `json:"last_ack_at,omitempty"`    // ACK 位点（客户端 Pull 后更新）
	CreatedAt    time.Time     `json:"created_at,omitempty"`
	UpdatedAt    time.Time     `json:"updated_at,omitempty"`
}
