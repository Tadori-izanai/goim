package http

import (
	"github.com/Terry-Mao/goim/internal/gateway"
	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/gin-gonic/gin"
)

// Server is http server.
type Server struct {
	engine  *gin.Engine
	gateway *gateway.Gateway
}

func New(c *conf.HTTPServer, g *gateway.Gateway) *Server {
	engine := gin.New()
	engine.Use(loggerHandler, recoverHandler)
	s := &Server{
		engine:  engine,
		gateway: g,
	}
	s.initRouter()
	go func() {
		if err := engine.Run(c.Addr); err != nil {
			panic(err)
		}
	}()
	return s
}

// Close close the server.
func (s *Server) Close() {}

func (s *Server) initRouter() {
	group := s.engine.Group("/goim")
	group.POST("/auth/register", s.register)
	group.POST("/auth/login", s.login)
}
