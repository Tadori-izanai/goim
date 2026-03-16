package dao

import (
	"context"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"gorm.io/gorm"
	"time"
)

// CreateGroup creates a group and added owner to this group.
func (d *Dao) CreateGroup(ctx context.Context, groupName string, creatorID int64) (int64, error) {
	group := &model.Group{
		Name:    groupName,
		OwnerID: creatorID,
	}
	err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		now := time.Now()
		groupMember := &model.GroupMember{
			GroupID:    group.ID,
			UserID:     creatorID,
			JoinedAt:   now,
			LastReadAt: now,
		}
		return tx.FirstOrCreate(&model.GroupMember{}, groupMember).Error
	})
	return group.ID, err
}

// JoinGroup checks if the user is in the group: if not then join the group.
func (d *Dao) JoinGroup(ctx context.Context, groupID, userID int64) error {
	now := time.Now()
	groupMember := &model.GroupMember{
		GroupID:    groupID,
		UserID:     userID,
		JoinedAt:   now,
		LastReadAt: now,
	}

	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		err := tx.WithContext(ctx).Model(&model.GroupMember{}).
			Where("group_id = ? AND user_id = ?", groupID, userID).Count(&count).Error
		if err == nil && count > 0 {
			return nil
		}
		return tx.WithContext(ctx).Create(groupMember).Error
	})
}

// IsInGroup checks if a user is in a group.
func (d *Dao) IsInGroup(ctx context.Context, groupID, userID int64) (bool, error) {
	var count int64
	err := d.db.WithContext(ctx).Model(&model.GroupMember{}).
		Where("group_id = ? AND user_id = ?", groupID, userID).Count(&count).Error
	return count > 0, err
}

// QuitGroup removes the user. Should first check whether IsInGroup.
func (d *Dao) QuitGroup(ctx context.Context, groupID, userID int64) error {
	return d.db.WithContext(ctx).
		Where("group_id = ? AND user_id = ?", groupID, userID).
		Delete(model.GroupMember{}).Error
}

// GetGroupByIDs returns groups matching the given IDs.
func (d *Dao) GetGroupByIDs(ctx context.Context, groupIDs []int64) ([]*model.Group, error) {
	var groups []*model.Group
	err := d.db.WithContext(ctx).Where("id IN ?", groupIDs).Find(&groups).Error
	return groups, err
}

// ListMemberIDs returns all member user IDs for given group.
func (d *Dao) ListMemberIDs(ctx context.Context, groupID int64) ([]int64, error) {
	var memberIDs []int64
	err := d.db.WithContext(ctx).Model(&model.GroupMember{}).
		Where("group_id = ?", groupID).
		Pluck("user_id", &memberIDs).Error
	return memberIDs, err
}

// ListGroupIDs returns all group IDs that the user joined.
func (d *Dao) ListGroupIDs(ctx context.Context, userID int64) ([]int64, error) {
	var groupIDs []int64
	err := d.db.WithContext(ctx).Model(&model.GroupMember{}).
		Where("user_id = ?", userID).
		Pluck("group_id", &groupIDs).Error
	return groupIDs, err
}
