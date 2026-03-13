package dao

import (
	"context"

	"github.com/Terry-Mao/goim/internal/gateway/model"
	"gorm.io/gorm"
)

// AddFriend adds a bidirectional friend relationship.
// Idempotent: does nothing if the relationship already exists.
func (d *Dao) AddFriend(ctx context.Context, userID, friendID int64) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.FirstOrCreate(&model.Friend{}, model.Friend{UserID: userID, FriendID: friendID}).Error; err != nil {
			return err
		}
		return tx.FirstOrCreate(&model.Friend{}, model.Friend{UserID: friendID, FriendID: userID}).Error
	})
}

// RemoveFriend removes a bidirectional friend relationship.
func (d *Dao) RemoveFriend(ctx context.Context, userID, friendID int64) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND friend_id = ?", userID, friendID).Delete(&model.Friend{}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? AND friend_id = ?", friendID, userID).Delete(&model.Friend{}).Error
	})
}

// IsFriend checks if two users are friends.
func (d *Dao) IsFriend(ctx context.Context, userID, friendID int64) (bool, error) {
	var count int64
	err := d.db.WithContext(ctx).Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", userID, friendID).
		Count(&count).Error
	return count > 0, err
}

// ListFriends returns all friend user IDs for the given user.
func (d *Dao) ListFriends(ctx context.Context, userID int64) ([]int64, error) {
	var friendIDs []int64
	err := d.db.WithContext(ctx).Model(&model.Friend{}).
		Where("user_id = ?", userID).
		Pluck("friend_id", &friendIDs).Error
	return friendIDs, err
}
