package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
)

type DBHelper struct {
	err error
}

func (helper *DBHelper) userFromAuth(auth string) *User {

	redisConn := redisPool.Get()
	defer redisConn.Close()

	if auth == "" {
		helper.err = errors.New("No Authentication")
		return nil
	}

	var userId, authSaved string

	userId, helper.err = redis.String(redisConn.Do("HGET", "auths", auth))

	authSaved, helper.err = redis.String(redisConn.Do("HGET", "user:"+userId, "auth"))

	if authSaved != auth {
		helper.err = errors.New("invalid auth")
		return nil
	}

	return helper.loadUserInfo(userId)
}

func (helper *DBHelper) loadUserInfo(userId string) *User {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var value []interface{}
	value, helper.err = redis.Values(redisConn.Do("HGETALL", "user:"+userId))
	user := &User{}
	helper.err = redis.ScanStruct(value, user)
	if helper.err != nil {
		return nil
	}
	return user
}

func (helper *DBHelper) getUserFromName(userName string) *User {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var value string
	value, helper.err = redis.String(redisConn.Do("HGET", "users", userName))

	return helper.loadUserInfo(value)
}

func (helper *DBHelper) getPost(postId string) *Post {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var values []interface{}
	values, helper.err = redis.Values(redisConn.Do("HGETALL", "post:"+postId))
	post := &Post{}
	helper.err = redis.ScanStruct(values, post)

	post.UserName, helper.err = redis.String(redisConn.Do("hget", "user:"+post.UserId, "userName"))
	return post
}

func (helper *DBHelper) getUserPosts(userId string, start int64, count int64) ([]*Post, int64) {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var (
		values []string
		length int64
	)
	values, helper.err = redis.Strings(redisConn.Do("LRANGE", "posts:"+userId, start, start+count-1))
	posts := []*Post{}

	for _, postId := range values {
		post := helper.getPost(postId)
		post.Time = strElapsed(post.Time)
		posts = append(posts, post)
	}

	length, helper.err = redis.Int64(redisConn.Do("LLEN", "posts:"+userId))

	if helper.err != nil {
		return posts, 0
	} else {
		return posts, length - start - int64(len(values))
	}
}

func (helper *DBHelper) getFollowers(userId string) int {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var count int
	count, helper.err = redis.Int(redisConn.Do("ZCARD", "followers:"+userId))
	if helper.err != nil {
		return 0
	} else {
		return count
	}

}

func (helper *DBHelper) getFollowing(userId string) int {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var count int
	count, helper.err = redis.Int(redisConn.Do("ZCARD", "following:"+userId))

	if helper.err != nil {
		return 0
	} else {
		return count
	}

}

func (helper *DBHelper) post(userId string, body string) {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var (
		postId    int
		userName  string
		followers []string
	)
	body = strings.Replace(body, "\n", " ", -1)
	postId, helper.err = redis.Int(redisConn.Do("INCR", "next_post_id"))
	userName, helper.err = redis.String(redisConn.Do("hget", "user:"+userId, "userName"))
	post := Post{userId, strconv.FormatInt(time.Now().Unix(), 10), body, userName}
	tableName := "post:" + strconv.Itoa(postId)
	_, helper.err = redisConn.Do("HMSET", redis.Args{}.Add(tableName).AddFlat(&post)...)
	followers, helper.err = redis.Strings(redisConn.Do("ZRANGE", "followers:"+userId, 0, -1))
	followers = append(followers, userId)

	for _, followerID := range followers {
		_, helper.err = redisConn.Do("LPUSH", "posts:"+followerID, postId)
	}
	_, helper.err = redisConn.Do("LPUSH", "timeline", postId)
	_, helper.err = redisConn.Do("LTRIM", "timeline", 0, 2000)
}

func (helper *DBHelper) getLatestUsers() []*User {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var (
		values []string
		users  = []*User{}
	)
	values, helper.err = redis.Strings(redisConn.Do("ZREVRANGE", "users_by_time", 0, 9))

	for _, userName := range values {
		users = append(users, &User{UserName: userName})
	}

	return users
}
func (helper *DBHelper) getLatestTimeLine(start int64, count int64) ([]*Post, int64) {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	var (
		values []string
		posts  = []*Post{}
		length int64
	)
	values, helper.err = redis.Strings(redisConn.Do("LRANGE", "timeline", start, start+count-1))

	for _, postId := range values {
		post := helper.getPost(postId)

		post.Time = strElapsed(post.Time)
		posts = append(posts, post)
	}

	length, helper.err = redis.Int64(redisConn.Do("LLEN", "timeline"))

	if helper.err != nil {
		return posts, 0
	} else {
		return posts, length - start - int64(len(values))
	}
}

func strElapsed(t string) string {

	ts, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return ""
	}
	te := time.Now().Unix() - ts
	if te < 60 {
		return fmt.Sprintf("%d seconds", te)
	}
	if te < 3600 {
		m := int(te / 60)
		if m > 1 {
			return fmt.Sprintf("%d minutes", m)
		} else {
			return fmt.Sprintf("%d minute", m)
		}
	}
	if te < 3600*24 {
		h := int(te / 3600)
		if h > 1 {
			return fmt.Sprintf("%d hours", h)
		} else {
			return fmt.Sprintf("%d hour", h)
		}
	}
	d := int(te / (3600 * 24))
	if d > 1 {
		return fmt.Sprintf("%d days", d)
	} else {
		return fmt.Sprintf("%d day", d)
	}
}
