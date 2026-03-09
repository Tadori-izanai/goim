package dao

import (
	"context"
	"errors"
	"time"

	"github.com/Terry-Mao/goim/internal/logic/conf"
	log "github.com/golang/glog"
	"github.com/gomodule/redigo/redis"
)

type Producer interface {
	ProduceMessage(key string, msg []byte) error
	Close() error
}

// Dao dao.
type Dao struct {
	c           *conf.Config
	producer    Producer
	redis       *redis.Pool
	redisExpire int32
}

// New new a dao and return.
func New(c *conf.Config) *Dao {
	d := &Dao{
		c:           c,
		producer:    newProducer(c),
		redis:       newRedis(c.Redis),
		redisExpire: int32(time.Duration(c.Redis.Expire) / time.Second),
	}
	return d
}

func newProducer(c *conf.Config) (producer Producer) {
	switch c.MQType {
	case conf.MQTypeKafka:
		producer = NewKafkaProducer(c.Kafka)
	case conf.MQTypeNats:
		producer = NewNatsProducer(c.Nats)
	default:
		log.Warningf("unknown MQType: %s. Changed to %s.", c.MQType, conf.MQTypeKafka)
		producer = NewKafkaProducer(c.Kafka)
	}
	return
}

func newRedis(c *conf.Redis) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     c.Idle,
		MaxActive:   c.Active,
		IdleTimeout: time.Duration(c.IdleTimeout),
		Dial: func() (redis.Conn, error) {
			conn, err := redis.Dial(c.Network, c.Addr,
				redis.DialConnectTimeout(time.Duration(c.DialTimeout)),
				redis.DialReadTimeout(time.Duration(c.ReadTimeout)),
				redis.DialWriteTimeout(time.Duration(c.WriteTimeout)),
				redis.DialPassword(c.Auth),
			)
			if err != nil {
				return nil, err
			}
			return conn, nil
		},
	}
}

// Close close the resource.
func (d *Dao) Close() error {
	errMQ := d.producer.Close()
	errRedis := d.redis.Close()
	return errors.Join(errMQ, errRedis)
}

// Ping dao ping.
func (d *Dao) Ping(c context.Context) error {
	return d.pingRedis(c)
}
