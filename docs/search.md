# Search design

> Status: design doc, no code in this PR.
> Tracks: [#9](https://github.com/icco/postmortems/issues/9)

## Goal

Let visitors of <https://postmortems.app> find postmortems by free-text
query (e.g. `"BGP leak"`, `"S3 outage 2017"`, `"DNS"`). Today the only
ways to discover content are the index, category pages, and (after #25)
company pages.

## Constraints and shape of the problem

- The corpus is small and slow-moving: ~250 entries, edited by PR. The
  full data set re-fits in memory comfortably.
- The site is a Go binary on Cloud Run on GCP, fronted by Cloud Load
  Balancer. There is no database. JSON for every entry is published at
  `/output/*.json`.
- There are real maintainers, but ops budget for this side project is
  effectively zero. We do not want to operate stateful infra.
- We already serve a static site plus the Go binary. Anything we add
  should ideally be re-buildable in CI on a push to `main`.
- Privacy / search analytics are not a hard requirement; this is a
  public corpus.

That set of constraints makes a *static, build-time* search index very
attractive: ship the index as a regular asset, do search in the
browser, never operate a search service.

## Options considered

### 1. BigQuery

Per the original issue suggestion: dump JSON output to BQ and use SQL.

- **Pros:** the site already runs on GCP; BQ has free-tier generosity
  for a corpus this size; trivial to load via `bq load` from
  `/output/*.json`; powerful for ad-hoc exploration.
- **Cons:** BQ is not a low-latency UI search backend. Per-query
  costs are real if traffic spikes. We would still need a thin Go
  endpoint that turns user queries into safe SQL and renders results;
  that's more code than any of the static options below.
- **Verdict:** great for offline analytics, wrong shape for a
  user-facing search box on a 250-entry site.

### 2. Algolia (or Typesense Cloud, MeiliSearch Cloud)

Hosted search-as-a-service.

- **Pros:** best UI primitives (instant results, highlighting,
  typo-tolerance) with the least integration effort; the Algolia free
  tier comfortably fits this corpus.
- **Cons:** introduces an external dependency for a site that today
  has none beyond GCP and GitHub; long-term reliance on a third party;
  configuration via dashboard rather than `git`-tracked.
- **Verdict:** technically viable, philosophically wrong for a
  community-maintained, Markdown-in-`git` site. Skip unless we get
  bored of our own index.

### 3. Self-hosted MeiliSearch / Typesense

A search server we operate.

- **Pros:** good ergonomics, modern relevance.
- **Cons:** stateful service to run on Cloud Run with persistent disk
  (or a small Compute VM), backups, restarts, version upgrades, and a
  whole new failure mode. Total mismatch with the operational budget.
- **Verdict:** no.

### 4. lunr.js

Classic client-side full-text index built at build time, queried in
JavaScript in the browser.

- **Pros:** zero server-side state; the index is just a JSON asset; we
  already produce one JSON file per postmortem so building a lunr
  index from those is straightforward.
- **Cons:** the index file size can grow quickly even on small
  corpora; the search UI is on us; lunr is in maintenance mode.
- **Verdict:** workable but worse than option 5 today.

### 5. Pagefind

A static-site search index that's effectively the modern lunr: it
crawls the rendered HTML at build time, produces a sharded index, and
ships a small JS runtime that fetches only the shards relevant to a
given query.

- **Pros:** purpose-built for static content; index shards keep page
  weight small; ranking quality is good for a corpus this size; usage
  is `pagefind` (CLI) plus a `<link>` and a tiny snippet on the page;
  no service to operate; a totally static asset, deployable next to
  `static/` or via the existing GCS bucket.
- **Cons:** node tool in our otherwise pure-Go pipeline; we would add
  a build step. Index needs rebuild when `data/` changes (i.e. on
  every merge to `main`).
- **Verdict:** **recommended.**

## Recommendation

Use **Pagefind**, indexed at build time against the rendered HTML.

Rationale, in priority order:

1. Matches our deployment model (static-ish Go site, no DB).
2. Zero operational surface: a `<script>` and a JSON folder of shards.
3. Re-builds deterministically from the same Markdown that drives
   `/output/*.json`, so the index can never drift further than one
   merge.
4. If we outgrow it, options 1 (BQ for analytics) and 2 (Algolia for
   UI) remain available without lock-in: search is a frontend
   concern at that point.

## Sketch of the integration

1. **Build step.** Add a `make search-index` (and a step in the
   GitHub Actions release workflow) that:
   1. Spins up the Go server in a temporary mode that writes every
      page to disk under `./public/` (a `-static-export` flag on
      `tool/` would work, or a small wrapper script that `wget`s each
      route into `./public/`).
   2. Runs `npx -y pagefind --site ./public --output-path ./public/pagefind`.
   3. Uploads `./public/pagefind/` next to the existing `output/` JSON
      so it is served as a static asset by the Go binary.
2. **Serving.** Mount `./public/pagefind/` at `/pagefind/` in
   `server/handlers.go` (the existing `r.Handle("/output/*", ...)`
   pattern is the template).
3. **UI.** Add a `<input type="search">` to `templates/layout.html`
   plus the Pagefind UI snippet:

   ```html
   <link rel="stylesheet" href="/pagefind/pagefind-ui.css">
   <script src="/pagefind/pagefind-ui.js"></script>
   <div id="search"></div>
   <script>
     window.addEventListener("DOMContentLoaded", () => {
       new PagefindUI({ element: "#search", showSubResults: true });
     });
   </script>
   ```

4. **Indexable content.** Pagefind reads `data-pagefind-body` /
   `data-pagefind-meta` annotations on the page. We would tag the
   description block on `templates/postmortem.html` with
   `data-pagefind-body` so only the actual postmortem text (not the
   global nav) feeds the index, and stamp company / categories /
   keywords / event date as metadata so we can offer faceted filters
   later.

## Out of scope for this design

- Faceted UI (company / category / keyword filters). Pagefind supports
  it; it's a follow-up.
- Surfacing search results inline on the index. We can do that with
  the Pagefind API rather than the bundled UI, but it's not a v1
  requirement.
- BigQuery for analytics. Could be added independently of search;
  worth a separate doc if/when someone wants it.

## Decision checklist before implementation

- [ ] Confirm the static-export approach (do we render every page from
  Go, or do we change the site to be statically generated end-to-end?
  The latter is more work but cleaner long-term.)
- [ ] Decide where the Pagefind output lives: alongside `static/` in
  the repo, or built fresh in CI? Recommend CI, to avoid a 1MB+ blob
  in `git`.
- [ ] Pick a node version pin (`actions/setup-node@v4`).
