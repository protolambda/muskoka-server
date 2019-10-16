package listing

import (
	"cloud.google.com/go/firestore"
	"context"
	"encoding/json"
	"fmt"
	. "github.com/protolambda/httphelpers/codes"
	"google.golang.org/api/iterator"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var firestoreClient *firestore.Client
var fsTransitionsCollection *firestore.CollectionRef
var fsTaskIndexRef *firestore.DocumentRef

var defaultResultsCount = 10
var maxResultsCount = 20

func init() {
	projectID := os.Getenv("GCP_PROJECT")
	ctx := context.Background()

	// database
	{
		cl, err := firestore.NewClient(ctx, projectID)
		if err != nil {
			log.Fatalf("Failed to create firestore client: %v", err)
		}
		firestoreClient = cl
		fsTransitionsCollection = cl.Collection("transitions")
		fsTaskIndexRef = cl.Collection("transitions-meta").Doc("next-index")
	}
}

type Task struct {
	Index       int                    `firestore:"index" json:"index"`
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

type ListingResult struct {
	Tasks    []Task `json:"tasks"`
	MaxIndex int    `json:"max-index"`
}

// versions are not used as keys in firestore, and may contain dots.
var VersionRegex, _ = regexp.Compile("^[0-9a-zA-Z][-_.0-9a-zA-Z]{0,128}$")

// make sure client name keys don't start with `__`, or underscores at all, or hyphens
var ClientNameRegex, _ = regexp.Compile("^[0-9a-zA-Z][-_0-9a-zA-Z]{0,128}$")

func Listing(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := fsTransitionsCollection.Query
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

	// latest-first
	q = q.OrderBy("index", firestore.Desc)

	if p, ok := params["has-fail"]; ok && len(p) > 0 && p[0] == "true" {
		q = q.Where("has-fail", "==", true)
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
	// paginate forwards by continuing *after* (i.e. excl) a given index
	if p, ok := params["after"]; ok && len(p) > 0 {
		afterIndex, err := strconv.ParseUint(p[0], 10, 64)
		if SERVER_BAD_INPUT.Check(w, err, "invalid after-index") {
			return
		}
		q = q.StartAfter(int(afterIndex))
	}
	// paginate backwards by stopping *before* (i.e. excl) a given index
	if p, ok := params["before"]; ok && len(p) > 0 {
		beforeIndex, err := strconv.ParseUint(p[0], 10, 64)
		if SERVER_BAD_INPUT.Check(w, err, "invalid before-index") {
			return
		}
		q = q.EndBefore(beforeIndex)
	}
	// do not select "workers" or "workers-versioned" helper fields.
	q = q.Select("blocks", "spec-version", "spec-config", "created", "results", "index")

	var maxIndex int
	outputList := make([]Task, 0)
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		err := firestoreClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
			// read the next index
			indexDoc, err := tx.Get(fsTaskIndexRef)
			if err != nil {
				return err
			}
			if indexDoc.Exists() {
				if err := indexDoc.DataTo(&maxIndex); err != nil {
					return err
				}
			} else {
				maxIndex = 0
			}
			// no need to query if there are no documents.
			if maxIndex != 0 {
				docsIter := tx.Documents(q)
				for {
					doc, err := docsIter.Next()
					if err == iterator.Done {
						break
					}
					if err != nil {
						return err
					}
					i := len(outputList)
					outputList = append(outputList, Task{})
					d := doc.Data()
					log.Printf("data: %v\n", d)
					if err := doc.DataTo(&outputList[i]); err != nil {
						return fmt.Errorf("could not parse result %s %v", doc.Ref.ID, err)
					}
					outputList[i].Key = doc.Ref.ID
				}
			}
			return nil
		})
		if SERVER_ERR.Check(w, err, "could not process listing query") {
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")

	// Experimental caching to make repeated scrolls through historical data by the same viewers cheaper.
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
	res := ListingResult{
		Tasks:    outputList,
		MaxIndex: maxIndex,
	}
	if err := enc.Encode(&res); err != nil {
		log.Printf("failed to encode query response to JSON: ")
	}
}

func checkHasBefore(q firestore.Query, index int) (bool, error) {
	return checkContainsOne(q.EndBefore(index))
}

func checkHasAfter(q firestore.Query, index int) (bool, error) {
	return checkContainsOne(q.StartAfter(index))
}

func checkContainsOne(q firestore.Query) (bool, error) {
	// only care about document existence, not the contents
	q = q.Select()
	// 1 item is enough
	q = q.Limit(1)
	ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
	docsIter := q.Documents(ctx)
	_, err := docsIter.Next()
	if err == iterator.Done {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
