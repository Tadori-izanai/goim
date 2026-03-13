package dao

import (
	"context"
	"errors"

	"github.com/Terry-Mao/goim/internal/gateway/model"
	"github.com/go-sql-driver/mysql"
)

// ErrDuplicateUsername is returned when a username already exists.
var ErrDuplicateUsername = errors.New("username already exists")

// CreateUser inserts a new user. Returns ErrDuplicateUsername on unique index conflict.
func (d *Dao) CreateUser(ctx context.Context, user *model.User) error {
	err := d.db.WithContext(ctx).Create(user).Error

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrDuplicateUsername
	}
	return err
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
