# muskoka-server

API to create test tasks and browse results.

The packages are intended to be ran as google-cloud-functions with Go-111 runtime.

A local server with routes for the function endpoints is included for debugging.

To add to your environment variables:
- `GCP_PROJECT=muskoka`: set the project ID
- `GOOGLE_APPLICATION_CREDENTIALS=muskoka-testing.key.json`: path to a service key for testing (`.key.json` is git-ignored).
    Required permissions: Pub/Sub publisher, datastore object admin (firestore uses same permissions), storage object admin.


Cloud func setup:
- Activate Cloud Functions API

Store Setup:
- Activate Storage API
- Add `transition_inputs` bucket.
- Add role to bucket > user: `allUsers`, permission: `store/Storage Object Viewer`

Firestore setup:
- Activate Datastore > Native (Firestore) API
