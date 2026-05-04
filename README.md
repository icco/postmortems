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
extract     Extract postmortems from the collection and create separate files.
generate    Generate JSON files from the postmortem Markdown files.
new         Create a new postmortem file.
validate    Validate the postmortem files in the directory.
serve       Serve the postmortem files in a small website.
categorize  Scrape each postmortem's source URL and suggest categories.
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

## Contributing

Edit a file under `data/` or open an issue.

## Shoutouts

- @dastergon for fixing bugs, porting hacky Ruby to Go, and implementing the webserver.
  See also [postmortem-templates](https://github.com/dastergon/postmortem-templates).
