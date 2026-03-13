package gateway

import (
	"context"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"github.com/gin-gonic/gin"
)

func (g *Gateway) addFriend(c context.Context, userID, friendID int64) error {
	return nil
}

func (g *Gateway) removeFriend(c context.Context, userID, friendID int64) error {
	return nil
}

func (g *Gateway) listFriend(c *gin.Context, userID int64) ([]*model.User, error) {
	return nil, nil
}
