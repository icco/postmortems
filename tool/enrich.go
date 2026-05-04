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

// enrichOptions configures the enrich action.
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

// enrichResult records the outcome of processing one .md file.
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

// EnrichPostmortems walks opts.Dir, fetches each source, calls the
// LLM, and writes results back when Apply is true. Per-file errors are
// surfaced on enrichResult instead of aborting the run.
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

	var paths []string
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
		paths = append(paths, filepath.Join(opts.Dir, name))
	}
	total := len(paths)
	opts.Logger.Info("enrich starting", "files", total, "workers", opts.Concurrency, "apply", opts.Apply)

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
		for _, p := range paths {
			jobs <- p
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	out := make([]enrichResult, 0, total)
	done := 0
	for r := range results {
		done++
		out = append(out, r)
		logEnrichProgress(opts.Logger, r, done, total, opts.Apply)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// enrichOne runs load -> freshness check -> fetch -> extract -> LLM ->
// merge -> save for one file.
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

func loadPostmortem(path string) (*postmortems.Postmortem, error) {
	f, err := os.Open(path) // #nosec G304 -- iterated path under the configured data dir
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return postmortems.Parse(f)
}

// mergeEnrichment writes fetch + LLM output into pm. Policy:
//   - archive_url, source_fetched_at: always updated.
//   - title/product/author/start/end/published_at: fill blanks; -force overwrites.
//   - description: rewritten unless -keep-description; old body moves to summary.
//   - keywords: union (case-insensitive).
//
// Returns the names of changed fields.
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

// mergeKeywords unions existing and additions case-insensitively,
// preserving order. Returns merged and whether anything was added.
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

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// errKind classifies an enrichResult error for counting/labelling.
func errKind(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case strings.HasPrefix(err.Error(), "fetch:"):
		return "fetch"
	case strings.HasPrefix(err.Error(), "llm:"):
		return "llm"
	default:
		return "error"
	}
}

// logEnrichProgress emits one streaming event per completed file with a
// done/total counter so long runs show progress.
func logEnrichProgress(logger *slog.Logger, r enrichResult, done, total int, apply bool) {
	if logger == nil {
		logger = slog.Default()
	}
	base := filepath.Base(r.Path)
	switch {
	case r.Skipped != "":
		logger.Info("enrich progress",
			"done", done, "total", total,
			"file", base, "outcome", "skipped", "reason", r.Skipped,
		)
	case r.Err != nil:
		logger.Error("enrich progress",
			"done", done, "total", total,
			"file", base, "url", r.URL,
			"outcome", "error", "kind", errKind(r.Err), "err", r.Err,
		)
	case len(r.Changed) == 0:
		logger.Info("enrich progress",
			"done", done, "total", total,
			"file", base, "url", r.URL, "outcome", "no-op",
		)
	default:
		logger.Info("enrich progress",
			"done", done, "total", total,
			"file", base, "url", r.URL,
			"outcome", "updated", "applied", apply,
			"used_archive", r.UsedArchive, "confidence", r.Confidence,
			"changed", r.Changed,
		)
	}
}

// LogEnrichReport emits a final summary for a finished enrich run.
// Per-file events are streamed live by EnrichPostmortems via
// logEnrichProgress, so this only tallies counters.
func LogEnrichReport(logger *slog.Logger, res []enrichResult, apply bool) {
	if logger == nil {
		logger = slog.Default()
	}
	var processed, updated, fetchErrs, llmErrs, archiveCount, skipped int
	for _, r := range res {
		processed++
		switch {
		case r.Skipped != "":
			skipped++
		case r.Err != nil:
			switch errKind(r.Err) {
			case "fetch":
				fetchErrs++
			case "llm":
				llmErrs++
			}
		case len(r.Changed) > 0:
			updated++
		}
		if r.UsedArchive {
			archiveCount++
		}
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
