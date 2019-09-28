package results

import (
	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	. "github.com/protolambda/muskoka-server/common"
	gcreds "golang.org/x/oauth2/google"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"net/http"
	"os"
	"time"
)

var fsTransitionsCollection *firestore.CollectionRef
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
		fsTransitionsCollection = firestoreClient.Collection("transitions")
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
		bucketName := "muskoka-transitions"
		if envName := os.Getenv("TRANSITIONS_BUCKET"); envName != "" {
			bucketName = envName
		}
		createSignedStoragePutUrl = func(name string) (string, error) {
			return storage.SignedURL(bucketName, name, &storage.SignedURLOptions{
				Scheme:         storage.SigningSchemeV4,
				Method:         "PUT",
				GoogleAccessID: conf.Email,
				PrivateKey:     conf.PrivateKey,
				Expires:        time.Now().Add(15 * time.Minute),
			})
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
}

type ResultEntry struct {
	Success       bool      `firestore:"success"`
	Created       time.Time `firestore:"created"`
	ClientName    string    `firestore:"client-name"`
	ClientVersion string    `firestore:"client-version"`
	PostHash      string    `firestore:"post-hash"`
}

type ResultMsg struct {
	// if the transition was successful (i.e. no err log)
	Success bool `json:"success"`
	// the flat-hash of the post-state SSZ bytes, for quickly finding different results.
	PostHash string `json:"post-hash"`
	// the client-name name of the client; 'zrnt', 'lighthouse', etc.
	ClientName string `json:"client-name"`
	// the version number of the client, may contain a git commit hash
	ClientVersion string `json:"client-version"`
	// identifies the transition task
	Key string `json:"key"`
}

type ResultResponseMsg struct {
	PostStateURL string `json:"post-state"`
	ErrLogURL    string `json:"err-log"`
	OutLogURL    string `json:"out-log"`
}

func Results(w http.ResponseWriter, r *http.Request) {
	// TODO check client auth

	dec := json.NewDecoder(r.Body)
	var result ResultMsg
	if SERVER_BAD_INPUT.Check(w, dec.Decode(&result), "could not decode result input") {
		return
	}

	if !RootRegex.Match([]byte(result.PostHash)) {
		SERVER_BAD_INPUT.Report(w, "post hash has invalid format")
		return
	}

	if !VersionRegex.Match([]byte(result.ClientVersion)) {
		SERVER_BAD_INPUT.Report(w, "client version is invalid")
		return
	}
	if !ClientNameRegex.Match([]byte(result.ClientName)) {
		SERVER_BAD_INPUT.Report(w, "client name is invalid")
		return
	}
	if !KeyRegex.Match([]byte(result.Key)) {
		SERVER_BAD_INPUT.Report(w, "task key is invalid")
		return
	}

	// checks if the task key exists, and retrieves the task information
	var task Task
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		taskDoc, err := fsTransitionsCollection.Doc(result.Key).Get(ctx)
		if status.Code(err) == codes.NotFound || (err == nil && !taskDoc.Exists()) {
			SERVER_BAD_INPUT.Report(w, "task does not exist, cannot process result")
			return
		}
		if SERVER_ERR.Check(w, err, "failed to lookup task") {
			return
		}
		if SERVER_ERR.Check(w, taskDoc.DataTo(&task), "failed to parse task") {
			return
		}
	}

	keyStr := uniqueID()
	{
		ctx, _ := context.WithTimeout(context.Background(), time.Second*5)
		_, err := fsTransitionsCollection.Doc(result.Key).Set(ctx, map[string]interface{}{
			"results": map[string]ResultEntry{
				keyStr: {
					Success:       result.Success,
					Created:       time.Now(),
					ClientName:    result.ClientName,
					ClientVersion: result.ClientVersion,
					PostHash:      result.PostHash,
				},
			},
			"workers-versioned": map[string]string{
				result.ClientName: result.ClientVersion,
			},
			"workers": map[string]bool{
				result.ClientName: true,
			},
		}, firestore.MergeAll)

		if SERVER_ERR.Check(w, err, "failed to register result.") {
			return
		}
	}

	respMsg := new(ResultResponseMsg)

	// create signed urls to upload results to
	{
		path := fmt.Sprintf("%s/%s/results/%s/%s/%s", task.SpecVersion, result.Key, result.ClientName, result.ClientVersion, keyStr)
		var err error
		respMsg.PostStateURL, err = createSignedStoragePutUrl(path + "/post.ssz")
		if SERVER_ERR.Check(w, err, "could not create signed post state url") {
			return
		}
		respMsg.ErrLogURL, err = createSignedStoragePutUrl(path + "/err_log.txt")
		if SERVER_ERR.Check(w, err, "could not create signed post state url") {
			return
		}
		respMsg.OutLogURL, err = createSignedStoragePutUrl(path + "/out_log.txt")
		if SERVER_ERR.Check(w, err, "could not create signed post state url") {
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(int(SERVER_OK))
	enc := json.NewEncoder(w)
	if err := enc.Encode(respMsg); err != nil {
		log.Printf("could not encode response for task %s, result %s: %v", result.Key, keyStr, err)
	}
}

func uniqueID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read error: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
