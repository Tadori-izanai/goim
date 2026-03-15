package gateway

import (
	"context"

	"github.com/Terry-Mao/goim/internal/gateway/model"
)

func (g *Gateway) AddFriend(c context.Context, userID, friendID int64) error {
	if userID == friendID {
		return ErrFriendSelf
	}
	if exists, err := g.dao.IsUserCreated(c, friendID); err != nil {
		return err
	} else if !exists {
		return ErrUserNotFound
	}
	return g.dao.AddFriend(c, userID, friendID)
}

func (g *Gateway) RemoveFriend(c context.Context, userID, friendID int64) error {
	if exists, err := g.dao.IsFriend(c, userID, friendID); err != nil {
		return err
	} else if !exists {
		return ErrNotFriend
	}
	return g.dao.RemoveFriend(c, userID, friendID)
}

func (g *Gateway) ListFriend(c context.Context, userID int64) ([]*model.User, error) {
	friendIDs, err := g.dao.ListFriends(c, userID)
	if err != nil {
		return nil, err
	}
	friends, err := g.dao.GetUsersByIDs(c, friendIDs)
	return friends, err
}
