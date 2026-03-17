package model

import "time"

type Group struct {
	ID        int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string `gorm:"size:128;not null" json:"name"`
	OwnerID   int64  `gorm:"not null;index" json:"owner_id"`
	CreatedAt time.Time `json:"-"`
}

// GroupMember 查询群成员
// `(group_id, user_id)` 唯一索引：防止重复加入，校验是否为成员
// `(user_id)` 索引：查询用户加入了哪些群
type GroupMember struct {
	ID       int64     `gorm:"primaryKey;autoIncrement"`
	GroupID  int64     `gorm:"not null;uniqueIndex:idx_group_user"`
	UserID   int64     `gorm:"not null;uniqueIndex:idx_group_user;index:idx_user"`
	JoinedAt time.Time `gorm:"not null"`
}
