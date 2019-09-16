package results

import (
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"context"
	"encoding/json"
	"fmt"
	gcreds "golang.org/x/oauth2/google"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"time"
)

var inputsBucket *storage.BucketHandle
var fsResultCollection *firestore.CollectionRef
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
	}

	{
		defaultCreds, err := gcreds.FindDefaultCredentials(ctx, storage.ScopeFullControl)
		if err != nil {
			log.Fatalf("failed to load default credentials with full storage scope")
		}
		conf, err := gcreds.JWTConfigFromJSON(defaultCreds.JSON, storage.ScopeFullControl)
		if err != nil {
			log.Fatalf("failed to parse default credentials")
		}
		createSignedStoragePutUrl = func(name string) (string, error) {
			return storage.SignedURL("transition_results", name, &storage.SignedURLOptions{
				Scheme:         storage.SigningSchemeV4,
				Method:         "PUT",
				GoogleAccessID: conf.Email,
				PrivateKey:     conf.PrivateKey,
				Expires:        time.Now().Add(15 * time.Minute),
			})
		}

	}
	// storage
	{
		storageClient, err := storage.NewClient(ctx)
		if err != nil {
			log.Fatalf("Failed to create storage client: %v", err)
		}

		inputsBucket = storageClient.Bucket("transition_outputs")
	}
}

type status int

const (
	SERVER_OK        status = 200
	SERVER_ERR       status = 500
	SERVER_BAD_INPUT status = 400
)

func (s status) report(w http.ResponseWriter, msg string) {
	w.WriteHeader(int(s))
	log.Println(msg)
	_, _ = fmt.Fprintln(w, msg)
}

func (s status) check(w http.ResponseWriter, err error, msg string) bool {
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

type ResultEntry struct {
	TaskKey       string    `firestore:"task-key"`
	Success       bool      `firestore:"success"`
	Created       time.Time `firestore:"created"`
	ClientVersion string    `firestore:"client-version"`
	StateRoot     string    `firestore:"state-root"`
}

type ResultMsg struct {
	// if the transition was successful (i.e. no err log)
	Success bool `json:"success"`
	// the hash-tree-root of the post-state
	StateRoot string `json:"state-root"`
	// identifies the client software running the transition
	ClientVersion string `json:"client-version"`
	// identifies the transition task
	Key string `json:"key"`
}

type ResponseMsg struct {
	PostStateURL string `json:"post-state"`
	ErrLogURL string `json:"err-log"`
	OutLogURL string `json:"out-log"`
}

var rootRegex, _ = regexp.Compile("0x[0-9a-f]{64}")

func Results(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	var result ResultMsg
	if SERVER_BAD_INPUT.check(w, dec.Decode(&result), "could not decode result input") {
		return
	}

	if !rootRegex.Match([]byte(result.StateRoot)) {
		SERVER_BAD_INPUT.report(w, "state root has invalid format")
		return
	}

	if len(result.ClientVersion) > 255 {
		SERVER_BAD_INPUT.report(w, "client version is too long")
		return
	}

	// TODO check if key exists (do not create results for tasks that do not exist)

	// TODO check client auth

	doc := fsResultCollection.NewDoc()

	// store task in firestore
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		resultEntry := &ResultEntry{
			TaskKey:       result.Key,
			Success:       result.Success,
			Created:       time.Now(),
			ClientVersion: result.ClientVersion,
			StateRoot:     result.StateRoot,
		}
		_, err := doc.Set(ctx, resultEntry)

		if SERVER_ERR.check(w, err, "failed to register result.") {
			return
		}
	}

	resp := new(ResponseMsg)

	// create signed urls to upload results to
	{
		var err error
		resp.PostStateURL, err = createSignedStoragePutUrl(fmt.Sprintf("%s~%s/post.ssz", result.Key, doc.ID))
		if SERVER_ERR.check(w, err, "could not create signed post state url") {
			return
		}
		resp.ErrLogURL, err = createSignedStoragePutUrl(fmt.Sprintf("%s~%s/err_log.txt", result.Key, doc.ID))
		if SERVER_ERR.check(w, err, "could not create signed post state url") {
			return
		}
		resp.OutLogURL, err = createSignedStoragePutUrl(fmt.Sprintf("%s~%s/out_log.txt", result.Key, doc.ID))
		if SERVER_ERR.check(w, err, "could not create signed post state url") {
			return
		}
	}

	keyStr := doc.ID
	w.WriteHeader(int(SERVER_OK))
	enc := json.NewEncoder(w)
	if err := enc.Encode(resp); err != nil {
		log.Printf("could not encode response for task %s, result %s: %v", result.Key, keyStr, err)
	}
}

func copyUploadToBucket(u *multipart.FileHeader, key string) error {
	ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
	bucketW := inputsBucket.Object(key).NewWriter(ctx)
	defer bucketW.Close()
	f, err := u.Open()
	defer f.Close()
	if _, err = io.Copy(bucketW, f); err != nil {
		return fmt.Errorf("could not store uploaded data %s: %v"+key, err)
	}
	return nil
}
