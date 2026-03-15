package http

import (
	"errors"
	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/gin-gonic/gin"
	"strconv"
)

func getFriendIDFromRouter(c *gin.Context) (int64, error) {
	param := c.Param("friend_id")
	friendID, err := strconv.ParseInt(param, 10, 64)
	return friendID, err
}

func (s *Server) addFriend(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}
	friendID, err := getFriendIDFromRouter(c)
	if err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	err = s.gateway.AddFriend(c, userID, friendID)
	if err != nil {
		switch {
		case errors.Is(err, gateway.ErrUserNotFound):
			errors_(c, RequestErr, err.Error())
		case errors.Is(err, gateway.ErrFriendSelf):
			errors_(c, RequestErr, err.Error())
		default:
			errors_(c, ServerErr, err.Error())
		}
		return
	}
	result(c, nil, OK)
}

func (s *Server) removeFriend(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}
	friendID, err := getFriendIDFromRouter(c)
	if err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	err = s.gateway.RemoveFriend(c, userID, friendID)
	if err != nil {
		switch {
		case errors.Is(err, gateway.ErrNotFriend):
			errors_(c, RequestErr, err.Error())
		default:
			errors_(c, ServerErr, err.Error())
		}
		return
	}
	result(c, nil, OK)
}

func (s *Server) listFriend(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}
	friends, err := s.gateway.ListFriend(c, userID)
	if err != nil {
		errors_(c, ServerErr, err.Error())
		return
	}
	result(c, friends, OK)
}
