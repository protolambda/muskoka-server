# muskoka-server

API to create test tasks and browse results.

The packages are intended to be ran as google-cloud-functions with Go-111 runtime.

A local server with routes for the function endpoints is included for debugging.

`gcloud auth login` and `export GCP_PROJECT=my_project_id` to run. 

Cloud func setup:
- Activate Cloud Functions API

Store Setup:
- Activate Storage API
- Add `transition_inputs` bucket.
- Add role to bucket > user: `allUsers`, permission: `store/Storage Object Viewer`

Firestore setup:
- Activate Datastore > Native (Firestore) API
