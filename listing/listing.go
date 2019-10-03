package listing

import (
	"cloud.google.com/go/firestore"
	"context"
	"encoding/json"
	. "github.com/protolambda/muskoka-server/common"
	"google.golang.org/api/iterator"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var fsTransitionsCollection *firestore.CollectionRef

var defaultResultsCount = 10
var maxResultsCount = 20

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
	// ignored by firestore. But used to uniquely identify the task, and fetch its contents from storage.
	Key string `firestore:"-" json:"key"`
	// Ignored for listing purposes
	//WorkersVersioned map[string]string      `firestore:"workers-versioned"`
	//Workers          map[string]bool        `firestore:"workers"`
}

type ResultEntry struct {
	Success       bool      `firestore:"success" json:"success"`
	Created       time.Time `firestore:"created" json:"created"`
	ClientName    string    `firestore:"client-name" json:"client-name"`
	ClientVersion string    `firestore:"client-version" json:"client-version"`
	PostHash      string    `firestore:"post-hash" json:"post-hash"`
	Files         ResultFilesRef `firestore:"files" json:"files"`
}

type ResultFilesRef struct {
	PostStateURL string `firestore:"post-state" json:"post-state"`
	ErrLogURL    string `firestore:"err-log" json:"err-log"`
	OutLogURL    string `firestore:"out-log" json:"out-log"`
}

func Listing(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := fsTransitionsCollection.Query
	// do not select "workers" or "workers-versioned" helper fields.
	selectedKeys := []string{"blocks", "spec-version", "spec-config", "created", "results"}
	// paginate forwards by continuing after a given result
	if p, ok := params["after"]; ok && len(p) > 0 {
		q = q.StartAfter(p[0])
	}
	// paginate backwards by stopping before a given result
	if p, ok := params["before"]; ok && len(p) > 0 {
		q = q.EndBefore(p[0])
	}
	if p, ok := params["limit"]; ok && len(p) > 0 {
		limit, err := strconv.ParseUint(p[0], 10, 32)
		if SERVER_BAD_INPUT.Check(w, err, "invalid limit") {
			return
		}
		if limit > uint64(maxResultsCount) {
			SERVER_BAD_INPUT.Report(w, "limit is too much")
			return
		}
		q = q.Limit(int(limit))
	} else {
		q = q.Limit(defaultResultsCount)
	}
	if p, ok := params["order"]; ok && len(p) > 0 {
		// explicit order names, for future compatibility
		switch p[0] {
		case "created-asc":
			q.OrderBy("created", firestore.Asc)
		case "created-desc":
			q.OrderBy("created", firestore.Desc)
		default:
			SERVER_BAD_INPUT.Report(w, "unrecognized order")
			return
		}
	} else {
		// default to latest-first
		q.OrderBy("created", firestore.Desc)
	}
	if p, ok := params["has-fail"]; ok && len(p) > 0 && p[0] == "true" {
		q = q.Where("has-fail", "==", true)
	}
	if p, ok := params["with-files"]; ok && len(p) > 0 && p[0] == "true" {
		selectedKeys = append(selectedKeys, "files")
	}
	if p, ok := params["spec-version"]; ok && len(p) > 0 {
		q = q.Where("spec-version", "==", p[0])
	}
	if p, ok := params["spec-config"]; ok && len(p) > 0 {
		q = q.Where("spec-config", "==", p[0])
	}
	for k, v := range params {
		if strings.HasPrefix(k, "client-") {
			clientName := k[len("client-"):]
			if !ClientNameRegex.Match([]byte(clientName)) {
				SERVER_BAD_INPUT.Report(w, "client name is invalid")
				return
			}
			if len(v) > 0 && v[0] != "all" {
				if !VersionRegex.Match([]byte(v[0])) {
					SERVER_BAD_INPUT.Report(w, "client version is invalid")
					return
				}
				// look for the specific version
				q = q.WherePath([]string{"workers-versioned", clientName}, "==", v[0])
			} else {
				// just that the key is present.
				q = q.WherePath([]string{"workers", v[0]}, "==", true)
			}
		}
	}
	q = q.Select(selectedKeys...)

	ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
	docsIter := q.Documents(ctx)
	outputList := make([]Task, 0)
	for {
		doc, err := docsIter.Next()
		if err == iterator.Done {
			break
		}
		if SERVER_ERR.Check(w, err, "could not process query result") {
			return
		}
		i := len(outputList)
		outputList = append(outputList, Task{})
		d := doc.Data()
		log.Printf("data: %v\n", d)
		if SERVER_ERR.Check(w, doc.DataTo(&outputList[i]), "could not parse result: "+doc.Ref.ID) {
			return
		}
		outputList[i].Key = doc.Ref.ID
	}
	w.Header().Set("Content-Type", "application/json")

	// TODO: experimental caching to make repeated scrolls through historical data by the same viewers cheaper.
	//  Lengths/triggers can be tweaked.
	// if older than a week -> cache for a day
	// if older than 3 hours -> cache for an hour
	// if newer than 30 seconds -> no cache
	// otherwise -> cache for 30 seconds
	if len(outputList) > 0 &&
		outputList[0].Created.Add(time.Hour * 24 * 7).Before(time.Now()) &&
		outputList[len(outputList)-1].Created.Add(time.Hour * 24 * 7).Before(time.Now()) {
		w.Header().Set("Cache-Control", "max-age=86400") // 1 day
	} else if len(outputList) > 0 &&
		outputList[0].Created.Add(time.Hour * 3).Before(time.Now()) &&
		outputList[len(outputList)-1].Created.Add(time.Hour * 3).Before(time.Now()) {
		w.Header().Set("Cache-Control", "max-age=3600") // 1 hour
	} else if len(outputList) > 0 &&
		outputList[0].Created.Add(time.Second * 30).After(time.Now()) &&
		outputList[len(outputList)-1].Created.Add(time.Second * 30).After(time.Now()) {
		w.Header().Set("Cache-Control", "no-cache") // no cache
	} else {
		w.Header().Set("Cache-Control", "max-age=30") // half a minute
	}

	w.WriteHeader(int(SERVER_OK))
	enc := json.NewEncoder(w)
	if err := enc.Encode(outputList); err != nil {
		log.Printf("failed to encode query response to JSON: ")
	}
}
