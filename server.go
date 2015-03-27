package main

import (
	"net/http"
	"fmt"
	"time"
	"log"
	"github.com/justinas/alice"
	"github.com/gorilla/context"
	"github.com/julienschmidt/httprouter"
	"encoding/json"
	"reflect"
	"github.com/garyburd/redigo/redis"
)


//Errors

type Errors struct {
	Errors []*Error `json:"errors"`
}

type Error struct {
	Id string `json:"id"`
	Status int `json:"status"`
	Title string `json:"title"`
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
)


//middle 
func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Welcome Ha Ha Ha Ha!")
}

func loggingHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request){
		t1 := time.Now()
		next.ServeHTTP(w, r)
		t2 := time.Now()
		log.Printf("[%s] %q %v\n", r.Method, r.URL.String(), t2.Sub(t1))
	}

	return http.HandlerFunc(fn)
}

func recoverHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request){
		defer func(){
			if err := recover(); err != nil  {
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

type Tea struct {
	Name string      `redis:"name"`
	Category string  `redis:"category"`
}

type TeaCollection struct {
	Data []Tea `json:"data"`
}

type TeaResource struct {
	Data Tea `json:"data"`
}


// Main Handlers
type appContext struct {
	conn redis.Conn
}

func (c *appContext)teaHandler(w http.ResponseWriter, r *http.Request) {

	params := context.Get(r, "params").(httprouter.Params)
	id := params.ByName("id")
	tableName := "tea:"+id

	v,_ := redis.StringMap(c.conn.Do("HGETALL", tableName))
	
	strB,_ := json.Marshal(v)
	
	tea := Tea{}
	
	json.Unmarshal([]byte(strB), &tea)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tea)
}

func (c *appContext)createTeaHandler(w http.ResponseWriter, r *http.Request) {

	body := context.Get(r, "body").(*TeaResource)
	tableName := "tea:2"
	_,err := c.conn.Do("HMSET",redis.Args{}.Add(tableName).AddFlat(&body.Data)...)

	if err != nil {
		panic(err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(body)
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

	redisConn, err := redis.Dial("tcp", "192.168.59.103:49153")

	if err != nil {
		panic(err)
	}

	defer redisConn.Close()

	appC := appContext{redisConn}

	commonHandler := alice.New(context.ClearHandler,loggingHandler,recoverHandler,acceptHandler)
	router := NewRouter()

	router.Get("/teas/:id", commonHandler.ThenFunc(appC.teaHandler))
	router.Post("/teas", commonHandler.Append(contentTypeHandler, bodyHandler(TeaResource{})).ThenFunc(appC.createTeaHandler))
	
	http.ListenAndServe(":8000", router)
}
