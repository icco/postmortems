package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/icco/postmortems"
)

// enrichOptions configures the enrich action. Sensible defaults are
// applied in EnrichPostmortems so callers can pass a zero value for
// non-critical fields.
type enrichOptions struct {
	Dir             string
	Only            string // process only this UUID; empty means all
	Apply           bool   // write changes back; false = report only
	Force           bool   // overwrite non-empty fields
	KeepDescription bool   // preserve existing Description body
	MaxAge          time.Duration
	HTTPTimeout     time.Duration
	Concurrency     int
	LLM             LLMClient    // injectable for tests
	Logger          *slog.Logger // diagnostics; defaults to slog.Default()
}

// enrichResult records the outcome of processing one .md file. Err is
// set on any terminal failure (load, fetch, llm, save); Skipped is set
// when -max-age caused us to leave the file alone. Changed lists the
// fields the merge layer would or did update.
type enrichResult struct {
	Path        string
	UUID        string
	URL         string
	UsedArchive bool
	Skipped     string
	Changed     []string
	Confidence  string
	Err         error
}

// EnrichPostmortems walks every postmortem under opts.Dir, fetches its
// source, asks the LLM for structured metadata, and (when Apply is
// true) writes the merged result back. Errors per-file are surfaced on
// the result struct rather than aborting the whole run.
func EnrichPostmortems(ctx context.Context, opts enrichOptions) ([]enrichResult, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("dir is required")
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.HTTPTimeout <= 0 {
		opts.HTTPTimeout = 20 * time.Second
	}
	if opts.MaxAge < 0 {
		opts.MaxAge = 0
	}
	if opts.LLM == nil {
		return nil, fmt.Errorf("LLM client is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	files, err := os.ReadDir(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	fetcher := NewFetcher(opts.HTTPTimeout)

	jobs := make(chan string)
	results := make(chan enrichResult)

	var wg sync.WaitGroup
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				results <- enrichOne(ctx, fetcher, opts, path)
			}
		}()
	}

	go func() {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			name := f.Name()
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			if opts.Only != "" && !strings.HasPrefix(name, opts.Only) {
				continue
			}
			jobs <- filepath.Join(opts.Dir, name)
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var out []enrichResult
	for r := range results {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// enrichOne runs the full pipeline for a single file: load, freshness
// check, fetch, extract HTML metadata, LLM call, merge, save. Each
// step is allowed to fail; the result struct carries the failure (if
// any) plus whatever progress was made.
func enrichOne(ctx context.Context, fetcher *Fetcher, opts enrichOptions, path string) enrichResult {
	res := enrichResult{Path: path}

	pm, err := loadPostmortem(path)
	if err != nil {
		res.Err = fmt.Errorf("load: %w", err)
		return res
	}
	res.UUID = pm.UUID
	res.URL = pm.URL

	if opts.MaxAge > 0 && !pm.SourceFetchedAt.IsZero() {
		if time.Since(pm.SourceFetchedAt) < opts.MaxAge {
			res.Skipped = "fresh"
			return res
		}
	}

	if pm.URL == "" {
		res.Err = fmt.Errorf("empty url in frontmatter")
		return res
	}

	fr, err := fetcher.Fetch(ctx, pm.URL)
	if err != nil {
		res.Err = fmt.Errorf("fetch: %w", err)
		return res
	}
	res.UsedArchive = fr.UsedArchive

	page := ExtractMetadata(fr.RawHTML)

	llmOut, err := opts.LLM.Enrich(ctx, EnrichInput{
		URL:         pm.URL,
		Company:     pm.Company,
		Existing:    pm,
		PageTitle:   page.Title,
		PageAuthor:  page.Author,
		PageDate:    page.PublishedAt,
		PageText:    page.PlainText,
		UsedArchive: fr.UsedArchive,
	})
	if err != nil {
		res.Err = fmt.Errorf("llm: %w", err)
		return res
	}
	res.Confidence = llmOut.Confidence

	changed := mergeEnrichment(pm, fr, page, llmOut, opts)
	res.Changed = changed

	if !opts.Apply || len(changed) == 0 {
		return res
	}

	if err := pm.Save(opts.Dir); err != nil {
		res.Err = fmt.Errorf("save: %w", err)
	}
	return res
}

// loadPostmortem opens path and returns the parsed Postmortem.
func loadPostmortem(path string) (*postmortems.Postmortem, error) {
	f, err := os.Open(path) // #nosec G304 -- iterated path under the configured data dir
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return postmortems.Parse(f)
}

// mergeEnrichment applies the merge policy:
//   - source_fetched_at and archive_url are always overwritten with the
//     latest fetch results.
//   - page-derived metadata (title/author/published_at) only fills in
//     blanks unless -force.
//   - LLM-derived fields (start/end/product/title/keywords) fill in
//     blanks unless -force; description is always replaced unless
//     -keep-description is set.
//   - The previous one-line description is preserved into Summary when
//     Summary is empty so we don't lose the human-curated blurb.
//
// Returns the list of field names that were modified, which doubles as
// a "did anything change" signal for the caller.
func mergeEnrichment(pm *postmortems.Postmortem, fr FetchResult, page PageMetadata, llm EnrichOutput, opts enrichOptions) []string {
	var changed []string
	set := func(name string, cur, next string, allowOverwrite bool) string {
		if next == "" {
			return cur
		}
		if cur != "" && !allowOverwrite {
			return cur
		}
		if cur == next {
			return cur
		}
		changed = append(changed, name)
		return next
	}
	setTime := func(name string, cur, next time.Time, allowOverwrite bool) time.Time {
		if next.IsZero() {
			return cur
		}
		if !cur.IsZero() && !allowOverwrite {
			return cur
		}
		if cur.Equal(next) {
			return cur
		}
		changed = append(changed, name)
		return next
	}

	if fr.ArchiveURL != "" && fr.ArchiveURL != pm.ArchiveURL {
		pm.ArchiveURL = fr.ArchiveURL
		changed = append(changed, "archive_url")
	}
	if !fr.FetchedAt.IsZero() {
		pm.SourceFetchedAt = fr.FetchedAt
		changed = append(changed, "source_fetched_at")
	}

	pm.Title = set("title", pm.Title, firstNonEmpty(llm.Title, page.Title), opts.Force)
	pm.Product = set("product", pm.Product, llm.Product, opts.Force)
	pm.SourceAuthor = set("source_author", pm.SourceAuthor, page.Author, opts.Force)
	pm.SourcePublishedAt = setTime("source_published_at", pm.SourcePublishedAt, page.PublishedAt, opts.Force)
	pm.StartTime = setTime("start_time", pm.StartTime, llm.StartTime, opts.Force)
	pm.EndTime = setTime("end_time", pm.EndTime, llm.EndTime, opts.Force)

	if len(llm.Keywords) > 0 {
		merged, added := mergeKeywords(pm.Keywords, llm.Keywords, opts.Force)
		if added {
			pm.Keywords = merged
			changed = append(changed, "keywords")
		}
	}

	if llm.ExpandedDescription != "" && !opts.KeepDescription {
		original := strings.TrimSpace(pm.Description)
		if pm.Summary == "" && original != "" && original != strings.TrimSpace(llm.ExpandedDescription) {
			pm.Summary = original
			changed = append(changed, "summary")
		}
		if strings.TrimSpace(pm.Description) != strings.TrimSpace(llm.ExpandedDescription) {
			pm.Description = strings.TrimSpace(llm.ExpandedDescription) + "\n"
			changed = append(changed, "description")
		}
	}

	return changed
}

// mergeKeywords unions existing and additions, preserving existing
// order then appending net-new keywords. Returns the merged slice and
// whether any new keywords were added (so callers know whether to mark
// the field as changed). When force is true, additions still cannot
// duplicate existing entries — overwrite semantics don't make sense
// for a free-form tag list.
func mergeKeywords(existing, additions []string, force bool) ([]string, bool) {
	have := map[string]bool{}
	for _, k := range existing {
		have[strings.ToLower(k)] = true
	}
	added := false
	out := append([]string{}, existing...)
	for _, k := range additions {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if have[strings.ToLower(k)] {
			continue
		}
		have[strings.ToLower(k)] = true
		out = append(out, k)
		added = true
	}
	_ = force
	return out, added
}

// firstNonEmpty returns the first non-empty string from the args, or
// empty if all are empty. Used to pick between LLM and HTML title.
func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// LogEnrichReport emits one structured log event per enrichment result
// plus a summary line. Logger may be nil, in which case slog.Default()
// is used. Mirrors the shape of the categorize tool's text report but
// rides on slog so the output is filterable / re-handlable.
func LogEnrichReport(logger *slog.Logger, res []enrichResult, apply bool) {
	if logger == nil {
		logger = slog.Default()
	}
	var (
		processed    int
		updated      int
		fetchErrs    int
		llmErrs      int
		archiveCount int
		skipped      int
	)
	for _, r := range res {
		processed++
		base := filepath.Base(r.Path)
		if r.Skipped != "" {
			skipped++
			logger.Debug("enrich skipped", "file", base, "reason", r.Skipped)
			continue
		}
		if r.UsedArchive {
			archiveCount++
		}
		if r.Err != nil {
			kind := "error"
			switch {
			case strings.HasPrefix(r.Err.Error(), "fetch:"):
				fetchErrs++
				kind = "fetch"
			case strings.HasPrefix(r.Err.Error(), "llm:"):
				llmErrs++
				kind = "llm"
			}
			logger.Error("enrich failed", "file", base, "url", r.URL, "kind", kind, "err", r.Err)
			continue
		}
		if len(r.Changed) == 0 {
			logger.Debug("enrich no-op", "file", base, "url", r.URL)
			continue
		}
		updated++
		logger.Info("enriched",
			"file", base,
			"url", r.URL,
			"applied", apply,
			"used_archive", r.UsedArchive,
			"confidence", r.Confidence,
			"changed", r.Changed,
		)
	}
	logger.Info("enrich summary",
		"processed", processed,
		"updated", updated,
		"skipped", skipped,
		"archive_fallback", archiveCount,
		"fetch_errors", fetchErrs,
		"llm_errors", llmErrs,
		"applied", apply,
	)
}
