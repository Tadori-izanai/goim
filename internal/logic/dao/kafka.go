package dao

import (
	"github.com/Terry-Mao/goim/internal/logic/conf"
	kafka "gopkg.in/Shopify/sarama.v1"
)

type KafkaProducer struct {
	kafkaPub kafka.SyncProducer
	c        *conf.Kafka
}

var _ Producer = new(KafkaProducer)

func NewKafkaProducer(c *conf.Kafka) *KafkaProducer {
	return &KafkaProducer{
		kafkaPub: newKafkaPub(c),
		c:        c,
	}
}

func newKafkaPub(c *conf.Kafka) kafka.SyncProducer {
	kc := kafka.NewConfig()
	kc.Producer.RequiredAcks = kafka.WaitForAll // Wait for all in-sync replicas to ack the message
	kc.Producer.Retry.Max = 10                  // Retry up to 10 times to produce the message
	kc.Producer.Return.Successes = true
	pub, err := kafka.NewSyncProducer(c.Brokers, kc)
	if err != nil {
		panic(err)
	}
	return pub
}

func (p *KafkaProducer) ProduceMessage(key string, msg []byte) error {
	m := &kafka.ProducerMessage{
		Key:   kafka.StringEncoder(key),
		Topic: p.c.Topic,
		Value: kafka.ByteEncoder(msg),
	}
	_, _, err := p.kafkaPub.SendMessage(m)
	return err
}

func (p *KafkaProducer) Close() error {
	return p.kafkaPub.Close()
}
