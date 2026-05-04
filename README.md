# postmortems

[![GoDoc](https://godoc.org/github.com/icco/postmortems?status.svg)](https://godoc.org/github.com/icco/postmortems) [![Go Report Card](https://goreportcard.com/badge/github.com/icco/postmortems)](https://goreportcard.com/report/github.com/icco/postmortems)

Public corpus of annotated postmortems, hosted at <https://postmortems.app>. Builds on
[danluu/post-mortems](https://github.com/danluu/post-mortems) by adding categories, time
data, and room for in-depth analysis.

JSON metadata for every entry is published at <https://postmortems.app/output/>.

## Frontend

The site is a small Go webserver (`./tool -action=serve`) that
renders `html/template` files under `templates/`. The visual layer is
[Tailwind CSS 4](https://tailwindcss.com) + [daisyUI 5](https://daisyui.com)
loaded from `cdn.jsdelivr.net` (no Node build step), with a small
`static/styles.css` for the bits Tailwind/daisyUI don't cover (long-form
prose, the GitHub-corner ribbon, sortable-table affordances). See
`templates/layout.html` for the wiring; the migration tracking issue is
[#107](https://github.com/icco/postmortems/issues/107).

### Reporting & Web Vitals

Browser security reports (CSP, COOP/COEP, Reporting-API, etc.) and
[`web-vitals`](https://github.com/GoogleChrome/web-vitals) are sent to
[`reportd.natwelch.com`](https://reportd.natwelch.com)
([icco/reportd](https://github.com/icco/reportd)) under the `postmortems`
service slug. The previous Fathom snippet pointing at `a.natwelch.com`
was removed in the same change. The relevant pieces:

- `securityHeaders` middleware in `server/handlers.go` emits
  `Reporting-Endpoints`, `Report-To`, and a `Content-Security-Policy`
  whose `report-uri` / `report-to default` reference reportd.
- `templates/layout.html` posts Web Vitals (`CLS`, `FCP`, `INP`, `LCP`,
  `TTFB`) via `navigator.sendBeacon` (with a `fetch` fallback) to
  `https://reportd.natwelch.com/analytics/postmortems`.

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

`import` is the one-shot way to keep the corpus in sync. It:

1. Reads `-source` (default: the [danluu/post-mortems](https://github.com/danluu/post-mortems) README).
2. Skips any entry whose URL canonicalises (Wayback unwrap, http/https, `www.`,
   trailing slash, fragment) to one already on disk, so previously enriched
   fields are never overwritten.
3. Saves the rest as fresh `data/<uuid>.md` files.
4. Runs `enrich` on just those new UUIDs (unless `-no-enrich` or no GCP
   credentials, in which case it logs a warning and stops after step 3).

The steady state is cheap: nothing changes upstream → the run does an
HTTP fetch and exits. Set up a daily cron and forget it.

### `enrich`

For every `data/*.md`, `enrich` fetches the source URL (with Wayback
fallback), extracts page metadata, applies regex-based category
suggestions, and asks Vertex Gemini for incident times, product,
keywords and an expanded description. The original one-liner is
preserved in `summary:` so the long-form `description:` body can be
rewritten without losing the editorial blurb.

Needs `GOOGLE_APPLICATION_CREDENTIALS` and `GOOGLE_CLOUD_PROJECT` (or
`-gcp-project`). Default model `gemini-2.5-flash` (~$0.10–$1 for the
full corpus).

```sh
go run ./tool -action=enrich                    # dry run
go run ./tool -action=enrich -apply             # write changes
go run ./tool -action=enrich -apply -force      # overwrite non-empty fields
go run ./tool -action=enrich -apply -only=01494547
go run ./tool -action=enrich -apply -only=01494547,019eb098    # multiple UUID prefixes
```

Other flags: `-keep-description`, `-max-age=720h`, `-gcp-location`,
`-gemini-model`, `-enrich-workers`. After enriching, run
`go run ./tool -action=generate` to refresh `output/*.json`.

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

#### How enrich handles junk pages

A lot of the original sources are dead, paywalled, captcha-walled,
status-page chrome, or rebrand-redirected to marketing pages. Several
heuristics keep these from polluting the corpus:

1. **Wayback unwrap.** A `url:` that's already a Wayback snapshot is
   rewritten to the origin URL with the snapshot moved to `archive_url:`.
2. **Wayback retry.** When the live origin returns 200 but Gemini
   reports the page is useless, `enrich` re-fetches the Wayback snapshot
   and re-runs the LLM. The second result is only adopted when it
   passes the junk check.
3. **Junk-description detection.** Gemini emits stock disclaimers
   ("the provided article text…", "is a marketing page", "in raw PDF
   format", "domain is for sale", "captcha", etc.) when it has nothing
   to work with. `looksLikeJunkDescription` matches these and we
   discard the entire LLM result instead of writing it.
4. **Bad-title detection.** `isBadTitle` rejects scraped titles like
   `Heroku Status`, `PagerDuty Status Page`, `Wayback Machine`,
   `Redirecting…`, `Help Center Closed`, `Loading`, `Please wait …
   verification`, `Acme Tech Blog`, `Acme Blog: Stories, Tutorials,
   Releases`, and bare domains like `skyliner.io`.
5. **Stale-cleanup on re-run.** When a file already has a junk
   description on disk (from an earlier pass), the body is reverted to
   `summary` and the title/keywords from that pass are dropped before
   the new fetch runs. The cleanups persist even if the new fetch
   fails.

#### What still needs a human

- **Dead origin + no Wayback snapshot.** When both fail, the entry
  keeps its original blurb in `description` and minimal frontmatter.
- **Rebrand redirects** (`stackdriver.com` → Google Observability)
  where the new page reads as a coherent product description, so the
  junk-description regexes don't catch it. Hand-edit when you spot one.
- **PDF sources.** Currently fetched as bytes and ignored by the HTML
  extractor. Worth a follow-up to download and parse text.

## Contributing

Edit a file under `data/` or open an issue.

## Shoutouts

- @dastergon for fixing bugs, porting hacky Ruby to Go, and implementing the webserver.
  See also [postmortem-templates](https://github.com/dastergon/postmortem-templates).
