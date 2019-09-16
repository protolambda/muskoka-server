# upload

Cloud func that:

- accepts a multi-part http upload
    - set form value: `spec-verion`
    - set form `pre` to a file
    - set form `blocks` to a list of files
 - creates a datastore entry with unique ID, in kind `transition_task`
 - uploads input data to `transition_inputs` bucket (`<spec-version>/<key>/{pre.ssz, block_%d.ssz}`)
 - emits JSON event to pus-sub with `spec-version:string`, `key:string`, `blocks:int`
 