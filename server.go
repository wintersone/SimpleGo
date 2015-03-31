package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"reflect"
	"time"

	"strconv"

	"github.com/garyburd/redigo/redis"
	"github.com/gorilla/context"
	"github.com/gorilla/securecookie"
	"github.com/julienschmidt/httprouter"
	"github.com/justinas/alice"
	"github.com/unrolled/render"
)

//Errors

var (
	tmplRender = render.New(render.Options{
		IsDevelopment: true,
	})

	redisConn redis.Conn
)

type Errors struct {
	Errors []*Error `json:"errors"`
}

type Error struct {
	Id     string `json:"id"`
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func WriteError(w http.ResponseWriter, err *Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Status)
	json.NewEncoder(w).Encode(Errors{[]*Error{err}})
}

var (
	ErrBadRequest           = &Error{"bad_request", 400, "Bad Request", "Request body is not well-formed. It must be JSON."}
	ErrNotAcceptable        = &Error{"not_acceptable", 406, "Not Acceptable", "Accept header must be set to 'application/json'."}
	ErrUnsupportedMediaType = &Error{"unsupported_media_type", 415, "Unsupported Media Type", "Content-Type header must be set to: 'application/json'."}
	ErrInternalServer       = &Error{"internal_server_error", 500, "Internal Server Error", "Something went wrong."}
	ErrNotFound             = &Error{"not_found", 404, "Not Found", "Not Found."}
)

//Display error relate html file
func Goback(w http.ResponseWriter, r *http.Request, err error) {
	templateParams := map[string]interface{}{}
	templateParams["err"] = err

	tmplRender.HTML(w, http.StatusOK, "error", templateParams)
}

//middle
func indexHandler(w http.ResponseWriter, r *http.Request) {

	tmplRender.HTML(w, http.StatusOK, "welcome", nil)
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	userName := r.PostFormValue("username")
	password := r.PostFormValue("password")
	passwordVerify := r.PostFormValue("password2")

	if userName == "" || password == "" || passwordVerify == "" {
		Goback(w, r, errors.New("Every field of the registration form is needed!"))
	}

	if password != passwordVerify {
		Goback(w, r, errors.New("The two password fileds don't match!"))
	}

}

func loggingHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		t1 := time.Now()
		next.ServeHTTP(w, r)
		t2 := time.Now()
		log.Printf("[%s] %q %v\n", r.Method, r.URL.String(), t2.Sub(t1))
	}

	return http.HandlerFunc(fn)
}

func recoverHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %+v", err)
				http.Error(w, http.StatusText(500), 500)
			}
		}()

		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

func acceptHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			WriteError(w, ErrBadRequest)
			return
		}

		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

func contentTypeHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			WriteError(w, ErrUnsupportedMediaType)
			return
		}

		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

func bodyHandler(v interface{}) func(http.Handler) http.Handler {

	t := reflect.TypeOf(v)

	m := func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			val := reflect.New(t).Interface()
			err := json.NewDecoder(r.Body).Decode(val)

			if err != nil {
				WriteError(w, ErrBadRequest)
				return
			}
			if next != nil {
				context.Set(r, "body", val)
				next.ServeHTTP(w, r)
			}

		}

		return http.HandlerFunc(fn)
	}

	return m
}

// Repo

type Response struct {
	Status string `json:"status"`

	Content interface{} `json:"content"`
}

type User struct {
	UserId   string `redis:"userId"`
	UserName string `redis:"userName"`
	Password string `redis:"password"`
	Auth     string `redis:"auth"`
}

func (u *User) IsEqual(user *User) bool {
	if u == nil || user == nil {
		return false
	}

	if u.UserId == user.UserId {
		return true
	}

	return false
}

func (u *User) IsFollowing(user *User) bool {
	v, err := redis.Int(redisConn.Do("ZSCORE", "following:"+u.UserId, user.UserId))
	if err != nil {
		return false
	}

	if v > 0 {
		return true
	} else {
		return false
	}
}

