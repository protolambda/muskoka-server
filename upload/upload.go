package upload

import (
	"bytes"
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
	"context"
	"encoding/json"
	"fmt"
	. "github.com/protolambda/httphelpers/codes"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var inputsBucket *storage.BucketHandle
var pubSubClient *pubsub.Client
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

	// pubsub
	{
		cl, err := pubsub.NewClient(ctx, projectID)
		if err != nil {
			log.Fatalf("Failed to create pubsub client: %v", err)
		}
		pubSubClient = cl
	}

	// storage
	{
		storageClient, err := storage.NewClient(ctx)
		if err != nil {
			log.Fatalf("Failed to create storage client: %v", err)
		}

		bucketName := "muskoka-transitions"
		if envName := os.Getenv("TRANSITIONS_BUCKET"); envName != "" {
			bucketName = envName
		}
		inputsBucket = storageClient.Bucket(bucketName)
	}
}

// 10 MB
const maxUploadMem = 10 * (1 << 20)

type Task struct {
	Blocks      int       `firestore:"blocks"`
	SpecVersion string    `firestore:"spec-version"`
	SpecConfig  string    `firestore:"spec-config"`
	Created     time.Time `firestore:"created"`
	// Results and workers are ignored, only added later when workers make results available
}

type TransitionMsg struct {
	Blocks      int    `json:"blocks"`
	SpecVersion string `json:"spec-version"`
	SpecConfig  string `json:"spec-config"`
	Key         string `json:"key"`
}

type UploadResponse struct {
	Key string `json:"key"`
}

var versionRegex, _ = regexp.Compile("[a-zA-Z0-9.-_]")

var configRegex, _ = regexp.Compile("[a-zA-Z0-9-_]")

func Upload(w http.ResponseWriter, r *http.Request) {
	specVersion := r.FormValue("spec-version")
	if specVersion == "" {
		SERVER_BAD_INPUT.Report(w, "spec version is not specified. Set the \"spec-version\" form value.")
		return
	}
	if len(specVersion) > 10 {
		SERVER_BAD_INPUT.Report(w, "spec version is too long")
		return
	}
	specConfig := r.FormValue("spec-config")
	if specConfig == "" {
		SERVER_BAD_INPUT.Report(w, "spec config is not specified. Set the \"spec-config\" form value.")
		return
	}
	if len(specConfig) > 100 {
		SERVER_BAD_INPUT.Report(w, "spec config name is too long")
		return
	}
	if !versionRegex.Match([]byte(specVersion)) {
		SERVER_BAD_INPUT.Report(w, "spec version is invalid")
		return
	}
	if !configRegex.Match([]byte(specConfig)) {
		SERVER_BAD_INPUT.Report(w, "spec config name is invalid")
		return
	}
	err := r.ParseMultipartForm(maxUploadMem)
	if SERVER_BAD_INPUT.Check(w, err, "cannot parse multipart upload") {
		return
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			log.Printf("could not clean up mutli-part upload: %v", err)
		}
	}()

	if blocks, ok := r.MultipartForm.File["blocks"]; !ok {
		SERVER_BAD_INPUT.Report(w, "no blocks were specified")
		return
	} else if len(blocks) > 16 {
		SERVER_BAD_INPUT.Report(w, fmt.Sprintf("cannot process high amount of blocks; %v", len(blocks)))
	}
	if pre, ok := r.MultipartForm.File["pre"]; !ok {
		SERVER_BAD_INPUT.Report(w, "no pre-state was specified")
		return
	} else if len(pre) != 1 {
		SERVER_BAD_INPUT.Report(w, "need exactly one pre-state file")
		return
	}

	pubSubTopic := pubSubClient.Topic(fmt.Sprintf("transition~%s~%s", specVersion, specConfig))
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		ok, err := pubSubTopic.Exists(ctx)
		if SERVER_ERR.Check(w, err, "could not check if spec version + config is a valid topic") {
			return
		} else if !ok {
			SERVER_BAD_INPUT.Report(w, "Cannot recognize provided spec version + config")
			return
		}
	}

	blocks := r.MultipartForm.File["blocks"]

	if indicesStr := r.FormValue("blocks-order"); indicesStr != "" {
		blockIndices := strings.Split(indicesStr, ",")
		if len(blockIndices) != len(blocks) {
			SERVER_BAD_INPUT.Report(w, "specified blocks order has mismatching index count compared to actual blocks uploaded")
			return
		}
		blocksReordered := make([]*multipart.FileHeader, len(blockIndices), len(blockIndices))
		blocksTaken := make([]bool, len(blockIndices), len(blockIndices))
		for dstIndex := 0; dstIndex < len(blockIndices); dstIndex++ {
			srcIndex, err := strconv.ParseUint(blockIndices[dstIndex], 10, 64)
			if err != nil || srcIndex >= uint64(len(blockIndices)) || blocksTaken[srcIndex] {
				SERVER_BAD_INPUT.Report(w, "specified block indices are not valid unique within-range indices")
				return
			}
			blocksReordered[dstIndex] = blocks[srcIndex]
			// don't re-use blocks. All must be unique.
			blocksTaken[srcIndex] = true
		}
		blocks = blocksReordered
	}

	doc := fsTransitionsCollection.NewDoc()
	keyStr := doc.ID

	// parse and store header
	preUpload := r.MultipartForm.File["pre"][0]
	log.Printf("%s pre upload header: %v", keyStr, preUpload.Header)
	if SERVER_ERR.Check(w, copyUploadToBucket(preUpload, specVersion+"/"+specConfig+"/"+keyStr+"/pre.ssz"),
		"could not store pre-state") {
		return
	}
	// parse and store blocks
	for i, b := range blocks {
		log.Printf("%s block %d upload header: %v", keyStr, i, b.Header)
		if SERVER_ERR.Check(w, copyUploadToBucket(b, specVersion+"/"+specConfig+"/"+keyStr+fmt.Sprintf("/block_%d.ssz", i)),
			fmt.Sprintf("could not store block %d", err)) {
			return
		}
	}

	// store task in firestore
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		task := &Task{
			Blocks:      len(blocks),
			SpecVersion: specVersion,
			SpecConfig:  specConfig,
			Created:     time.Now(),
		}
		_, err := doc.Set(ctx, task)

		if SERVER_ERR.Check(w, err, "failed to register task.") {
			return
		}
	}

	// fire pubsub event
	{
		trMsg := &TransitionMsg{
			Blocks:      len(blocks),
			SpecVersion: specVersion,
			SpecConfig:  specConfig,
			Key:         keyStr,
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		if err := enc.Encode(trMsg); err != nil {
			log.Printf("failed to emit event, could not encode task to JSON: %v, err: %v", trMsg, err)
			return
		}
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		<-pubSubTopic.Publish(ctx, &pubsub.Message{
			Data: buf.Bytes(),
		}).Ready()
	}

	// Success, redirect to result
	http.Redirect(w, r, "/task/"+keyStr, http.StatusSeeOther)
}

func copyUploadToBucket(u *multipart.FileHeader, key string) error {
	ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
	bucketW := inputsBucket.Object(key).NewWriter(ctx)
	f, err := u.Open()
	if err != nil {
		return fmt.Errorf("could not receive uploaded data for %s: %v", key, err)
	}
	if _, err = io.Copy(bucketW, f); err != nil {
		return fmt.Errorf("could not store uploaded data %s: %v", key, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not close uploaded data file for %s: %v", key, err)
	}
	if err := bucketW.Close(); err != nil {
		return fmt.Errorf("could not push uploaded data to cloud bucket for %s: %v", key, err)
	}
	return nil
}
