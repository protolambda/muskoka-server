# muskoka-server

API to create test tasks and browse results.

The packages are intended to be ran as google-cloud-functions with Go-111 runtime.

A local server with routes for the function endpoints is included for debugging.

To add to your environment variables:
- `GCP_PROJECT=muskoka`: set the project ID
- `GOOGLE_APPLICATION_CREDENTIALS=muskoka-testing.key.json`: path to a service key for testing (`.key.json` is git-ignored).
    Required permissions: Pub/Sub publisher, datastore object admin (firestore uses same permissions), storage object admin.
- `TRANSITIONS_BUCKET` to use a custom storage bucket.

APIs to activate:
- IAM             -- permissions, there by default
- Cloud functions -- to handle API requests and process results 
- Storage         -- to store inputs and outputs of all transitions
- Pub/Sub         -- to communicate new tasks and results as events
- Firestore       -- to track tasks and results

Note: deployments are to europe-west 3 and 2 regions, to keep latency between services low. 

```bash
# Set project ID
export GCP_PROJECT=muskoka
gcloud config set project $GCP_PROJECT

# Login (if not already authenticated)
gcloud auth


# Storage
# ==========================================

# Decide on an inputs bucket
export TRANSITIONS_BUCKET=muskoka-transitions

# Make-Bucket for transition inputs
gsutil mb -l europe-west3 gs://$TRANSITIONS_BUCKET/

# Make transition inputs publicly readable
gsutil iam ch allUsers:objectViewer gs://$TRANSITIONS_BUCKET

# Decide on an outputs bucket for a team
export TEAM_BUCKET=muskoka_eth2team

# Make-Bucket for team storage
gsutil mb -l europe-west3 gs://$TEAM_BUCKET/

# Make transition outputs of the team publicly readable
gsutil iam ch allUsers:objectViewer gs://$TEAM_BUCKET


# Pub/Sub
# ==========================================

export SPEC_VERSION=v0.8.3
export SPEC_CONFIG=minimal

# Create an input topic
gcloud pubsub topics create transition~$SPEC_VERSION~$SPEC_CONFIG

export CLIENT_NAME=eth2team
export WORKER_ID=worker1

# Create a subscription for a team worker node (this creates a PULL subscription, with a 100 second ACK time, and 20 min message retention time)
gcloud pubsub subscriptions create $SPEC_VERSION~$SPEC_CONFIG~$CLIENT_NAME~$WORKER_ID --ack-deadline=100 --message-retention-duration=1200 --topic transition~$SPEC_VERSION~$SPEC_CONFIG

# Create an output topic for each team
gcloud pubsub topics create results~$CLIENT_NAME


# Firestore
# ==========================================

# Collections and documented are automatically created, no setup requirements here


# Cloud functions
# ==========================================

# Collect results for each client team in a separate Go cloud func for independent and isolated permission/upgrade management.
(cd results && gcloud functions deploy results --region=europe-west2 --entry-point=Results --memory=128M --runtime=go111 --trigger-topic results~$CLIENT_NAME --set-env-vars MUSKOKA_CLIENT_NAME=$CLIENT_NAME)

# Process transition uploads
(cd upload && gcloud functions deploy upload --region=europe-west2 --entry-point=Upload --memory=128M --runtime=go111 --trigger-http --allow-unauthenticated)

# Serve Task retrievals
(cd get_task && gcloud functions deploy task --region=europe-west2 --entry-point=GetTask --memory=128M --runtime=go111 --trigger-http --allow-unauthenticated)

# Serve Task searches
(cd listing && gcloud functions deploy listing --region=europe-west2 --entry-point=Listing --memory=128M --runtime=go111 --trigger-http --allow-unauthenticated)


# IAM
# ==========================================

export CLIENT_NAME=eth2team

export CLIENT_SERV_ACC=client-$CLIENT_NAME-serv1

# Create service account for the team, shared between their working nodes (or it can be per worker if preferred):
gcloud beta iam service-accounts create $CLIENT_SERV_ACC \
    --description "Client muskoka account for Eth 2.0 client $CLIENT_NAME" \
    --display-name "Client $CLIENT_NAME"

# Create a key-file for the service account
gcloud iam service-accounts keys create service_account_$CLIENT_SERV_ACC.key.json \
  --iam-account $CLIENT_SERV_ACC@$GCP_PROJECT.iam.gserviceaccount.com

# Allow the service account to write to the team storage
# gsutil iam ch [MEMBER_TYPE]:[MEMBER_NAME]:[IAM_ROLE] gs://[BUCKET_NAME]
gsutil iam ch serviceAccount:$CLIENT_SERV_ACC@$GCP_PROJECT.iam.gserviceaccount.com:roles/storage.objectCreator gs://$TEAM_BUCKET


# Pubsub and function access permissions are best managed through the google cloud web console

# Pubsub: 
    Fore each team:
    - select inputs subscription -> Permissions -> Add member -> service account name, add roles: Pub/Sub Viewer, Pub/Sub Subscriber
    - select outputs topic -> Permissions -> Add member -> service account name, add roles: Pub/Sub Viewer, Pub/Sub Publisher

# Functions: select function -> Permissions -> Add member -> service account name
```