func (u *User) GetFollowers() int {

	if u == nil {
		log.Printf("nil user")
	}
	if redisConn == nil {
		log.Printf("nil redis")
	}
	count, err := redis.Int(redisConn.Do("ZCARD", "followers:"+u.UserId))

	if err != nil {
		log.Printf("error follower is %s", err)
		return 0
	} else {
		return count
	}

}

func (u *User) GetFollowing() int {
	count, err := redis.Int(redisConn.Do("ZCARD", "following:"+u.UserId))

	if err != nil {
		log.Printf("error following is %s", err)
		return 0
	} else {
		return count
	}
}

func (u *User) Follow(user *User) error {

	_, err := redisConn.Do("ZADD", "following:"+u.UserId, time.Now().Unix(), user.UserId)

	if err != nil {
		return err
	}

	_, err = redisConn.Do("ZADD", "followers:"+user.UserId, time.Now().Unix(), u.UserId)
	if err != nil {
		return err
	}

	return nil
}

func (u *User) UnFollow(user *User) error {

	_, err := redisConn.Do("ZREM", "following:"+u.UserId, user.UserId)

	if err != nil {
		return err
	}
	_, err = redisConn.Do("ZREM", "followers:"+user.UserId, u.UserId)
	if err != nil {
		return err
	}

	return nil
}

type Post struct {
	UserId   string `redis:"userId"`
	Time     string `redis:"time"`
	Body     string `redis:"body"`
	UserName string
}

// Main Handlers
type appContext struct {
	conn   redis.Conn
	helper DBHelper
}

func (c *appContext) registerHandler(w http.ResponseWriter, r *http.Request) {
	userName := r.PostFormValue("username")
	password := r.PostFormValue("password")
	passwordVerify := r.PostFormValue("password2")

	if userName == "" || password == "" || passwordVerify == "" {
		Goback(w, r, errors.New("Every field of the registration form is needed!"))
		return
	}

	if password != passwordVerify {
		Goback(w, r, errors.New("The two password fileds don't match!"))
		return
	}

	userExistId, err := c.conn.Do("HGET", "users", userName)

	if err != nil {
		Goback(w, r, err)

		return
	}

	if userExistId != nil {
		Goback(w, r, errors.New("Sorry the selected username is already in use."))
		return
	}

	userId, _ := redis.Int(c.conn.Do("INCR", "next_user_id"))

	auth := securecookie.GenerateRandomKey(32)

	tableName := "user:" + strconv.Itoa(userId)

	userInfo := User{strconv.Itoa(userId), userName, password, string(auth)}

	_, err = c.conn.Do("HMSET", redis.Args{}.Add(tableName).AddFlat(&userInfo)...)
	if err != nil {
		Goback(w, r, err)

		return
	}

	_, err = c.conn.Do("HSET", "users", userName, userId)

	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = c.conn.Do("HSET", "auths", auth, userId)

	if err != nil {
		Goback(w, r, err)

		return
	}

	_, err = c.conn.Do("ZADD", "users_by_time", time.Now().Unix(), userName)

	if err != nil {
		Goback(w, r, err)

		return
	}

	setSession(string(auth), w)

	templateParams := map[string]interface{}{}
	templateParams["username"] = userName
	tmplRender.HTML(w, http.StatusOK, "register", templateParams)
}

