package listing

import (
	"cloud.google.com/go/firestore"
	"context"
	. "github.com/protolambda/muskoka-server/common"
	"log"
	"net/http"
	"os"
	"strconv"
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
	Blocks      int       `firestore:"blocks"`
	SpecVersion string    `firestore:"spec-version"`
	Created     time.Time `firestore:"created"`
}

type ResultEntry struct {
	Success       bool      `firestore:"success"`
	Created       time.Time `firestore:"created"`
	ClientVendor  string    `firestore:"client-vendor"`
	ClientVersion string    `firestore:"client-version"`
	PostHash      string    `firestore:"post-hash"`
}

func Listing(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	q := fsTransitionsCollection.Query
	if p, ok := params["offset"]; ok && len(p) > 0 {
		offset, err := strconv.ParseUint(p[0], 10, 32)
		if SERVER_BAD_INPUT.Check(w, err, "invalid offset") {
			return
		}
		q = q.Offset(int(offset))
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
	if p, ok := params["spec-version"]; ok && len(p) > 0 {
		q = q.Where("spec-version", "==", p[0])
	}

}