package http

import (
	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/gin-gonic/gin"
)

func (s *Server) userInfo(c *gin.Context) {
	var arg struct {
		IDs []int64 `form:"ids" binding:"required"`
	}
	if err := c.BindQuery(&arg); err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}
	res, err := s.gateway.UserInfo(c, arg.IDs)
	if err != nil {
		result(c, nil, RequestErr)
		return
	}
	result(c, res, OK)
}

func (s *Server) userByName(c *gin.Context) {
	username := c.Param("username")
	res, err := s.gateway.UserByName(c, username)
	if err != nil {
		result(c, nil, RequestErr)
		return
	}
	result(c, res, OK)
}

func (s *Server) listJoinedGroups(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}
	groups, err := s.gateway.ListJoinedGroups(c, userID)
	if err != nil {
		errors_(c, ServerErr, err.Error())
		return
	}
	result(c, groups, OK)
}
