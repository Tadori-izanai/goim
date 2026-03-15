package http

import (
	"errors"
	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

func (s *Server) sendMessage(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}

	var body struct {
		To          int64  `json:"to"`
		ContentType int8   `json:"content_type"`
		Content     string `json:"content"`
	}
	if err := c.BindJSON(&body); err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	msgID, err := s.gateway.SendMessage(c, userID, body.To, body.ContentType, body.Content)
	if err != nil {
		switch {
		case errors.Is(err, gateway.ErrNotFriend):
			errors_(c, RequestErr, gateway.ErrNotFriend.Error())
		default:
			errors_(c, ServerErr, err.Error())
		}
		return
	}
	result(c, msgID, OK)
}

func (s *Server) historyMessage(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}

	sinceMs, err := strconv.ParseInt(c.Query("since"), 10, 64)
	if err != nil {
		errors_(c, RequestErr, "invalid since parameter")
		return
	}
	sinceTime := time.UnixMilli(sinceMs)

	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil {
		errors_(c, RequestErr, "invalid limit parameter")
		return
	}

	messages, err := s.gateway.HistoryMessage(c, userID, sinceTime, limit)
	if err != nil {
		errors_(c, ServerErr, err.Error())
		return
	}
	result(c, messages, OK)
}
