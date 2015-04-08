package main

import (
	"log"
	"net/http"

	"github.com/dchest/scrypt"

	"github.com/gorilla/sessions"
)

const (
	sessionName = "Auth"
	userKey     = "UserId"
	saltKey     = "+acxKecey7bX3f$WwmLgku%m&+l#L0@S"
)

func getUser(r *http.Request) (userId string) {
	session, err := redisStore.Get(r, sessionName)
	if err != nil {
		log.Printf("err in get session %v", err)
	}

	if session.Values[userKey] == nil {
		return ""
	}
	return session.Values[userKey].(string)
}

func setSession(auth string, r *http.Request, w http.ResponseWriter) {
	session, err := redisStore.Get(r, sessionName)
	if err != nil {
		log.Printf("err in get session %v", err)
	}

	session.Values[userKey] = auth

	saveSession(r, w)
}

func clearSession(r *http.Request, w http.ResponseWriter) {
	session, err := redisStore.Get(r, sessionName)
	if err != nil {
		log.Printf("err in get session %v", err)
	}

	session.Options.MaxAge = -1

	saveSession(r, w)
}

func saveSession(r *http.Request, w http.ResponseWriter) {
	if err := sessions.Save(r, w); err != nil {
		log.Printf("err in get session %v", err)
	}
}

func encryptedPassword(password string) (string, error) {
	dk, err := scrypt.Key([]byte(password), []byte(saltKey), 16384, 8, 1, 32)
	return string(dk), err
}
