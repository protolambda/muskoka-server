# results

Cloud func that receives results from a pubsub topic.

Results are received as a JSON object:
 - `index:int` (for pagination purposes)
 - `success:bool`
 - `post-hash:string`
 - `state-root:hex-string`
 - `client-name:string`
 - `client-version:string`
 - `key:string` (of the task)
 - `files:map`
    - `post-state:string` (URL to file)
    - `err-log:string` (URL to file)
    - `out-log:string` (URL to file)
 
The environment var `MUSKOKA_CLIENT_NAME` must match the `client-name` to be accepted.
In the future multiple `client-name` inputs may be accepted with other settings.

There results are put into firestore:
  - Same data as JSON input, excl repeat of the task key, the result is merged in as nested data.
  - Result data is merged into `results` value of the targeted task in the `transitions` collection.
    Key: `<task key>.results.<result key>`. Data: `{success: bool, created: time, client-name: string, client-version: string, post-hash: string, files: map}`
  - Worker is registered to have produced a result, by merging in the following keys into the task:
      - `<taks key>.workers.<worker client name>` is set to `true`.
      - `<task key>.workers-versioned.<worker client name>` is set to `<worker client version>`
      - `<task key>.has-fail` is set to `true` if the result was not a success
