package dao

import (
	"context"

	"github.com/Terry-Mao/goim/internal/logic/conf"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type NatsProducer struct {
	conn *nats.Conn
	js   jetstream.JetStream
	c    *conf.Nats
}

var _ Producer = new(NatsProducer)

func NewNatsProducer(c *conf.Nats) *NatsProducer {
	nc, err := nats.Connect(c.Addr)
	if err != nil {
		panic(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		panic(err)
	}
	return &NatsProducer{
		conn: nc,
		js:   js,
		c:    c,
	}
}

func (p *NatsProducer) ProduceMessage(key string, msg []byte) error {
	_, err := p.js.Publish(context.Background(), p.c.Subject, msg)
	return err
}

func (p *NatsProducer) Close() error {
	p.conn.Close()
	return nil
}
