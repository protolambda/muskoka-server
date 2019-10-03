# listing

API for querying tasks and the corresponding results.

**Query params** (URL params):
- `after=<key>`: return results after the given key. For pagination.
- `before=<key>`: return results before the given key. For pagination.
- `limit=<int>`: maximum number of results to return. Will be `min(user_limit, hard_limit)` in practice.
- `order=<order>`: sorting order. Options: `created-asc`, `created-desc` (default)
- `spec-version=<string>`: spec version to filter for
- `has-fail=<bool>`: to only list results that had a non-success result.
- `with-files=<bool>`: to add file urls to the resulting tasks.
- `client-<client-name>=<client-version | all>`: only show tasks with results for the given client, and only the specified version.
   Repeat the parameter to query for multiple clients or versions. 'all' can be used as a catch-all for versions.

**Result**: a JSON encoded list of elements, format:

```
{
  "blocks": int,
  "spec-version": string,
  "created": time,
  "key": string, // to retrieve storage data with 
  "results: {   // may not exist or be empty.
    <unique result key>: {
       "success": bool,
       "created": time,
       "client-name": string,
       "client-version": string,
       "post-hash": string,
       "files": {
           "post-state": string, // URL to file
           "err-log": string,  // URL to file
           "out-log": string,  // URL to file
        }
    },
    ... more results
  }
}
```

Storage path format for inputs: `https://storage.googleapis.com/<bucket>/<spec-version>/<key>/{pre.ssz, block_%d.ssz}`

Output files are linked in the results `"files"` data.
