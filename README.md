# muskoka-server

API to create test tasks and browse results.

The packages are intended to be ran as google-cloud-functions with Go-111 runtime.

A local server with routes for the function endpoints is included for debugging.

To add to your environment variables:
- `GCP_PROJECT=muskoka`: set the project ID
- `GOOGLE_APPLICATION_CREDENTIALS=muskoka-testing.key.json`: path to a service key for testing (`.key.json` is git-ignored).
    Required permissions: Pub/Sub publisher, datastore object admin (firestore uses same permissions), storage object admin.
- `TRANSITIONS_BUCKET` to use a custom storage bucket.

Cloud func setup:
- Activate Cloud Functions API

Storage setup:
- Activate Storage API
- Add `muskoka-transitions` bucket (or what you set the `TRANSITIONS_BUCKET` environment var to).
- Add role to bucket > user: `allUsers`, permission: `store/Storage Object Viewer`

Pub/Sub setup:
- Create `transition` topic.
- Create subscriptions to the `transition` topic for worker nodes.

Firestore setup:
- Activate Datastore > Native (Firestore) API
