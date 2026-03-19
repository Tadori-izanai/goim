package gateway

import (
	"net/http"
	"time"

	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/Terry-Mao/goim/internal/gateway/dao"
	log "github.com/golang/glog"
)

type Gateway struct {
	c      *conf.Config
	dao    *dao.Dao
	client *http.Client
	ack    *ackService
}

func New(c *conf.Config) *Gateway {
	g := &Gateway{
		c:   c,
		dao: dao.New(c),
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 32,
			},
			Timeout: 5 * time.Second,
		},
		ack: newAckService(c.ACK),
	}
	return g
}

func (g *Gateway) Close() {
	if err := g.dao.Close(); err != nil {
		log.Errorf("close dao error: %v", err)
	}
}
