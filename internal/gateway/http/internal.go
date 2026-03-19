package http

import (
	"github.com/gin-gonic/gin"
	"strconv"
)

func (s *Server) ack(c *gin.Context) {
	msgID := c.Param(":msg_id")
	s.gateway.Ack(msgID)
	result(c, nil, OK)
}

func (s *Server) userOffline(c *gin.Context) {
	param := c.Param(":mid")
	mid, err := strconv.ParseInt(param, 10, 64)
	if err != nil {
		result(c, err, ServerErr)
		return
	}
	s.gateway.UserOffline(mid)
	result(c, nil, OK)
}
