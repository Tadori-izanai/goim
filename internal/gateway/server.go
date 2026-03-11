package gateway

import (
	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/Terry-Mao/goim/internal/gateway/dao"
	log "github.com/golang/glog"
)

type Server struct {
	c   *conf.Config
	dao *dao.Dao
}

func New(c *conf.Config) *Server {
	s := &Server{
		c:   c,
		dao: dao.New(c),
	}
	return s
}

func (s *Server) Close() {
	if err := s.dao.Close(); err != nil {
		log.Errorf("close dao error: %v", err)
	}
}
