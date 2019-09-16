package upload

import (
	"cloud.google.com/go/datastore"
	"cloud.google.com/go/storage"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"
)

var inputsBucket *storage.BucketHandle
var datastoreClient *datastore.Client
const dsTaskKind = "task"

func init() {
	ctx := context.Background()

	// Creates a client.
	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create storage client: %v", err)
	}

	datastoreClient, err = datastore.NewClient(ctx, datastore.DetectProjectID)
	if err != nil {
		log.Fatalf("Failed to create datastore client: %v", err)
	}

	// Sets the name for the new bucket.
	bucketName := "pre-states"

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
	log.Println(msg)
	_, _ = fmt.Fprintln(w, msg)
	w.WriteHeader(int(s))
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
	Blocks int
	SpecVersion string
	Created time.Time
}

func Upload(w http.ResponseWriter, r *http.Request) {
	specVersion := r.Header.Get("spec-version")
	if specVersion == "" {
		BAD.report(w, "spec version is not specified. Set the \"spec-version\" header.")
		return
	}
	if len(specVersion) > 10 {
		BAD.report(w, "spec version is too long")
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
	} else 	if len(pre) != 1 {
		BAD.report(w, "need exactly one pre-state file")
		return
	}

	blocks := r.MultipartForm.File["blocks"]

	ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
	task := &Task{
		Blocks:      len(blocks),
		SpecVersion: specVersion,
		Created:     time.Now(),
	}
	key, err := datastoreClient.Put(ctx, datastore.IncompleteKey(dsTaskKind, nil), task)
	if BAD.check(w, err, "could not register task entry") {
		return
	}
	keyStr := key.Encode()
	_, err = fmt.Fprintf(w, "key: %s", keyStr)

	// parse and store header
	preUpload := r.MultipartForm.File["pre"][0]
	log.Printf("%s pre upload header: %v", keyStr, preUpload.Header)
	if BAD.check(w, copyUploadToBucket(preUpload, keyStr+"/pre.ssz"), "could not handle uploaded state") {
		return
	}
	// parse and store blocks
	for i, b := range blocks {
		log.Printf("%s block %d upload header: %v", keyStr, i, b.Header)
		if BAD.check(w, copyUploadToBucket(b, keyStr+fmt.Sprintf("/block_%d.ssz", i)), fmt.Sprintf("could not handle uploaded block %d", i)) {
			return
		}
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
