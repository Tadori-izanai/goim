package job

import (
	"github.com/Terry-Mao/goim/internal/job/conf"
	cluster "github.com/bsm/sarama-cluster"
	log "github.com/golang/glog"
)

type KafkaConsumer struct {
	consumer *cluster.Consumer
}

var _ Consumer = new(KafkaConsumer)

func NewKafkaConsumer(c *conf.Kafka) *KafkaConsumer {
	return &KafkaConsumer{
		consumer: newKafkaSub(c),
	}
}

func newKafkaSub(c *conf.Kafka) *cluster.Consumer {
	config := cluster.NewConfig()
	config.Consumer.Return.Errors = true
	config.Group.Return.Notifications = true
	consumer, err := cluster.NewConsumer(c.Brokers, c.Group, []string{c.Topic}, config)
	if err != nil {
		panic(err)
	}
	return consumer
}

func (c *KafkaConsumer) Consume(handler func(msg []byte)) {
	for {
		select {
		case err := <-c.consumer.Errors():
			log.Errorf("consumer error(%v)", err)
		case n := <-c.consumer.Notifications():
			log.Infof("consumer rebalanced(%v)", n)
		case msg, ok := <-c.consumer.Messages():
			if !ok {
				return
			}
			c.consumer.MarkOffset(msg, "")
			handler(msg.Value)
			log.Infof("consume: %s/%d/%d\t%s", msg.Topic, msg.Partition, msg.Offset, msg.Key)
		}
	}
}

func (c *KafkaConsumer) Close() error {
	return c.consumer.Close()
}
