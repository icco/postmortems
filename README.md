# postmortems

[![GoDoc](https://godoc.org/github.com/icco/postmortems?status.svg)](https://godoc.org/github.com/icco/postmortems)

Available at https://postmortems.app

This repo means to create a public repository of postmortems with annotated metadata and summaries of public postmortem documents. This repo takes the work in https://github.com/danluu/post-mortems and expands on it, trying to add categories, time data, and room for more in-depth analysis.

If you'd like to contribute, either edit a postmortem file in the data folder, or try to fix an issue.

If you want further process the postmortem metadata files locally, we have a [folder](https://postmortems.app/output/) with all the metadata in JSON format. Please let us know your findings.

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
```

## Shoutouts

 - @dastergon for fixing a bunch of bugs, porting hacky Ruby code to Go, and implementing webserver!

If you would like to find postmortem templates from various companies, you can check on Github at the [postmortem-templates](https://github.com/dastergon/postmortem-templates) repository.
