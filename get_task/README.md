# task

API for getting a single task.

**Query params** (URL params):
- `key=<key>`: return info for the specified task.

**Result**: a JSON encoded task object with its transition results, format:

```
{
  "blocks": int,
  "spec-version": string,
  "created": time,
  "results: {   // may not exist or be empty.
    <unique result key>: {
       "success": bool,
       "created": time,
       "client-vendor": string,
       "client-version": string,
       "post-hash": string
    },
    ... more results
  }
}
```

Storage result link formats:

- inputs: `<spec-version>/<key>/{pre.ssz, block_%d.ssz}`
- results: `<spec-version>/<key>/results/<client-vendor>/<client-version>/<result-key>/{post.ssz, out_log.txt, err_log.txt}`

Queried on the storage API endpoint: `https://storage.googleapis.com`
