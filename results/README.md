# results

Cloud func that:

- accepts results:
  - a JSON results object (`key:string`, `state-root:hex-string`, `client-version:string`).
    Put results into firestore:
      - Result data is merged into `results` value of the targeted task in the `transitions` collection.
        Key: `<task key>.results.<result key>`. Data: `{success: bool, created: time, client-vendor: string, client-version: string, post-hash: string}`
      - Worker is registered to have produced a result, by merging in the following keys into the task:
          - `<taks key>.workers.<worker client vendor>` is set to `true`.
          - `<task key>.workers-versioned.<worker client vendor>` is set to `<worker client version>`
  - responds with a signed storage PUT url for `muskoka-transitions` bucket entry `<spec-version>/<key>/results/<client-vendor>/<client-version>/<result-key>/{post.ssz, out_log.txt, err_log.txt}` to upload result to.

