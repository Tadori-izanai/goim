package http

import (
	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

func (s *Server) syncMessages(c *gin.Context) {
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

	syncResults, err := s.gateway.SyncMessages(c, userID, sinceTime, limit)
	if err != nil {
		errors_(c, ServerErr, err.Error())
		return
	}
	result(c, syncResults, OK)
}

func (s *Server) syncAck(c *gin.Context) {
	userID, ok := getUserIDFromBearer(c)
	if !ok {
		errors_(c, RequestErr, gateway.ErrInvalidCredentials.Error())
		return
	}

	var body struct {
		AckAt model.UnixMilliTime `json:"ack_at"`
	}
	if err := c.BindJSON(&body); err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}

	err := s.gateway.SyncAck(c, userID, time.Time(body.AckAt))
	if err != nil {
		errors_(c, ServerErr, err.Error())
		return
	}
	result(c, nil, OK)
}
