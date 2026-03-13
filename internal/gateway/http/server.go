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
	groupAuth := s.engine.Group("/goim/auth")
	{
		groupAuth.POST("/register", s.register)
		groupAuth.POST("/login", s.login)
	}

	groupUser := s.engine.Group("/goim/user")
	{
		groupUser.GET("/info", s.userInfo)
		groupUser.GET("/username/:username", s.userByName)
	}

	groupFriend := s.engine.Group("/goim/friend")
	groupFriend.Use(jwtHandler)
	{
		groupFriend.POST("", s.addFriend)
		groupFriend.DELETE("/:friend_id", s.removeFriend)
		groupFriend.GET("", s.listFriend)
	}

	groupChat := s.engine.Group("/goim/chat")
	groupChat.Use(jwtHandler)
	{
		groupChat.POST("", s.sendMessage)
		groupChat.GET("", s.historyMessage)
	}

}
