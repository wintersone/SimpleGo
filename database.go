package main

import (
	"log"
	"time"

	"github.com/garyburd/redigo/redis"
	"gopkg.in/boj/redistore.v1"
)

var (
	authKey    = []byte("y@b(@+fab&^PFnG$yJ5%^5TWgJt3OigHYYcb!J6(2@$UUK1S@9iajQAAL2y4Ou*=")
	encryptKey = []byte("xKB(nJhIQvc(45%*ZO!#h0KjMW!VM=$!")
)

func NewPool(server string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", server)
			if err != nil {
				log.Printf("error is %s", err)
				return nil, err
			}

			return c, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

func NewRedisStore(pool *redis.Pool) *redistore.RediStore {
	redisStore, err := redistore.NewRediStoreWithPool(pool, authKey, encryptKey)
	if err != nil {
		log.Fatal("err in init redis store")
		return nil
	}
	return redisStore
}
