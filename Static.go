package main

import (
	"log"
	"net/http"
)

type Static struct {
	Dir http.FileSystem
}

func (s *Static) saticHandler(w http.ResponseWriter, r *http.Request) {

	if r.Method != "GET" && r.Method != "HEAD" {
		WriteError(w, ErrNotFound)
	}

	log.Printf("path is %s", r.URL.Path)

	file := r.URL.Path
	f, err := s.Dir.Open(file)
	if err != nil {
		// discard the error?
		log.Printf("error is %s", err)
		WriteError(w, ErrNotFound)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		WriteError(w, ErrNotFound)
		return
	}

	http.ServeContent(w, r, file, fi.ModTime(), f)
}
