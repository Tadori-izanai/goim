package http

import (
	"context"
	"github.com/gin-gonic/gin"
)

func helper(c *gin.Context, gatewayHandler func(context.Context, string, string) (any, error)) {
	var body struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errors(c, RequestErr, err.Error())
		return
	}
	res, err := gatewayHandler(c, body.Username, body.Password)
	if err != nil {
		result(c, nil, ServerErr) // todo: refine error code
		return
	}
	result(c, res, OK)
}

func (s *Server) register(c *gin.Context) {
	helper(c, s.gateway.Register)
}

func (s *Server) login(c *gin.Context) {
	helper(c, s.gateway.Login)
}
