package http

import (
	"context"
	"errors"
	"gorm.io/gorm"

	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/gin-gonic/gin"
)

func helper(c *gin.Context, gatewayHandler func(context.Context, string, string) (any, error)) {
	var body struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errors_(c, RequestErr, err.Error())
		return
	}
	res, err := gatewayHandler(c, body.Username, body.Password)
	if err != nil {
		switch {
		case errors.Is(err, gateway.ErrDuplicateUsername):
			errors_(c, RequestErr, err.Error())
		case errors.Is(err, gateway.ErrInvalidCredentials):
			errors_(c, RequestErr, err.Error())
		case errors.Is(err, gorm.ErrRecordNotFound):
			errors_(c, RequestErr, err.Error())
		default:
			errors_(c, ServerErr, err.Error())
		}
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
