package gateway

import (
	"context"
	"github.com/Terry-Mao/goim/internal/gateway/model"
)

func (g *Gateway) CreateGroup(c context.Context, userID int64, groupName string) (*model.Group, error) {
	group, err := g.dao.CreateGroup(c, groupName, userID)
	return group, err
}

func (g *Gateway) JoinGroup(c context.Context, groupID, userID int64) error {
	if exists, err := g.dao.IsGroupCreated(c, groupID); err != nil {
		return err
	} else if !exists {
		return ErrGroupNotFound
	}
	return g.dao.JoinGroup(c, groupID, userID)
}

func (g *Gateway) QuitGroup(c context.Context, groupID, userID int64) error {
	if exists, err := g.dao.IsGroupCreated(c, groupID); err != nil {
		return err
	} else if !exists {
		return ErrGroupNotFound
	}
	if isMember, err := g.dao.IsInGroup(c, groupID, userID); err != nil {
		return err
	} else if !isMember {
		return ErrNotGroupMember
	}
	return g.dao.QuitGroup(c, groupID, userID)
}

func (g *Gateway) ListGroupMembers(c context.Context, groupID, userID int64) ([]*model.User, error) {
	if exists, err := g.dao.IsGroupCreated(c, groupID); err != nil {
		return nil, err
	} else if !exists {
		return nil, ErrGroupNotFound
	}
	if isMember, err := g.dao.IsInGroup(c, groupID, userID); err != nil {
		return nil, err
	} else if !isMember {
		return nil, ErrNotGroupMember
	}

	userIDs, err := g.dao.ListMemberIDs(c, groupID)
	if err != nil {
		return nil, err
	}

	users, err := g.dao.GetUsersByIDs(c, userIDs)
	return users, err
}
