module github.com/protolambda/muskoka-server

go 1.11

require (
	cloud.google.com/go/pubsub v1.0.1
	github.com/google/go-cmp v0.3.1 // indirect
	github.com/gorilla/mux v1.7.3
	github.com/protolambda/muskoka-server/get_task v0.0.0
	github.com/protolambda/muskoka-server/listing v0.0.0
	github.com/protolambda/muskoka-server/results v0.0.0
	github.com/protolambda/muskoka-server/upload v0.0.0
	go.opencensus.io v0.22.1 // indirect
	golang.org/x/exp v0.0.0-20190912063710-ac5d2bfcbfe0 // indirect
	golang.org/x/net v0.0.0-20190916140828-c8589233b77d // indirect
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e // indirect
	golang.org/x/sys v0.0.0-20190916141854-1a3b71a79e4a // indirect
	golang.org/x/tools v0.0.0-20190916130336-e45ffcd953cc // indirect
	google.golang.org/appengine v1.6.2 // indirect
)

replace github.com/protolambda/muskoka-server/listing => ./listing

replace github.com/protolambda/muskoka-server/results => ./results

replace github.com/protolambda/muskoka-server/upload => ./upload

replace github.com/protolambda/muskoka-server/get_task => ./get_task
