package results

import (
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"context"
	"encoding/json"
	"fmt"
	gcreds "golang.org/x/oauth2/google"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"
)

var fsResultCollection, fsTaskCollection *firestore.CollectionRef
var createSignedStoragePutUrl func(name string) (string, error)

func init() {
	projectID := os.Getenv("GCP_PROJECT")
	ctx := context.Background()

	// database
	{
		firestoreClient, err := firestore.NewClient(ctx, projectID)
		if err != nil {
			log.Fatalf("Failed to create firestore client: %v", err)
		}
		fsResultCollection = firestoreClient.Collection("transition_result")
		fsTaskCollection = firestoreClient.Collection("transition_task")
	}

	{
		defaultCreds, err := gcreds.FindDefaultCredentials(ctx, storage.ScopeFullControl)
		if err != nil {
			log.Fatalf("failed to load default credentials with full storage scope")
		}
		conf, err := gcreds.JWTConfigFromJSON(defaultCreds.JSON, storage.ScopeFullControl)
		if err != nil {
			log.Fatalf("failed to parse default credentials: %v", err)
		}
		createSignedStoragePutUrl = func(name string) (string, error) {
			return storage.SignedURL("muskoka-transitions", name, &storage.SignedURLOptions{
				Scheme:         storage.SigningSchemeV4,
				Method:         "PUT",
				GoogleAccessID: conf.Email,
				PrivateKey:     conf.PrivateKey,
				Expires:        time.Now().Add(15 * time.Minute),
			})
		}

	}
}

type statCode int

const (
	SERVER_OK        statCode = 200
	SERVER_ERR       statCode = 500
	SERVER_BAD_INPUT statCode = 400
)

func (s statCode) report(w http.ResponseWriter, msg string) {
	w.WriteHeader(int(s))
	log.Println(msg)
	_, _ = fmt.Fprintln(w, msg)
}

func (s statCode) check(w http.ResponseWriter, err error, msg string) bool {
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

type Task struct {
	Blocks      int       `firestore:"blocks"`
	SpecVersion string    `firestore:"spec-version"`
	Created     time.Time `firestore:"created"`
	Status      string    `firestore:"status"`
}

type ResultEntry struct {
	TaskKey       string    `firestore:"task-key"`
	Success       bool      `firestore:"success"`
	Created       time.Time `firestore:"created"`
	ClientVersion string    `firestore:"client-version"`
	PostHash      string    `firestore:"post-hash"`
}

type ResultMsg struct {
	// if the transition was successful (i.e. no err log)
	Success bool `json:"success"`
	// the flat-hash of the post-state SSZ bytes, for quickly finding different results.
	PostHash string `json:"post-hash"`
	// identifies the client software running the transition
	ClientVersion string `json:"client-version"`
	// identifies the transition task
	Key string `json:"key"`
}

type ResultResponseMsg struct {
	PostStateURL string `json:"post-state"`
	ErrLogURL    string `json:"err-log"`
	OutLogURL    string `json:"out-log"`
}

var rootRegex, _ = regexp.Compile("0x[0-9a-f]{64}")

func Results(w http.ResponseWriter, r *http.Request) {
	// TODO check client auth

	dec := json.NewDecoder(r.Body)
	var result ResultMsg
	if SERVER_BAD_INPUT.check(w, dec.Decode(&result), "could not decode result input") {
		return
	}

	if !rootRegex.Match([]byte(result.PostHash)) {
		SERVER_BAD_INPUT.report(w, "post hash has invalid format")
		return
	}

	if len(result.ClientVersion) > 255 {
		SERVER_BAD_INPUT.report(w, "client version is too long")
		return
	}

	// checks if the task key exists, and retrieves the task information
	var task Task
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		taskDoc, err := fsTaskCollection.Doc(result.Key).Get(ctx)
		if status.Code(err) == codes.NotFound || (err == nil && !taskDoc.Exists()) {
			SERVER_BAD_INPUT.report(w, "task does not exist, cannot process result")
			return
		}
		if SERVER_ERR.check(w, err, "failed to lookup task") {
			return
		}
		if SERVER_ERR.check(w, taskDoc.DataTo(&task), "failed to parse task") {
			return
		}
	}

	resultDoc := fsResultCollection.NewDoc()

	// store task result in firestore
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		resultEntry := &ResultEntry{
			TaskKey:       result.Key,
			Success:       result.Success,
			Created:       time.Now(),
			ClientVersion: result.ClientVersion,
			PostHash:      result.PostHash,
		}
		_, err := resultDoc.Set(ctx, resultEntry)

		if SERVER_ERR.check(w, err, "failed to register result.") {
			return
		}
	}

	respMsg := new(ResultResponseMsg)

	// create signed urls to upload results to
	{
		path := fmt.Sprintf("%s/%s/results/%s/%s", task.SpecVersion, result.Key, result.ClientVersion, resultDoc.ID)
		var err error
		respMsg.PostStateURL, err = createSignedStoragePutUrl(path + "/post.ssz")
		if SERVER_ERR.check(w, err, "could not create signed post state url") {
			return
		}
		respMsg.ErrLogURL, err = createSignedStoragePutUrl(path + "/err_log.txt")
		if SERVER_ERR.check(w, err, "could not create signed post state url") {
			return
		}
		respMsg.OutLogURL, err = createSignedStoragePutUrl(path + "/out_log.txt")
		if SERVER_ERR.check(w, err, "could not create signed post state url") {
			return
		}
	}

	keyStr := resultDoc.ID
	w.WriteHeader(int(SERVER_OK))
	enc := json.NewEncoder(w)
	if err := enc.Encode(respMsg); err != nil {
		log.Printf("could not encode response for task %s, result %s: %v", result.Key, keyStr, err)
	}
}