func (c *appContext) loginHandler(w http.ResponseWriter, r *http.Request) {
	userName := r.PostFormValue("username")
	password := r.PostFormValue("password")
	if userName == "" || password == "" {
		Goback(w, r, errors.New("You need to enter both username and password to login.!"))

		return
	}

	userExistId, err := c.conn.Do("HGET", "users", userName)

	if err != nil {
		Goback(w, r, err)

		return
	}

	if userExistId == nil {
		Goback(w, r, errors.New("Wrong username or password"))

		return
	}

	userId, _ := redis.Int(c.conn.Do("HGET", "users", userName))
	tableName := "user:" + strconv.Itoa(userId)
	realPassword, _ := redis.String(c.conn.Do("hget", tableName, "password"))

	if realPassword != password {

		Goback(w, r, errors.New("Wrong username or password"))

		return
	}

	auth, err := redis.String(c.conn.Do("hget", tableName, "auth"))

	if err != nil {
		Goback(w, r, err)
		return
	}

	setSession(auth, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (c *appContext) indexHandler(w http.ResponseWriter, r *http.Request) {

	//	tmplRender.HTML(w, http.StatusOK, "welcome", nil)

	auth := getAuth(r)

	_, err := c.helper.userFromAuth(auth)

	if err != nil {
		tmplRender.HTML(w, http.StatusOK, "welcome", nil)

	} else {
		http.Redirect(w, r, "/home", http.StatusFound)
	}

}
func (c *appContext) homeHandler(w http.ResponseWriter, r *http.Request) {
	auth := getAuth(r)
	user, err := c.helper.userFromAuth(auth)
	templateParams := map[string]interface{}{}
	templateParams["user"] = user

	var start int64
	if "" == r.FormValue("start") {
		start = int64(0)
	} else {
		start, err = strconv.ParseInt(r.FormValue("start"), 10, 64)

		if err != nil {
			start = int64(0)
		}
	}

	posts, rest, err := c.helper.getUserPosts(user.UserId, start, 10)

	if err == nil {

		templateParams["posts"] = posts

		if start > 0 {
			templateParams["prev"] = start - 10
		}

		if rest > 0 {
			templateParams["next"] = start + 10
		}
	}

	tmplRender.HTML(w, http.StatusOK, "home", templateParams)
}

func (c *appContext) postHandler(w http.ResponseWriter, r *http.Request) {

	auth := getAuth(r)
	user, err := c.helper.userFromAuth(auth)

	if err != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	status := r.PostFormValue("status")

	if status == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	err = c.helper.post(user.UserId, status)

	if err != nil {
		Goback(w, r, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)

}

func (c *appContext) profileHandler(w http.ResponseWriter, r *http.Request) {
	userName := r.FormValue("u")

	if userName == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	userOther, err := c.helper.getUserFromName(userName)

	if err != nil {
		Goback(w, r, err)
		return
	}

	templateParams := map[string]interface{}{}
	templateParams["profile"] = userOther

	auth := getAuth(r)
	userMe, err := c.helper.userFromAuth(auth)

	if err != nil {
		Goback(w, r, err)
		return
	}

	templateParams["user"] = userMe

	var start int64
	if "" == r.FormValue("start") {
		start = int64(0)
	} else {
		start, err = strconv.ParseInt(r.FormValue("start"), 10, 64)

		if err != nil {
			start = int64(0)
		}
	}

	posts, rest, err := c.helper.getUserPosts(userOther.UserId, start, 10)

	if err == nil {

		templateParams["posts"] = posts

		if start > 0 {
			templateParams["prev"] = start - 10
		}

		if rest > 0 {
			templateParams["next"] = start + 10
		}
	}

	tmplRender.HTML(w, http.StatusOK, "profile", templateParams)
}

func (c *appContext) timelineHandler(w http.ResponseWriter, r *http.Request) {
	templateParams := map[string]interface{}{}
	users, err := c.helper.getLatestUsers()

	if err != nil {
		Goback(w, r, err)
		return
	}

	posts, _, err := c.helper.getLatestTimeLine(0, 50)

	if err != nil {
		Goback(w, r, err)
		return
	}
	templateParams["users"] = users
	templateParams["posts"] = posts

	tmplRender.HTML(w, http.StatusOK, "timeline", templateParams)
}
func (c *appContext) followHandler(w http.ResponseWriter, r *http.Request) {

	userId := r.FormValue("uid")

	if userId == "" {
		Goback(w, r, errors.New("invalid user id"))
		return
	}

	auth := getAuth(r)
	userMe, err := c.helper.userFromAuth(auth)

	if userMe.UserId == userId {
		Goback(w, r, errors.New("you can't follow yourself"))
		return
	}

	err = userMe.Follow(&User{UserId: userId})

	if err != nil {

		Goback(w, r, err)
		return
	}

	profile, err := c.helper.loadUserInfo(userId)
	if err != nil {
		Goback(w, r, err)
		return
	}

	http.Redirect(w, r, "/profile?u="+profile.UserName, http.StatusFound)

}

func (c *appContext) unfollowHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.FormValue("uid")

	if userId == "" {
		Goback(w, r, errors.New("invalid user id"))
		return
	}

	auth := getAuth(r)
	userMe, err := c.helper.userFromAuth(auth)

	if userMe.UserId == userId {
		Goback(w, r, errors.New("you can't follow yourself"))
		return
	}

	err = userMe.UnFollow(&User{UserId: userId})

	if err != nil {
		Goback(w, r, err)
		return
	}

	profile, err := c.helper.loadUserInfo(userId)
	if err != nil {
		Goback(w, r, err)
		return
	}

	http.Redirect(w, r, "/Profile?u="+profile.UserName, http.StatusFound)

}

func (c *appContext) logoutHandler(w http.ResponseWriter, r *http.Request) {
	auth := getAuth(r)
	userMe, err := c.helper.userFromAuth(auth)

	if err != nil {
		Goback(w, r, err)
		return
	}

	newAuth := securecookie.GenerateRandomKey(32)

	oldAuth, err := redis.String(c.conn.Do("HGET", "user:"+userMe.UserId, "auth"))

	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = c.conn.Do("HSET", "user:"+userMe.UserId, "auth", newAuth)
	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = c.conn.Do("HSET", "auths", newAuth, userMe.UserId)
	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = c.conn.Do("HDEL", "auths", oldAuth)
	if err != nil {
		Goback(w, r, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

type router struct {
	*httprouter.Router
}

func (r *router) Get(path string, handler http.Handler) {
	r.GET(path, wrapHandler(handler))
}

func (r *router) Post(path string, handler http.Handler) {
	r.POST(path, wrapHandler(handler))
}

func (r *router) Handle(path string, handler http.Handler) {
	r.Handle(path, handler)
}

func NewRouter() *router {
	return &router{httprouter.New()}
}

func wrapHandler(h http.Handler) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		context.Set(r, "params", ps)
		h.ServeHTTP(w, r)
	}
}

func main() {

	var err error
	redisConn, err = redis.Dial("tcp", "192.168.59.103:49153")

	if err != nil {
		panic(err)
	}

	defer redisConn.Close()

	appC := appContext{redisConn, DBHelper{redisConn}}

	satic := Static{http.Dir("public")}

	commonHandler := alice.New(context.ClearHandler, loggingHandler, recoverHandler)

	router := NewRouter()
	router.NotFound = satic.saticHandler

	router.Get("/", commonHandler.ThenFunc(appC.indexHandler))
	router.Get("/home", commonHandler.ThenFunc(appC.homeHandler))
	router.Post("/post", commonHandler.ThenFunc(appC.postHandler))
	router.Post("/register", commonHandler.ThenFunc(appC.registerHandler))
	router.Post("/login", commonHandler.ThenFunc(appC.loginHandler))
	router.Get("/follow", commonHandler.ThenFunc(appC.followHandler))
	router.Get("/unfollow", commonHandler.ThenFunc(appC.unfollowHandler))
	router.Get("/Profile", commonHandler.ThenFunc(appC.profileHandler))
	router.Get("/timeline", commonHandler.ThenFunc(appC.timelineHandler))
	router.Get("/logout", commonHandler.ThenFunc(appC.logoutHandler))

	http.ListenAndServe(":8000", router)
}
