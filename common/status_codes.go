package common

import (
	"fmt"
	"log"
	"net/http"
)

type StatCode int

const (
	SERVER_OK        StatCode = 200
	SERVER_ERR       StatCode = 500
	SERVER_BAD_INPUT StatCode = 400
)

func (s StatCode) Report(w http.ResponseWriter, msg string) {
	w.WriteHeader(int(s))
	log.Println(msg)
	_, _ = fmt.Fprintln(w, msg)
}

func (s StatCode) Check(w http.ResponseWriter, err error, msg string) bool {
	if err != nil {
		log.Println(msg)
		log.Println(err)
		_, _ = fmt.Fprintln(w, msg)
		w.WriteHeader(int(s))
		return true
	} else {
		return false
	}
}
