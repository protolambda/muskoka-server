# upload

Cloud func that:

- accepts a multi-part http upload
    - set form value: `spec-verion`
    - set form `pre` to a file
    - set form `blocks` to a list of files
    - optional: set form `blocks-order` to a list of indices. These must be `len(blocks)` and unique.
      Re-maps upload order (block `i` will be sourced from upload `blocks[blocksorder[i]]`).
      Client-side can't modify `blocks` order because of security restrictions in the browser.
 - creates a firestore entry with unique ID, in collection `transitions`
 - uploads input data to `muskoka-transitions` bucket (`<spec-version>/<key>/{pre.ssz, block_%d.ssz}`)
 - emits JSON event to pus-sub with `spec-version:string`, `key:string`, `blocks:int`
 