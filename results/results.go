package results

import (
	"bytes"
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	. "github.com/protolambda/muskoka-server/common"
	//"google.golang.org/api/iam/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"os"
	"time"
)

const storageAPI = "https://storage.googleapis.com"

// default: every client is denied.
var CheckClient = func(name string) bool {
	return false
}

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

	{
		if envName := os.Getenv("ETH2_CLIENT_NAME"); envName != "" {
			// default: every client is denied.
			CheckClient = func(name string) bool {
				return name == envName
			}
		}
	}
}

type Task struct {
	Blocks           int                    `firestore:"blocks"`
	SpecVersion      string                 `firestore:"spec-version"`
	SpecConfig       string                 `firestore:"spec-config"`
	Created          time.Time              `firestore:"created"`
	Results          map[string]ResultEntry `firestore:"results"`
	WorkersVersioned map[string]string      `firestore:"workers-versioned"`
	Workers          map[string]bool        `firestore:"workers"`
	HasFail          bool                   `firestore:"has-fail"`
}

type ResultEntry struct {
	Success       bool      `firestore:"success"`
	Created       time.Time `firestore:"created"`
	ClientName    string    `firestore:"client-name"`
	ClientVersion string    `firestore:"client-version"`
	PostHash      string    `firestore:"post-hash"`
	Files         ResultFilesRef `firestore:"files"`
}

type ResultFilesRef struct {
	PostStateURL string `firestore:"post-state-url"`
	ErrLogURL    string `firestore:"err-log-url"`
	OutLogURL    string `firestore:"out-log-url"`
}

type ResultMsg struct {
	// if the transition was successful (i.e. no err log)
	Success bool `json:"success"`
	// the flat-hash of the post-state SSZ bytes, for quickly finding different results.
	PostHash string `json:"post-hash"`
	// the name of the client; 'zrnt', 'lighthouse', etc.
	ClientName string `json:"client-name"`
	// the version number of the client, may contain a git commit hash
	ClientVersion string `json:"client-version"`
	// identifies the transition task
	Key string `json:"key"`
	// Result files
	Files ResultFilesData `json:"files"`
}

type ResultFilesData struct {
	// bucket
	Bucket string `json:"bucket"`
	// object path within bucket
	PostState string `json:"post-state"`
	ErrLog    string `json:"err-log"`
	OutLog    string `json:"out-log"`
}

// PubSubMessage is the payload of a Pub/Sub event.
type PubSubMessage struct {
	Data []byte
}

// Client auth is checked by configuring the cloud function
// to only consume messages from a topic specific to the client.
// And setting the ETH2_CLIENT_NAME environment var.
func Results(ctx context.Context, m *pubsub.Message) error {
	dec := json.NewDecoder(bytes.NewReader(m.Data))
	var result ResultMsg
	if err := dec.Decode(&result); err != nil {
		return fmt.Errorf("could not decode result input: %v", err)
	}
	if !RootRegex.Match([]byte(result.PostHash)) {
		return errors.New("post hash has invalid format")
	}

	if !VersionRegex.Match([]byte(result.ClientVersion)) {
		return errors.New("client version is invalid")
	}
	if !CheckClient(result.ClientName) {
		return errors.New("client name is invalid")
	}
	if !KeyRegex.Match([]byte(result.Key)) {
		return errors.New("task key is invalid")
	}

	// checks if the task key exists, and retrieves the task information
	var task Task
	{
		ctx, _ := context.WithTimeout(ctx, time.Second*5)
		taskDoc, err := fsTransitionsCollection.Doc(result.Key).Get(ctx)
		if status.Code(err) == codes.NotFound || (err == nil && !taskDoc.Exists()) {
			return errors.New("task does not exist, cannot process result")
		}
		if err != nil {
			return fmt.Errorf("failed to lookup task: %v", err)
		}
		if err := taskDoc.DataTo(&task); err != nil {
			return fmt.Errorf("failed to parse task: %v", err)
		}
	}

	keyStr := uniqueID()
	{
		ctx, _ := context.WithTimeout(ctx, time.Second*5)
		mergeData := map[string]interface{}{
			"results": map[string]ResultEntry{
				keyStr: {
					Success:       result.Success,
					Created:       time.Now(),
					ClientName:    result.ClientName,
					ClientVersion: result.ClientVersion,
					PostHash:      result.PostHash,
					Files: ResultFilesRef{
						PostStateURL: fmt.Sprintf("%s/%s/%s", storageAPI, result.Files.Bucket, result.Files.PostState),
						OutLogURL: fmt.Sprintf("%s/%s/%s", storageAPI, result.Files.Bucket, result.Files.OutLog),
						ErrLogURL: fmt.Sprintf("%s/%s/%s", storageAPI, result.Files.Bucket, result.Files.ErrLog),
					},
				},
			},
			"workers-versioned": map[string]string{
				result.ClientName: result.ClientVersion,
			},
			"workers": map[string]bool{
				result.ClientName: true,
			},
		}
		if !result.Success {
			mergeData["has-fail"] = true
		}
		if _, err := fsTransitionsCollection.Doc(result.Key).Set(ctx, mergeData, firestore.MergeAll); err != nil {
			return fmt.Errorf("failed to register result: %v", err)
		}
	}
	return nil
}

func uniqueID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read error: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
