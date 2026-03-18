package dao

import (
	"context"
	"errors"
	"time"

	"github.com/Terry-Mao/goim/internal/gateway/model"
	"github.com/go-sql-driver/mysql"
)

// ErrDuplicateUsername is returned when a username already exists.
var ErrDuplicateUsername = errors.New("username already exists")

// CreateUser inserts a new user. Returns ErrDuplicateUsername on unique index conflict.
func (d *Dao) CreateUser(ctx context.Context, username, hashPwd string) (int64, error) {
	user := &model.User{
		Username:     username,
		Password:     hashPwd,
		LastOnlineAt: time.Now(),
		LastAckAt:    model.UnixMilliTime(time.Now()),
	}

	err := d.db.WithContext(ctx).Create(user).Error

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return 0, ErrDuplicateUsername
	}
	return user.ID, err
}

func (d *Dao) IsUserCreated(ctx context.Context, userID int64) (bool, error) {
	var count int64
	err := d.db.WithContext(ctx).Model(model.User{}).
		Where("id = ?", userID).Count(&count).Error
	return count > 0, err
}

// GetUserByUsername finds a user by username. Returns gorm.ErrRecordNotFound if not found.
func (d *Dao) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := d.db.WithContext(ctx).First(&user, "username = ?", username).Error

	return &user, err
}

// GetUsersByIDs returns users matching the given IDs.
func (d *Dao) GetUsersByIDs(ctx context.Context, ids []int64) ([]*model.User, error) {
	var users []*model.User
	err := d.db.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error
	return users, err
}

func (d *Dao) UpdateLastOnlineAt(ctx context.Context, userID int64, t time.Time) error {
	return d.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).Update("last_online_at", t).Error
}

func (d *Dao) UpdateLastAckAt(ctx context.Context, userID int64, t time.Time) error {
	return d.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).Update("last_ack_at", t).Error
}

func (d *Dao) GetLastAckAt(ctx context.Context, userID int64) (time.Time, error) {
	var t time.Time
	err := d.db.WithContext(ctx).Model(model.User{}).
		Where("id = ?", userID).Select("last_ack_at").Scan(&t).Error
	return t, err
}
