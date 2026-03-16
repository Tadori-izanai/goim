package gateway

import (
	"context"
	"github.com/Terry-Mao/goim/internal/gateway/model"
)

func (g *Gateway) UserInfo(c context.Context, ids []int64) ([]*model.User, error) {
	users, err := g.dao.GetUsersByIDs(c, ids)
	return users, err
}

func (g *Gateway) UserByName(c context.Context, username string) (*model.User, error) {
	user, err := g.dao.GetUserByUsername(c, username)
	return user, err
}

func (g *Gateway) ListJoinedGroups(c context.Context, userID int64) ([]*model.Group, error) {
	groupIDs, err := g.dao.ListGroupIDs(c, userID)
	if err != nil {
		return nil, err
	}
	groups, err := g.dao.GetGroupByIDs(c, groupIDs)
	return groups, err
}
