package main

import (
	"errors"
	"fmt"
	"log"

	"strings"
	"time"

	"strconv"

	"github.com/garyburd/redigo/redis"
)

type DBHelper struct {
	conn redis.Conn
}

func (helper *DBHelper) userFromAuth(auth string) (*User, error) {

	if auth == "" {
		return nil, errors.New("No Authentication")
	}

	userId, err := redis.String(helper.conn.Do("HGET", "auths", auth))

	if err != nil {
		return nil, err
	}

	authSaved, err := redis.String(helper.conn.Do("HGET", "user:"+userId, "auth"))
	if err != nil {
		return nil, err
	}

	if authSaved != auth {
		return nil, errors.New("")
	}

	return helper.loadUserInfo(userId)
}

func (helper *DBHelper) loadUserInfo(userId string) (*User, error) {

	v, err := redis.Values(helper.conn.Do("HGETALL", "user:"+userId))

	if err != nil {
		return nil, err
	}

	user := &User{}

	err = redis.ScanStruct(v, user)

	log.Printf("userName is %s", user.UserName)

	if err != nil {
		return nil, err
	}

	return user, nil
}

func (helper *DBHelper) getUserFromName(userName string) (*User, error) {

	value, err := redis.String(helper.conn.Do("HGET", "users", userName))

	if err != nil {
		return nil, err
	}

	return helper.loadUserInfo(value)
}

func (helper *DBHelper) getPost(postId string) (*Post, error) {

	v, err := redis.Values(helper.conn.Do("HGETALL", "post:"+postId))
	if err != nil {
		return nil, err
	}

	post := &Post{}

	err = redis.ScanStruct(v, post)

	if err != nil {
		return nil, err
	}

	userName, err := redis.String(helper.conn.Do("hget", "user:"+post.UserId, "userName"))
	post.UserName = userName
	return post, nil
}

func (helper *DBHelper) getUserPosts(userId string, start int64, count int64) ([]*Post, int64, error) {
	values, err := redis.Strings(helper.conn.Do("LRANGE", "posts:"+userId, start, start+count-1))
	posts := []*Post{}

	for _, postId := range values {
		post, err := helper.getPost(postId)

		post.Time = strElapsed(post.Time)

		if err != nil {
			return nil, 0, err
		} else {
			posts = append(posts, post)
		}
	}

	length, err := redis.Int64(helper.conn.Do("LLEN", "posts:"+userId))

	if err != nil {
		return posts, 0, err
	} else {
		return posts, length - start - int64(len(values)), nil
	}
}

func (helper *DBHelper) getFollowers(userId string) int {
	count, err := redis.Int(helper.conn.Do("ZCARD", "followers:"+userId))

	if err != nil {
		return 0
	} else {
		return count
	}

}

func (helper *DBHelper) getFollowing(userId string) int {
	count, err := redis.Int(helper.conn.Do("ZCARD", "following:"+userId))

	if err != nil {
		return 0
	} else {
		return count
	}

}

func (helper *DBHelper) post(userId string, body string) error {

	body = strings.Replace(body, "\n", " ", -1)
	postId, err := redis.Int(helper.conn.Do("INCR", "next_post_id"))
	if err != nil {
		return err
	}

	userName, err := redis.String(helper.conn.Do("hget", "user:"+userId, "userName"))

	if err != nil {
		return err
	}

	post := Post{userId, strconv.FormatInt(time.Now().Unix(), 10), body, userName}
	tableName := "post:" + strconv.Itoa(postId)

	_, err = helper.conn.Do("HMSET", redis.Args{}.Add(tableName).AddFlat(&post)...)

	if err != nil {
		return err
	}

	followers, err := redis.Strings(helper.conn.Do("ZRANGE", "followers:"+userId, 0, -1))
	if err != nil {
		return err
	}

	followers = append(followers, userId)

	for _, followerID := range followers {
		_, err = helper.conn.Do("LPUSH", "posts:"+followerID, postId)
		if err != nil {
			return err
		}

	}

	_, err = helper.conn.Do("LPUSH", "timeline", postId)
	if err != nil {
		return err
	}

	_, err = helper.conn.Do("LTRIM", "timeline", 0, 2000)
	if err != nil {
		return err
	}

	return nil
}

func (helper *DBHelper) getLatestUsers() ([]*User, error) {
	v, err := redis.Strings(helper.conn.Do("ZREVRANGE", "users_by_time", 0, 9))
	if err != nil {
		return nil, err
	}

	users := []*User{}

	for _, userName := range v {
		users = append(users, &User{UserName: userName})
	}

	return users, nil
}
func (helper *DBHelper) getLatestTimeLine(start int64, count int64) ([]*Post, int64, error) {

	values, err := redis.Strings(helper.conn.Do("LRANGE", "timeline", start, start+count-1))
	posts := []*Post{}

	for _, postId := range values {
		post, err := helper.getPost(postId)

		post.Time = strElapsed(post.Time)

		if err != nil {
			return nil, 0, err
		} else {
			posts = append(posts, post)
		}
	}

	length, err := redis.Int64(helper.conn.Do("LLEN", "timeline"))

	if err != nil {
		return posts, 0, err
	} else {
		return posts, length - start - int64(len(values)), nil
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
