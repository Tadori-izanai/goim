package http

import (
	"errors"
	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/gin-gonic/gin"
	"strconv"
)

func getGroupIDFromRouter(c *gin.Context) (int64, error) {
	param := c.Param("group_id")
	groupID, err := strconv.ParseInt(param, 10, 64)
	return groupID, err
}

func (s *Server) createGroup(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	group, err := s.gateway.CreateGroup(c, userID, body.Name)
	if err != nil {
		errors_(c, ServerErr, err.Error())
		return
	}
	result(c, group, OK)
}

func (s *Server) joinGroup(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}
	groupID, err := getGroupIDFromRouter(c)
	if err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	err = s.gateway.JoinGroup(c, groupID, userID)
	if err != nil {
		switch {
		case errors.Is(err, gateway.ErrGroupNotFound):
			errors_(c, RequestErr, err.Error())
		default:
			errors_(c, ServerErr, err.Error())
		}
		return
	}
	result(c, nil, OK)
}

func (s *Server) quitGroup(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}
	groupID, err := getGroupIDFromRouter(c)
	if err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	err = s.gateway.QuitGroup(c, groupID, userID)
	if err != nil {
		switch {
		case errors.Is(err, gateway.ErrGroupNotFound):
			errors_(c, RequestErr, err.Error())
		case errors.Is(err, gateway.ErrNotGroupMember):
			errors_(c, RequestErr, err.Error())
		default:
			errors_(c, ServerErr, err.Error())
		}
		return
	}
	result(c, nil, OK)
}

func (s *Server) listGroupMembers(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}
	groupID, err := getGroupIDFromRouter(c)
	if err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	users, err := s.gateway.ListGroupMembers(c, groupID, userID)
	if err != nil {
		switch {
		case errors.Is(err, gateway.ErrGroupNotFound):
			errors_(c, RequestErr, err.Error())
		case errors.Is(err, gateway.ErrNotGroupMember):
			errors_(c, RequestErr, err.Error())
		default:
			errors_(c, ServerErr, err.Error())
		}
		return
	}
	result(c, users, OK)
}
