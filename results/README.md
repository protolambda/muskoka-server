# results

Cloud func that:

- accepts results:
  - a JSON results object (`key:string`, `state-root:hex-string`, `client-version:string`).
    Put results into firestore, in kind `transition_result`
  - responds with a signed storage PUT url for `muskoka-transitions` bucket entry `<spec-version>/<key>/results/<client-version>/<result-key>/{post.ssz, out_log.txt, err_log.txt}` to upload result to.

