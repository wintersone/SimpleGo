package main

import (
	"net/http"

	"github.com/dchest/scrypt"
	"github.com/gorilla/securecookie"
)

var (
	cookieHandler = securecookie.New(
		[]byte("y@b(@+fab&^PFnG$yJ5%^5TWgJt3OigHYYcb!J6(2@$UUK1S@9iajQAAL2y4Ou*="),
		[]byte("xKB(nJhIQvc(45%*ZO!#h0KjMW!VM=$!"))
)

const (
	cookieName = "auth"
	cookieKey  = "auth"
	saltKey    = "+acxKecey7bX3f$WwmLgku%m&+l#L0@S"
)

func getAuth(r *http.Request) (auth string) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		cookieValue := make(map[string]string)
		if err = cookieHandler.Decode(cookieName, cookie.Value, &cookieValue); err == nil {
			auth = cookieValue[cookieKey]
		}
	}
	return auth
}

func setSession(auth string, w http.ResponseWriter) {
	value := map[string]string{
		cookieKey: auth,
	}
	if encoded, err := cookieHandler.Encode(cookieName, value); err == nil {
		cookie := &http.Cookie{
			Name:  cookieName,
			Value: encoded,
			Path:  "/",
		}
		http.SetCookie(w, cookie)
	}
}

func clearSession(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	}
	http.SetCookie(w, cookie)
}

func encryptedPassword(password string) (string, error) {
	dk, err := scrypt.Key([]byte(password), []byte(saltKey), 16384, 8, 1, 32)
	return string(dk), err
}
