package dao

import (
	"github.com/Terry-Mao/goim/internal/logic/conf"
	"github.com/nats-io/nats.go"
)

type NatsProducer struct {
	conn *nats.Conn
	c    *conf.Nats
}

var _ Producer = new(NatsProducer)

func NewNatsProducer(c *conf.Nats) *NatsProducer {
	nc, err := nats.Connect(c.Addr)
	if err != nil {
		panic(err)
	}
	return &NatsProducer{
		conn: nc,
		c:    c,
	}
}

func (p *NatsProducer) ProduceMessage(key string, msg []byte) error {
	return p.conn.Publish(p.c.Subject+key, msg)
}

func (p *NatsProducer) Close() error {
	p.conn.Close()
	return nil
}
