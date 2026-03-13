package model

import "time"

// Friend 好友关系表，双向存储。
// A 和 B 互为好友时，存两行: (A,B) 和 (B,A)，查好友列表只需 WHERE user_id = ?。
type Friend struct {
	ID        int64 `gorm:"primaryKey;autoIncrement"`
	UserID    int64 `gorm:"not null;uniqueIndex:idx_user_friend"`
	FriendID  int64 `gorm:"not null;uniqueIndex:idx_user_friend"`
	CreatedAt time.Time
}
