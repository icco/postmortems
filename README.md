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
categorize      Scrape each postmortem's source URL and suggest categories.
enrich          Fetch each source URL (with Wayback fallback), extract metadata,
                ask Gemini for incident times/product/expanded description,
                and write the merged result back.
```

### `categorize`

The `categorize` action fetches each postmortem's source URL and greps
the response body against a set of regular expressions per category
(see `tool/categorize.go`). Without `-apply` it runs as a dry-run that
prints suggestions to stdout. With `-apply` it merges the suggested
categories back into each Markdown file.

```sh
# dry run
go run ./tool -action=categorize

# write suggestions back into ./data/*.md
go run ./tool -action=categorize -apply
```

The tool is intentionally a one-shot helper: review the diff before
committing. There is no automated PR-filing bot.

### `enrich`

The `enrich` action goes online for every entry under `data/`, pulls
the source page (falling back to the Wayback Machine when the origin
is dead), extracts cheap metadata (`<title>`, `og:title`, JSON-LD
`author` / `datePublished`, `<time datetime>`) via regex, and then
asks Vertex AI Gemini for the harder fields: a curated `title`, a
specific `product`, the `start_time` / `end_time` of the incident, a
short `keywords` list, and a 3–6 paragraph `expanded_description` that
replaces the original one-liner. The original blurb is preserved into
the new `summary:` frontmatter field, and `archive_url:` is recorded
for every entry so dead links stay accessible later.

```sh
# Required env vars (Application Default Credentials):
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
export GOOGLE_CLOUD_PROJECT=my-gcp-project   # or pass -gcp-project

# Dry run: report what would change without writing.
go run ./tool -action=enrich

# Write changes back, only filling blank fields.
go run ./tool -action=enrich -apply

# Force overwrite of non-empty title/product/start_time/end_time.
go run ./tool -action=enrich -apply -force

# Refresh metadata only, leave the description body alone.
go run ./tool -action=enrich -apply -keep-description

# Process a single entry by UUID prefix.
go run ./tool -action=enrich -apply -only=01494547

# Re-enrich entries last touched more than 14 days ago.
go run ./tool -action=enrich -apply -max-age=336h
```

Other useful flags: `-gcp-location` (default `us-central1`),
`-gemini-model` (default `gemini-2.5-flash`), `-enrich-workers`
(default 4), and `-http-timeout` (shared with `categorize`, default
15s). Cost is roughly $0.10–$1 to enrich the full corpus on
`gemini-2.5-flash`; review diffs before committing.

After enriching, regenerate the published JSON:

```sh
go run ./tool -action=generate
```

## Contributing

Edit a file under `data/` or open an issue.

## Shoutouts

- @dastergon for fixing bugs, porting hacky Ruby to Go, and implementing the webserver.
  See also [postmortem-templates](https://github.com/dastergon/postmortem-templates).
