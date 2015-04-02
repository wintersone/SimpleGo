package main

import (
	"log"

	"github.com/garyburd/redigo/redis"
)

const (
	redis_server = "192.168.59.103:49153"
	conn_type    = "tcp"
)

var (
	redisConn redis.Conn
)

func init() {
	var err error
	redisConn, err = redis.Dial(conn_type, redis_server)
	if err != nil {
		log.Fatalln("Error: Cannot connect to redis", err)
	}
}
