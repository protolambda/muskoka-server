package main

import (
	"context"
	"github.com/gorilla/mux"
	"github.com/protolambda/muskoka-server/get_task"
	"github.com/protolambda/muskoka-server/listing"
	"github.com/protolambda/muskoka-server/results"
	"github.com/protolambda/muskoka-server/upload"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	fs := http.FileServer(http.Dir("static"))

	r := mux.NewRouter()
	r.Use(loggingMiddleware)
	r.Use(corsMiddleware)
	r.HandleFunc("/upload", upload.Upload)
	r.HandleFunc("/results", results.Results)
	r.HandleFunc("/listing", listing.Listing)
	r.HandleFunc("/task", get_task.GetTask)
	r.HandleFunc("/task/{key}", get_task.GetTask)
	r.Handle("/", fs)
	// Add routes as needed

	srv := &http.Server{
		Addr:         "0.0.0.0:8080",
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	c := make(chan os.Signal, 1)
	// Catch SIGINT (Ctrl+C) and shutdown gracefully
	signal.Notify(c, os.Interrupt)
	<-c

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("shutting down")
	os.Exit(0)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length,Content-Range")
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println(r.RequestURI)
		next.ServeHTTP(w, r)
	})
}
