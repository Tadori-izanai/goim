package model

import "time"

type Group struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	Name      string `gorm:"size:128;not null"`
	OwnerID   int64  `gorm:"not null;index"`
	CreatedAt time.Time
}

// GroupMember 查询群成员
// `(group_id, user_id)` 唯一索引：防止重复加入，校验是否为成员
// `(user_id)` 索引：查询用户加入了哪些群
type GroupMember struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	GroupID    int64     `gorm:"not null;uniqueIndex:idx_group_user"`
	UserID     int64     `gorm:"not null;uniqueIndex:idx_group_user;index:idx_user"`
	JoinedAt   time.Time `gorm:"not null"`
	LastReadAt time.Time `gorm:"not null"` // 用户在该群的已读位置，用于离线消息
}
