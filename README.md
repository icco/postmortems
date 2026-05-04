# postmortems

[![GoDoc](https://godoc.org/github.com/icco/postmortems?status.svg)](https://godoc.org/github.com/icco/postmortems) [![Go Report Card](https://goreportcard.com/badge/github.com/icco/postmortems)](https://goreportcard.com/report/github.com/icco/postmortems)

Public corpus of annotated postmortems, hosted at <https://postmortems.app>. Builds on
[danluu/post-mortems](https://github.com/danluu/post-mortems) by adding categories, time
data, and room for in-depth analysis.

JSON metadata for every entry is published at <https://postmortems.app/output/>.

## Tool

```
$ go run ./tool
pm [options...]
Options:
-action     The action we should take.
-dir        The directory with Markdown files for to extract or parse. Defaults to ./data

Actions:
extract         Extract postmortems from the collection and create separate files.
upstream-fetch  Download and extract postmortems from danluu/post-mortems.
generate        Generate JSON files from the postmortem Markdown files.
new             Create a new postmortem file.
validate        Validate the postmortem files in the directory.
serve           Serve the postmortem files in a small website.
enrich          Fetch each source URL (with Wayback fallback), extract metadata,
                run regex-based category suggestions, and ask Gemini for
                incident times/product/expanded description.
```

### `enrich`

Fetches each `data/*.md` source URL (Wayback fallback for dead links),
extracts page metadata, runs regex matching to suggest additional
`categories`, and asks Vertex Gemini for incident times, product,
keywords, and an expanded description. The old one-liner moves into a
`summary:` field; `archive_url:` is recorded for every entry. Wayback
URLs in the `url:` field are unwrapped to their original target and
the snapshot moves to `archive_url:`.

Needs `GOOGLE_APPLICATION_CREDENTIALS` and `GOOGLE_CLOUD_PROJECT` (or
`-gcp-project`). Default model `gemini-2.5-flash` (~$0.10–$1 for the
full corpus).

```sh
go run ./tool -action=enrich                    # dry run
go run ./tool -action=enrich -apply             # write changes
go run ./tool -action=enrich -apply -force      # overwrite non-empty fields
go run ./tool -action=enrich -apply -only=01494547
```

Other flags: `-keep-description`, `-max-age=720h`, `-gcp-location`,
`-gemini-model`, `-enrich-workers`. After enriching, run
`go run ./tool -action=generate` to refresh `output/*.json`.

## Contributing

Edit a file under `data/` or open an issue.

## Shoutouts

- @dastergon for fixing bugs, porting hacky Ruby to Go, and implementing the webserver.
  See also [postmortem-templates](https://github.com/dastergon/postmortem-templates).
