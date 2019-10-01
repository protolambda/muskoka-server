package main

import (
	"cloud.google.com/go/pubsub"
	"context"
	"fmt"
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

var pubsubClient *pubsub.Client

func main() {
	// Setup pubsub client
	{
		cl, err := pubsub.NewClient(context.Background(), os.Getenv("GCP_PROJECT"))
		if err != nil {
			log.Fatalf("Failed to create pubsub client: %v", err)
		}
		pubsubClient = cl
	}

	clients := []string{
		"zrnt",
	}
	// this is not an authenticated cloud func, but a dev environment. Just accept any client we are listening for.
	results.CheckClient = func(name string) bool {
		for _, c := range clients {
			if c == name {
				return true
			}
		}
		return false
	}

	for _, c := range clients {
		startPubsubListener(fmt.Sprintf("results~%s", c), results.Results)
	}

	fs := http.FileServer(http.Dir("static"))

	r := mux.NewRouter()
	r.Use(loggingMiddleware)
	r.Use(corsMiddleware)
	r.HandleFunc("/upload", upload.Upload)
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

func startPubsubListener(subId string, pubsubHandler func(ctx context.Context, m *pubsub.Message) error) {
	sub := pubsubClient.Subscription(subId)
	// check if the subscription exists
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*15)
		if exists, err := sub.Exists(ctx); err != nil {
			log.Fatalf("could not check if pubsub subscription exists: %v\n", err)
		} else if !exists {
			log.Fatalf("subscription %s does not exist. Either the worker was misconfigured (try --sub-id) or a new subscription needs to be created and permissioned.", subId)
		}
	}
	// configure pubsub receiver
	sub.ReceiveSettings = pubsub.ReceiveSettings{
		MaxExtension:           -1,
		MaxOutstandingMessages: 20,
		MaxOutstandingBytes:    1 << 10,
		NumGoroutines:          4,
		Synchronous:            true,
	}
	// try receiving messages
	{
		if err := sub.Receive(context.Background(), func(ctx context.Context, message *pubsub.Message) {
			if err := pubsubHandler(ctx, message); err != nil {
				log.Printf("failed pubsub function call: %v", err)
			}
		}); err != nil {
			log.Fatalf("could not receive pubsub messages: %v", err)
		}
	}
}
