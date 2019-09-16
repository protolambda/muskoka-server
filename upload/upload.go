package upload

import (
	"bytes"
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"time"
)

var inputsBucket *storage.BucketHandle
var firestoreClient *firestore.Client
var transitionTopic *pubsub.Topic
var fsTaskCollection *firestore.CollectionRef

func init() {
	projectID := os.Getenv("GCP_PROJECT")
	ctx := context.Background()

	// Creates a client.
	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create storage client: %v", err)
	}

	firestoreClient, err = firestore.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create firestore client: %v", err)
	}
	fsTaskCollection = firestoreClient.Collection("transition_task")

	pubsubClient, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("Failed to create pubsub client: %v", err)
	}
	transitionTopic = pubsubClient.Topic("transition")

	// Sets the name for the new bucket.
	bucketName := "transition_inputs"

	// Creates a Bucket instance.
	inputsBucket = storageClient.Bucket(bucketName)
}

// 10 MB
const maxUploadMem = 10 * (1 << 20)

type status int

const (
	OK  status = 200
	BAD status = 500
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

type Task struct {
	Blocks      int `firestore:"blocks"`
	SpecVersion string `firestore:"spec-version"`
	Created     time.Time `firestore:"created"`
	Status      string `firestore:"status"`
}

type TransitionMsg struct {
	Blocks      int    `json:"blocks"`
	SpecVersion string `json:"spec-version"`
	Key         string `json:"key"`
}

var versionRegex, _ = regexp.Compile("[a-zA-Z0-9.-]")

func Upload(w http.ResponseWriter, r *http.Request) {
	specVersion := r.FormValue("spec-version")
	if specVersion == "" {
		BAD.report(w, "spec version is not specified. Set the \"spec-version\" form value.")
		return
	}
	if len(specVersion) > 10 {
		BAD.report(w, "spec version is too long")
		return
	}
	if !versionRegex.Match([]byte(specVersion)) {
		BAD.report(w, "spec version is invalid")
		return
	}
	err := r.ParseMultipartForm(maxUploadMem)
	if BAD.check(w, err, "cannot parse multipart upload") {
		return
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			log.Printf("could not clean up mutli-part upload: %v", err)
		}
	}()

	if blocks, ok := r.MultipartForm.File["blocks"]; !ok {
		BAD.report(w, "no blocks were specified")
		return
	} else if len(blocks) > 16 {
		BAD.report(w, fmt.Sprintf("cannot process high amount of blocks; %v", len(blocks)))
	}
	if pre, ok := r.MultipartForm.File["pre"]; !ok {
		BAD.report(w, "no pre-state was specified")
		return
	} else if len(pre) != 1 {
		BAD.report(w, "need exactly one pre-state file")
		return
	}

	blocks := r.MultipartForm.File["blocks"]

	doc := fsTaskCollection.NewDoc()

	// store task in firestore
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		task := &Task{
			Blocks:      len(blocks),
			SpecVersion: specVersion,
			Created:     time.Now(),
			Status:      "started",
		}
		_, err := doc.Set(ctx, task)

		if BAD.check(w, err, "failed to register task.") {
			return
		}
	}

	keyStr := doc.ID
	// TODO proper full json response
	_, err = fmt.Fprintf(w, "key: %s", keyStr)
	w.WriteHeader(200)

	// TODO: could return to response faster by putting remainder in go routine

	failedUpload := false
	// parse and store header
	preUpload := r.MultipartForm.File["pre"][0]
	log.Printf("%s pre upload header: %v", keyStr, preUpload.Header)
	if err := copyUploadToBucket(preUpload, specVersion+"/"+keyStr+"/pre.ssz"); err != nil {
		log.Printf("could not upload pre-state: %v", err)
		failedUpload = false
	}
	if !failedUpload {
		// parse and store blocks
		for i, b := range blocks {
			log.Printf("%s block %d upload header: %v", keyStr, i, b.Header)
			if err := copyUploadToBucket(b, specVersion+"/"+keyStr+fmt.Sprintf("/block_%d.ssz", i)); err != nil {
				log.Printf(fmt.Sprintf("could not handle uploaded block %d: %v", i, err))
				failedUpload = true
				break
			}
		}
	}
	if failedUpload {
		// mark firestore entry as failed
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		task := &Task{
			Status:      "failed",
		}
		_, err := doc.Set(ctx, task, firestore.Merge([]string{"status"}))
		if err != nil {
			log.Printf("upload to bucket failed, and then marking the task as 'failed' failed...")
		}
		return
	}

	// fire pubsub event
	{
		trMsg := &TransitionMsg{
			Blocks:      len(blocks),
			SpecVersion: specVersion,
			Key:         keyStr,
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		if err := enc.Encode(trMsg); err != nil {
			log.Printf("failed to emit event, could not encode task to JSON: %v, err: %v", trMsg, err)
			return
		}
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		<-transitionTopic.Publish(ctx, &pubsub.Message{
			Data: buf.Bytes(),
		}).Ready()
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
