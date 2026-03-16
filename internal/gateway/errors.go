package gateway

import (
	"errors"

	"github.com/Terry-Mao/goim/internal/gateway/dao"
)

var (
	// ErrDuplicateUsername is exposed for the HTTP layer to distinguish business errors.
	ErrDuplicateUsername  = dao.ErrDuplicateUsername
	// ErrInvalidCredentials is returned when username not found or password mismatch.
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrFriendSelf         = errors.New("cannot add yourself as friend")
	ErrNotFriend          = errors.New("not friends")

	ErrGroupNotFound  = errors.New("group not found")
	ErrNotGroupMember = errors.New("not a group member")
)
