package main

import (
	"encoding/json"
	"errors"
	"flag"
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

func authHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		helper := DBHelper{}
		auth := getAuth(r)
		user := helper.userFromAuth(auth)
		if helper.err == nil {
			log.Printf("user is %@", user.UserName)
			context.Set(r, "user", user)
		}
		log.Printf("error is %@", helper.err)
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

type Post struct {
	UserId   string `redis:"userId"`
	Time     string `redis:"time"`
	Body     string `redis:"body"`
	UserName string
}

// Main Handlers

func registerHandler(w http.ResponseWriter, r *http.Request) {
	redisConn := redisPool.Get()
	defer redisConn.Close()

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
	password, err := encryptedPassword(r.PostFormValue("password"))
	if err != nil {
		Goback(w, r, err)
		return
	}

	userExistId, err := redisConn.Do("HGET", "users", userName)

	if err != nil {
		Goback(w, r, err)

		return
	}

	if userExistId != nil {
		Goback(w, r, errors.New("Sorry the selected username is already in use."))
		return
	}

	userId, _ := redis.Int(redisConn.Do("INCR", "next_user_id"))

	auth := securecookie.GenerateRandomKey(32)

	tableName := "user:" + strconv.Itoa(userId)

	userInfo := User{UserId: strconv.Itoa(userId), UserName: userName, Password: password, Auth: string(auth)}

	_, err = redisConn.Do("HMSET", redis.Args{}.Add(tableName).AddFlat(&userInfo)...)
	if err != nil {
		Goback(w, r, err)

		return
	}

	_, err = redisConn.Do("HSET", "users", userName, userId)

	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = redisConn.Do("HSET", "auths", auth, userId)

	if err != nil {
		Goback(w, r, err)

		return
	}

	_, err = redisConn.Do("ZADD", "users_by_time", time.Now().Unix(), userName)

	if err != nil {
		Goback(w, r, err)

		return
	}

	setSession(string(auth), w)

	templateParams := map[string]interface{}{}
	templateParams["username"] = userName
	tmplRender.HTML(w, http.StatusOK, "register", templateParams)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	userName := r.PostFormValue("username")

	password, err := encryptedPassword(r.PostFormValue("password"))
	if err != nil {
		Goback(w, r, err)
		return
	}

	if userName == "" || password == "" {
		Goback(w, r, errors.New("You need to enter both username and password to login.!"))

		return
	}

	userExistId, err := redisConn.Do("HGET", "users", userName)

	if err != nil {
		Goback(w, r, err)

		return
	}

	if userExistId == nil {
		Goback(w, r, errors.New("Wrong username or password"))

		return
	}

	userId, _ := redis.Int(redisConn.Do("HGET", "users", userName))
	tableName := "user:" + strconv.Itoa(userId)
	realPassword, _ := redis.String(redisConn.Do("hget", tableName, "password"))

	if realPassword != password {

		Goback(w, r, errors.New("Wrong username or password"))

		return
	}

	auth, err := redis.String(redisConn.Do("hget", tableName, "auth"))

	if err != nil {
		Goback(w, r, err)
		return
	}

	setSession(auth, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {

	//	tmplRender.HTML(w, http.StatusOK, "welcome", nil)
	user := context.Get(r, "user")

	if user == nil {
		tmplRender.HTML(w, http.StatusOK, "welcome", nil)

	} else {
		http.Redirect(w, r, "/home", http.StatusFound)
	}

}
func homeHandler(w http.ResponseWriter, r *http.Request) {

	helper := DBHelper{}
	user := context.Get(r, "user").(*User)
	templateParams := map[string]interface{}{}
	templateParams["user"] = user

	var start int64
	var err error
	if "" == r.FormValue("start") {
		start = int64(0)
	} else {
		start, err = strconv.ParseInt(r.FormValue("start"), 10, 64)

		if err != nil {
			start = int64(0)
		}
	}

	posts, rest := helper.getUserPosts(user.UserId, start, 10)

	if helper.err == nil {

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

func postHandler(w http.ResponseWriter, r *http.Request) {
	helper := DBHelper{}
	user := context.Get(r, "user").(*User)
	if helper.err != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	status := r.PostFormValue("status")

	if status == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	helper.post(user.UserId, status)

	if helper.err != nil {
		Goback(w, r, helper.err)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)

}

func profileHandler(w http.ResponseWriter, r *http.Request) {
	helper := DBHelper{}
	userName := r.FormValue("u")

	if userName == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	userOther := helper.getUserFromName(userName)

	if helper.err != nil {
		Goback(w, r, helper.err)
		return
	}

	templateParams := map[string]interface{}{}
	templateParams["profile"] = userOther

	userMe := context.Get(r, "user").(*User)

	templateParams["user"] = userMe

	var start int64
	var err error
	if "" == r.FormValue("start") {
		start = int64(0)
	} else {
		start, err = strconv.ParseInt(r.FormValue("start"), 10, 64)

		if err != nil {
			start = int64(0)
		}
	}

	posts, rest := helper.getUserPosts(userOther.UserId, start, 10)

	if helper.err == nil {

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

func timelineHandler(w http.ResponseWriter, r *http.Request) {
	helper := DBHelper{}
	templateParams := map[string]interface{}{}
	users := helper.getLatestUsers()

	if helper.err != nil {
		Goback(w, r, helper.err)
		return
	}

	posts, _ := helper.getLatestTimeLine(0, 50)

	if helper.err != nil {
		Goback(w, r, helper.err)
		return
	}
	templateParams["users"] = users
	templateParams["posts"] = posts

	tmplRender.HTML(w, http.StatusOK, "timeline", templateParams)
}
func followHandler(w http.ResponseWriter, r *http.Request) {
	helper := DBHelper{}

	userId := r.FormValue("uid")

	if userId == "" {
		Goback(w, r, errors.New("invalid user id"))
		return
	}

	userMe := context.Get(r, "user").(*User)

	if userMe.UserId == userId {
		Goback(w, r, errors.New("you can't follow yourself"))
		return
	}

	userMe.Follow(&User{UserId: userId})

	if userMe.err != nil {

		Goback(w, r, userMe.err)
		return
	}

	profile := helper.loadUserInfo(userId)
	if helper.err != nil {
		Goback(w, r, helper.err)
		return
	}

	http.Redirect(w, r, "/profile?u="+profile.UserName, http.StatusFound)

}

func unfollowHandler(w http.ResponseWriter, r *http.Request) {
	helper := DBHelper{}
	userId := r.FormValue("uid")

	if userId == "" {
		Goback(w, r, errors.New("invalid user id"))
		return
	}

	userMe := context.Get(r, "user").(*User)

	if userMe.UserId == userId {
		Goback(w, r, errors.New("you can't follow yourself"))
		return
	}

	userMe.UnFollow(&User{UserId: userId})

	if userMe.err != nil {
		Goback(w, r, userMe.err)
		return
	}

	profile := helper.loadUserInfo(userId)
	if helper.err != nil {
		Goback(w, r, helper.err)
		return
	}

	http.Redirect(w, r, "/Profile?u="+profile.UserName, http.StatusFound)

}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	redisConn := redisPool.Get()
	defer redisConn.Close()

	helper := DBHelper{}
	userMe := context.Get(r, "user").(*User)
	if helper.err != nil {
		Goback(w, r, helper.err)
		return
	}

	newAuth := securecookie.GenerateRandomKey(32)

	oldAuth, err := redis.String(redisConn.Do("HGET", "user:"+userMe.UserId, "auth"))

	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = redisConn.Do("HSET", "user:"+userMe.UserId, "auth", newAuth)
	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = redisConn.Do("HSET", "auths", newAuth, userMe.UserId)
	if err != nil {
		Goback(w, r, err)
		return
	}

	_, err = redisConn.Do("HDEL", "auths", oldAuth)
	if err != nil {
		Goback(w, r, err)
		return
	}

	clearSession(w)
	context.Delete(r, "user")
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

var (
	redisPool   *redis.Pool
	redisServer = flag.String("redisServer", "192.168.59.103:49153", "")
)

func main() {

	flag.Parse()
	redisPool = NewPool(*redisServer)
	defer redisPool.Close()

	satic := Static{http.Dir("public")}

	commonHandler := alice.New(context.ClearHandler, loggingHandler, recoverHandler, authHandler)

	router := NewRouter()
	router.NotFound = satic.saticHandler

	router.Get("/", commonHandler.ThenFunc(indexHandler))
	router.Get("/home", commonHandler.ThenFunc(homeHandler))
	router.Post("/post", commonHandler.ThenFunc(postHandler))
	router.Post("/register", commonHandler.ThenFunc(registerHandler))
	router.Post("/login", commonHandler.ThenFunc(loginHandler))
	router.Get("/follow", commonHandler.ThenFunc(followHandler))
	router.Get("/unfollow", commonHandler.ThenFunc(unfollowHandler))
	router.Get("/Profile", commonHandler.ThenFunc(profileHandler))
	router.Get("/timeline", commonHandler.ThenFunc(timelineHandler))
	router.Get("/logout", commonHandler.ThenFunc(logoutHandler))

	http.ListenAndServe(":8000", router)
}
