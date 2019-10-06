package get_task

import (
	"cloud.google.com/go/firestore"
	"context"
	"encoding/json"
	"github.com/gorilla/mux"
	. "github.com/protolambda/httphelpers/codes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"
)

var fsTransitionsCollection *firestore.CollectionRef

func init() {
	projectID := os.Getenv("GCP_PROJECT")
	ctx := context.Background()

	// database
	{
		firestoreClient, err := firestore.NewClient(ctx, projectID)
		if err != nil {
			log.Fatalf("Failed to create firestore client: %v", err)
		}
		fsTransitionsCollection = firestoreClient.Collection("transitions")
	}
}

type Task struct {
	Blocks      int                    `firestore:"blocks" json:"blocks"`
	SpecVersion string                 `firestore:"spec-version" json:"spec-version"`
	SpecConfig  string                 `firestore:"spec-config" json:"spec-config"`
	Created     time.Time              `firestore:"created" json:"created"`
	Results     map[string]ResultEntry `firestore:"results" json:"results"`
	// Ignored for listing purposes
	//WorkersVersioned map[string]string      `firestore:"workers-versioned"`
	//Workers          map[string]bool        `firestore:"workers"`
	//HasFail     bool                        `firestore:"has-fail"`
}

type ResultEntry struct {
	Success       bool           `firestore:"success" json:"success"`
	Created       time.Time      `firestore:"created" json:"created"`
	ClientName    string         `firestore:"client-name" json:"client-name"`
	ClientVersion string         `firestore:"client-version" json:"client-version"`
	PostHash      string         `firestore:"post-hash" json:"post-hash"`
	Files         ResultFilesRef `firestore:"files" json:"files"`
}

type ResultFilesRef struct {
	PostState string `firestore:"post-state" json:"post-state"`
	ErrLog    string `firestore:"err-log" json:"err-log"`
	OutLog    string `firestore:"out-log" json:"out-log"`
}

// make sure keys don't start with `__`, or underscores at all
var KeyRegex, _ = regexp.Compile("^[-0-9a-zA-Z=][-_0-9a-zA-Z=]{0,128}$")

func GetTask(w http.ResponseWriter, r *http.Request) {
	mVars := mux.Vars(r)
	params := r.URL.Query()
	var key string
	p, okP := params["key"]
	m, okM := mVars["key"]
	if okP && len(p) > 0 {
		key = p[0]
	} else if okM {
		key = m
	} else {
		SERVER_BAD_INPUT.Report(w, "No key specified. Set the 'key' URL param.")
		return
	}
	if !KeyRegex.Match([]byte(key)) {
		SERVER_BAD_INPUT.Report(w, "task key is invalid")
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
	dat, err := fsTransitionsCollection.Doc(key).Get(ctx)
	if status.Code(err) == codes.NotFound || (err == nil && !dat.Exists()) {
		w.WriteHeader(404)
		return
	}
	if SERVER_ERR.Check(w, err, "could not get task by key") {
		return
	}
	var task Task
	if err := dat.DataTo(&task); SERVER_ERR.Check(w, err, "could not parse task data retrieved from key") {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// TODO: experimental caching to make repeated retrieval of historical data by the same viewers cheaper.
	//  Lengths/triggers can be tweaked.
	// if older than a week -> cache for a day
	// if older than 3 hours -> cache for an hour
	// if newer than 30 seconds -> no cache
	// otherwise -> cache for 30 seconds
	if task.Created.Add(time.Hour * 24 * 7).Before(time.Now()) {
		w.Header().Set("Cache-Control", "max-age=86400") // 1 day
	} else if task.Created.Add(time.Hour * 3).Before(time.Now()) {
		w.Header().Set("Cache-Control", "max-age=3600") // 1 hour
	} else if task.Created.Add(time.Second * 30).After(time.Now()) {
		w.Header().Set("Cache-Control", "no-cache") // no cache
	} else {
		w.Header().Set("Cache-Control", "max-age=30") // half a minute
	}

	w.WriteHeader(int(SERVER_OK))
	enc := json.NewEncoder(w)
	if err := enc.Encode(&task); err != nil {
		log.Printf("failed to encode query response to JSON: ")
	}
}
