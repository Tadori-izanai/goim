package gateway

import (
	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/Terry-Mao/goim/internal/gateway/dao"
	log "github.com/golang/glog"
)

type Gateway struct {
	c   *conf.Config
	dao *dao.Dao
}

func New(c *conf.Config) *Gateway {
	g := &Gateway{
		c:   c,
		dao: dao.New(c),
	}
	return g
}

func (g *Gateway) Close() {
	if err := g.dao.Close(); err != nil {
		log.Errorf("close dao error: %v", err)
	}
}
