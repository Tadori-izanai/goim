package job

import (
	"github.com/Terry-Mao/goim/internal/job/conf"
	log "github.com/golang/glog"
	"github.com/nats-io/nats.go"
)

type NatsConsumer struct {
	conn *nats.Conn
	c    *conf.Nats
	done chan struct{}
}

var _ Consumer = new(NatsConsumer)

func NewNatsConsumer(c *conf.Nats) *NatsConsumer {
	return &NatsConsumer{
		conn: newNatsConn(c),
		c:    c,
		done: make(chan struct{}),
	}
}

func newNatsConn(c *conf.Nats) *nats.Conn {
	nc, err := nats.Connect(c.Addr)
	if err != nil {
		panic(err)
	}
	return nc
}

func (n *NatsConsumer) Consume(handler func(msg []byte)) {
	_, err := n.conn.Subscribe(n.c.Subject, func(msg *nats.Msg) {
		handler(msg.Data)
		log.Infof("consume: %s", msg.Subject)
	})
	if err != nil {
		log.Errorf("nats subscribe error(%v)", err)
		return
	}
	<-n.done
}

func (n *NatsConsumer) Close() error {
	close(n.done)
	n.conn.Close()
	return nil
}
