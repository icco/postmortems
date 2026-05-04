# postmortems

[![GoDoc](https://godoc.org/github.com/icco/postmortems?status.svg)](https://godoc.org/github.com/icco/postmortems) [![Go Report Card](https://goreportcard.com/badge/github.com/icco/postmortems)](https://goreportcard.com/report/github.com/icco/postmortems)

Public corpus of annotated postmortems, hosted at <https://postmortems.app>. Builds on
[danluu/post-mortems](https://github.com/danluu/post-mortems) by adding categories, time
data, and room for in-depth analysis.

JSON metadata for every entry is published at <https://postmortems.app/output/>.

## Frontend conventions

- Tailwind CSS 4 + daisyUI 5 are loaded from `cdn.jsdelivr.net` on
  purpose &mdash; we run no Node build step. `static/styles.css` is for
  the rare cases the CDN can't cover (e.g. blackfriday-rendered prose).
- Browser reports and Web Vitals are sent to
  [`reportd.natwelch.com`](https://github.com/icco/reportd) under the
  service slug `postmortems`. Don't reintroduce a third-party analytics
  snippet; if you need new metrics, send them through reportd.
- Security/reporting headers use
  [`unrolled/secure`](https://github.com/unrolled/secure) configured to
  match `icco/reportd` and `icco/inspiration`. Mirror those repos when
  changing the policy so all of natwelch's services stay in sync.

## Tool

```
$ go run ./tool
pm [options...]
Options:
-action     The action we should take.
-dir        The directory with Markdown files for to extract or parse. Defaults to ./data

Actions:
import          Pull the latest entries from -source (default:
                danluu/post-mortems README), additively save any new ones,
                then enrich just those new entries via Gemini. Idempotent
                and safe to run repeatedly.
generate        Generate JSON files from the postmortem Markdown files.
new             Create a new postmortem file.
validate        Validate the postmortem files in the directory.
serve           Serve the postmortem files in a small website.
enrich          Fetch each source URL (with Wayback fallback), extract metadata,
                run regex-based category suggestions, and ask Gemini for
                incident times/product/expanded description.
```

### `import`

```sh
go run ./tool -action=import                   # fetch upstream + enrich new entries
go run ./tool -action=import -no-enrich        # save new entries, skip the LLM step
go run ./tool -action=import -source=./list.md # custom source (URL or file path)
```

Entries whose URL canonicalises (Wayback unwrap, http/https, `www.`,
trailing slash, fragment) to one already on disk are skipped, so
previously enriched fields are never overwritten. With nothing new
upstream the run is one HTTP fetch and exits — safe to put on a cron.
Without GCP credentials, `import` still saves new entries and just
skips the enrich step with a warning.

### `enrich`

Needs `GOOGLE_APPLICATION_CREDENTIALS` and `GOOGLE_CLOUD_PROJECT` (or
`-gcp-project`). Default model `gemini-2.5-flash` (~$0.10–$1 for the
full corpus). Per entry: fetch (Wayback fallback) → extract metadata →
regex-based category suggestions → Vertex Gemini for incident
times/product/keywords/expanded description. The original one-liner
moves to `summary:` whenever the body is rewritten.

```sh
go run ./tool -action=enrich                                # dry run
go run ./tool -action=enrich -apply                         # write changes
go run ./tool -action=enrich -apply -force                  # overwrite non-empty fields
go run ./tool -action=enrich -apply -only=01494547,019eb098 # comma-separated UUID prefixes
```

Other flags: `-keep-description`, `-max-age=720h`, `-gcp-location`,
`-gemini-model`, `-enrich-workers`. Run `-action=generate` afterwards
to refresh `output/*.json`.

#### YAML schema

| Field | Source | Notes |
| --- | --- | --- |
| `url` | hand-curated | Wayback URLs auto-unwrapped to the origin. |
| `archive_url` | Wayback availability API | Recorded for every entry when a snapshot exists. |
| `title` | LLM > page `<title>` | Bad-title patterns (status page, captcha, blog index, bare domain) are treated as empty. |
| `start_time`, `end_time` | LLM | RFC3339; left unset on low confidence. |
| `categories` | hand-curated + regex | Suggestions are unioned with existing values. |
| `keywords` | LLM | Case-insensitive union with existing values. |
| `company`, `product` | hand-curated; LLM fills blanks | `-force` lets the LLM overwrite. |
| `summary` | original blurb | Auto-populated when `description` is rewritten. |
| `source_published_at` | page metadata (OpenGraph/JSON-LD) | |
| `source_fetched_at` | enrich run | Used for `-max-age` freshness skipping. |
| `description` (body) | LLM expansion | Falls back to `summary` if the LLM has nothing. |

#### Junk-page handling

Many sources are dead, paywalled, captcha-walled, or rebrand-redirected
to marketing pages. `enrich` defends the corpus by:

- **Unwrapping Wayback snapshots** in `url:` to origin + `archive_url:`
  so re-imports stay idempotent.
- **Retrying via Wayback** when the live origin returns 200 but Gemini
  emits one of the stock "I had nothing to work with" disclaimers
  (`looksLikeJunkDescription`). The retry result is only adopted when
  it passes the same check.
- **Rejecting bad scraped titles** matching `isBadTitle` —
  `Heroku Status`, `Wayback Machine`, `Help Center Closed`,
  `Redirecting…`, bare domains like `skyliner.io`, etc.
- **Cleaning junk on re-run.** A file that already has a junk body
  reverts to `summary` and drops the title/keywords from that pass
  before the new fetch runs; the cleanups persist even if the fetch
  fails.

Things still needing a hand-edit: dead origin + no Wayback snapshot
(blurb-only entries), rebrand redirects whose new page reads as a
coherent product description, and PDF sources (currently fetched as
bytes and ignored by the HTML extractor).

## Contributing

Edit a file under `data/` or open an issue.

## Shoutouts

- @dastergon for fixing bugs, porting hacky Ruby to Go, and implementing the webserver.
  See also [postmortem-templates](https://github.com/dastergon/postmortem-templates).
