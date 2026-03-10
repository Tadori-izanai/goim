package job

import (
	"context"

	"github.com/Terry-Mao/goim/internal/job/conf"
	log "github.com/golang/glog"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type NatsConsumer struct {
	conn *nats.Conn
	js   jetstream.JetStream
	c    *conf.Nats
	done chan struct{}
}

var _ Consumer = new(NatsConsumer)

func NewNatsConsumer(c *conf.Nats) *NatsConsumer {
	nc, err := nats.Connect(c.Addr)
	if err != nil {
		panic(err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		panic(err)
	}
	return &NatsConsumer{
		conn: nc,
		js:   js,
		c:    c,
		done: make(chan struct{}),
	}
}

func (n *NatsConsumer) Consume(handler func(msg []byte)) {
	ctx := context.Background()
	_, err := n.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "goim",
		Subjects: []string{n.c.Subject},
	})
	if err != nil {
		log.Errorf("nats jetstream create stream error(%v)", err)
		return
	}
	cons, err := n.js.CreateOrUpdateConsumer(ctx, "goim", jetstream.ConsumerConfig{
		Durable:       "goim-job",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: n.c.Subject,
	})
	if err != nil {
		log.Errorf("nats jetstream create consumer error(%v)", err)
		return
	}
	cctx, err := cons.Consume(func(msg jetstream.Msg) {
		handler(msg.Data())
		if err := msg.Ack(); err != nil {
			log.Errorf("nats jetstream ack error(%v)", err)
		}
		log.Infof("consume: %s", msg.Subject())
	})
	if err != nil {
		log.Errorf("nats jetstream consume error(%v)", err)
		return
	}
	<-n.done
	cctx.Stop()
}

func (n *NatsConsumer) Close() error {
	close(n.done)
	n.conn.Close()
	return nil
}
