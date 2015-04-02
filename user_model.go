package main

import (
	"time"

	"github.com/garyburd/redigo/redis"
)

type User struct {
	UserId   string `redis:"userId"`
	UserName string `redis:"userName"`
	Password string `redis:"password"`
	Auth     string `redis:"auth"`
	err      error
}

func (u *User) IsEqual(user *User) bool {
	if nil == u || nil == user {
		return false
	}

	if u.UserId == user.UserId {
		return true
	}

	return false
}

func (u *User) IsFollowing(user *User) bool {
	var v int
	v, u.err = redis.Int(redisConn.Do("ZSCORE", "following:"+u.UserId, user.UserId))
	if u.err != nil {
		return false
	}

	if v > 0 {
		return true
	} else {
		return false
	}
}

func (u *User) GetFollowers() int {
	var count int
	count, u.err = redis.Int(redisConn.Do("ZCARD", "followers:"+u.UserId))

	if u.err != nil {
		return 0
	} else {
		return count
	}

}

func (u *User) GetFollowing() int {
	var count int
	count, u.err = redis.Int(redisConn.Do("ZCARD", "following:"+u.UserId))

	if u.err != nil {
		return 0
	} else {
		return count
	}
}

func (u *User) Follow(user *User) {
	_, u.err = redisConn.Do("ZADD", "following:"+u.UserId, time.Now().Unix(), user.UserId)
	_, u.err = redisConn.Do("ZADD", "followers:"+user.UserId, time.Now().Unix(), u.UserId)
}

func (u *User) UnFollow(user *User) {
	_, u.err = redisConn.Do("ZREM", "following:"+u.UserId, user.UserId)
	_, u.err = redisConn.Do("ZREM", "followers:"+user.UserId, u.UserId)

}
