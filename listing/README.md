# listing

API for querying tasks and the corresponding results.

**Query params** (URL params):
- `after=<key>`: return results after the given key. For pagination.
- `before=<key>`: return results before the given key. For pagination.
- `limit=<int>`: maximum number of results to return. Will be `min(user_limit, hard_limit)` in practice.
- `order=<order>`: sorting order. Options: `created-asc`, `created-desc` (default)
- `spec-version=<string>`: spec version to filter for
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
       "post-hash": string
    },
    ... more results
  }
}
```

Storage result link formats:

- inputs: `<spec-version>/<key>/{pre.ssz, block_%d.ssz}`
- results: `<spec-version>/<key>/results/<client-name>/<client-version>/<result-key>/{post.ssz, out_log.txt, err_log.txt}`

Queried on the storage API endpoint: `https://storage.googleapis.com`
