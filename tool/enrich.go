package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/icco/postmortems"
)

// badTitlePatterns matches page-chrome titles (status pages, archive
// wrappers, captcha walls, blog/help-center landings, bare domains)
// that aren't real incident titles. See README "How enrich handles
// junk pages".
var badTitlePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(.{1,30}\s)?status$`),
	regexp.MustCompile(`(?i)^.{1,30} status page$`),
	regexp.MustCompile(`(?i)^wayback machine$`),
	regexp.MustCompile(`(?i)^internet archive$`),
	regexp.MustCompile(`(?i)^just a moment\.?\.?\.?$`),
	regexp.MustCompile(`(?i)^attention required.*$`),
	regexp.MustCompile(`(?i)^access denied$`),
	regexp.MustCompile(`(?i)^(403 forbidden|forbidden)$`),
	regexp.MustCompile(`(?i)^(404|page not found|not found)$`),
	regexp.MustCompile(`(?i)^untitled.*$`),
	regexp.MustCompile(`(?i)^redirect(ing)?(\.{0,3}|\x{2026})$`),
	regexp.MustCompile(`(?i)^help center.*$`),
	regexp.MustCompile(`(?i)^loading\.?\.?\.?$`),
	regexp.MustCompile(`(?i)please wait.*verification`),
	regexp.MustCompile(`(?i)^please wait.*$`),
	regexp.MustCompile(`(?i)^updates? on the status of .*$`),
	regexp.MustCompile(`(?i)^.{1,40} blog: .*$`),
	regexp.MustCompile(`(?i)^.{1,40} tech blog$`),
	regexp.MustCompile(`(?i)^.{1,40} blog$`),
	regexp.MustCompile(`(?i)^[a-z0-9-]+\.(io|com|net|org|dev|tech|co)$`),
}

// isBadTitle reports whether s looks like generic page chrome rather
// than a real incident title.
func isBadTitle(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	for _, re := range badTitlePatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// junkDescriptionPatterns matches Gemini's "I had nothing to work with"
// disclaimers. A match means we discard the whole LLM result rather
// than persist it. See README "How enrich handles junk pages".
var junkDescriptionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bthe (provided|given) (article )?(text|content|source)\b`),
	regexp.MustCompile(`(?i)\bdoes not contain (any )?(specific )?(details|information|content)\b`),
	regexp.MustCompile(`(?i)\bcannot be (extracted|generated|determined|derived)\b`),
	regexp.MustCompile(`(?i)\bis (extremely |too )(brief|short)\b`),
	regexp.MustCompile(`(?i)\bis a (marketing|product overview|promotional|landing|sales).*\bpage\b`),
	regexp.MustCompile(`(?i)\bis a marketing\b`),
	regexp.MustCompile(`(?i)\bis the (general )?(home|blog|tech blog|status) ?page\b`),
	regexp.MustCompile(`(?i)\bis a placeholder\b`),
	regexp.MustCompile(`(?i)\bis a status page (for|of)\b`),
	regexp.MustCompile(`(?i)\bin raw pdf format\b`),
	regexp.MustCompile(`(?i)\bwayback machine capture metadata\b`),
	regexp.MustCompile(`(?i)\bdomain is for sale\b`),
	regexp.MustCompile(`(?i)\bredirecting\.?\.?\.?$`),
	regexp.MustCompile(`(?i)\b(captcha|cloudflare challenge|browser verification)\b`),
	regexp.MustCompile(`(?i)\barchive team\b.*\bvolunteers\b`),
	regexp.MustCompile(`(?i)\bnot the (specific )?(post-?mortem|incident|outage)\b`),
	regexp.MustCompile(`(?i)\bdetails (about|regarding) (a |the )?(timeline|incident|outage|root cause)\b`),
	regexp.MustCompile(`(?i)\bis a (comprehensive )?product page\b`),
	regexp.MustCompile(`(?i)\bmarketing and informational resource\b`),
	regexp.MustCompile(`(?i)\bnot detailed in the provided\b`),
	regexp.MustCompile(`(?i)\bis not (available|present|detailed) (from|in) the (provided|given|article)\b`),
}

// titleFromPage reports whether stored looks like it was scraped from
// page's <title>. Loose match (case-insensitive, either side may be
// substring) tolerates site suffixes like " | Acme" that come and go.
func titleFromPage(stored, page string) bool {
	stored = strings.TrimSpace(stored)
	page = strings.TrimSpace(page)
	if stored == "" || page == "" {
		return isBadTitle(stored)
	}
	if strings.EqualFold(stored, page) {
		return true
	}
	storedLower := strings.ToLower(stored)
	pageLower := strings.ToLower(page)
	if strings.Contains(pageLower, storedLower) || strings.Contains(storedLower, pageLower) {
		return true
	}
	return isBadTitle(stored)
}

// appendUnique appends s to xs if it's not already present.
func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// looksLikeJunkDescription reports whether s reads like an "LLM had no
// useful source" disclaimer rather than an actual incident write-up.
func looksLikeJunkDescription(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, re := range junkDescriptionPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

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

	// Pre-fetch cleanups. These get persisted via flushSave even when
	// the fetch or LLM call later fails.
	var preChanged []string

	// Unwrap a Wayback snapshot stored in url: into url + archive_url.
	if origin, snapshot, ok := ParseWaybackURL(pm.URL); ok {
		pm.URL = origin
		preChanged = append(preChanged, "url")
		if pm.ArchiveURL == "" {
			pm.ArchiveURL = snapshot
			preChanged = append(preChanged, "archive_url")
		}
		res.URL = origin
	}

	// Roll back a previous junk LLM pass: restore the original blurb
	// from summary and drop the title/keywords it scraped from page
	// chrome.
	if looksLikeJunkDescription(pm.Description) && strings.TrimSpace(pm.Summary) != "" {
		pm.Description = pm.Summary + "\n"
		pm.Summary = ""
		preChanged = append(preChanged, "description", "summary")
		if pm.Title != "" {
			pm.Title = ""
			preChanged = appendUnique(preChanged, "title")
		}
		if len(pm.Keywords) > 0 {
			pm.Keywords = nil
			preChanged = appendUnique(preChanged, "keywords")
		}
	}

	fr, err := fetcher.Fetch(ctx, pm.URL)
	if err != nil {
		res.Err = fmt.Errorf("fetch: %w", err)
		res.Changed = preChanged
		flushSave(pm, opts, &res, preChanged)
		return res
	}
	res.UsedArchive = fr.UsedArchive

	page := ExtractMetadata(fr.RawHTML)
	llmIn := EnrichInput{
		URL:         pm.URL,
		Company:     pm.Company,
		Existing:    pm,
		PageTitle:   page.Title,
		PageDate:    page.PublishedAt,
		PageText:    page.PlainText,
		UsedArchive: fr.UsedArchive,
	}
	llmOut, err := opts.LLM.Enrich(ctx, llmIn)
	if err != nil {
		res.Err = fmt.Errorf("llm: %w", err)
		res.Changed = preChanged
		flushSave(pm, opts, &res, preChanged)
		return res
	}

	// Origin is dead-but-200 (status page, captcha, parking, rebrand):
	// retry once via the Wayback snapshot. We prefer the URL from the
	// availability API but fall back to a curated archive_url already
	// in the file, since the API sometimes claims no snapshot when one
	// clearly exists.
	archiveCandidate := fr.ArchiveURL
	if archiveCandidate == "" {
		archiveCandidate = pm.ArchiveURL
	}
	if _, ifSnap, ok := ParseWaybackURL(archiveCandidate); ok {
		archiveCandidate = ifSnap
	}
	if looksLikeJunkDescription(llmOut.ExpandedDescription) && !fr.UsedArchive && archiveCandidate != "" && archiveCandidate != pm.URL {
		archiveHTML, _, archiveErr := fetcher.GetRaw(ctx, archiveCandidate)
		if archiveErr == nil && strings.TrimSpace(archiveHTML) != "" {
			page2 := ExtractMetadata(archiveHTML)
			if strings.TrimSpace(page2.PlainText) != "" {
				llmIn2 := llmIn
				llmIn2.PageTitle = page2.Title
				llmIn2.PageDate = page2.PublishedAt
				llmIn2.PageText = page2.PlainText
				llmIn2.UsedArchive = true
				llmOut2, err2 := opts.LLM.Enrich(ctx, llmIn2)
				if err2 == nil && !looksLikeJunkDescription(llmOut2.ExpandedDescription) {
					page = page2
					llmOut = llmOut2
					fr.UsedArchive = true
					fr.RawHTML = archiveHTML
					res.UsedArchive = true
				}
			}
		}
	}
	res.Confidence = llmOut.Confidence

	// Still junk after retry: drop the LLM result, drop page metadata,
	// and wipe a stored title that obviously came from this same page.
	if looksLikeJunkDescription(llmOut.ExpandedDescription) {
		if pm.Title != "" && titleFromPage(pm.Title, page.Title) {
			pm.Title = ""
			preChanged = appendUnique(preChanged, "title")
		}
		llmOut = EnrichOutput{Confidence: llmOut.Confidence, Notes: llmOut.Notes}
		page = PageMetadata{}
	}

	changed := mergeEnrichment(pm, fr, page, llmOut, opts)
	for _, c := range preChanged {
		if !contains(changed, c) {
			changed = append(changed, c)
		}
	}
	res.Changed = changed
	flushSave(pm, opts, &res, changed)
	return res
}

// flushSave writes pm back to disk when apply mode is on and at least
// one field changed, surfacing any save error on res. Lets the caller
// keep the existing fetch/llm error if both happen.
func flushSave(pm *postmortems.Postmortem, opts enrichOptions, res *enrichResult, changed []string) {
	if !opts.Apply || len(changed) == 0 {
		return
	}
	if err := pm.Save(opts.Dir); err != nil {
		if res.Err != nil {
			res.Err = fmt.Errorf("%w; save: %w", res.Err, err)
		} else {
			res.Err = fmt.Errorf("save: %w", err)
		}
	}
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
//   - title/product/start/end/published_at: fill blanks; -force overwrites.
//   - description: rewritten unless -keep-description; old body moves to summary.
//   - keywords: union (case-insensitive).
//   - categories: regex matches on the page text are unioned with existing.
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

	pm.Title, changed = applyTitle(pm.Title, llm.Title, page.Title, opts.Force, changed)
	pm.Product = set("product", pm.Product, llm.Product, opts.Force)
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

	if suggestions := matchCategories(page.PlainText, pm.Categories); len(suggestions) > 0 {
		pm.Categories = mergeCategories(pm.Categories, suggestions)
		changed = append(changed, "categories")
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

// nonBad returns s unless it matches a known-bad title pattern.
func nonBad(s string) string {
	if isBadTitle(s) {
		return ""
	}
	return s
}

// applyTitle picks the best title for pm given the llm/page candidates
// and the -force flag, treating known-bad titles as empty so they get
// replaced or wiped rather than persisted. Returns the new title and
// the (possibly extended) changed list.
func applyTitle(existing, llmTitle, pageTitle string, force bool, changed []string) (string, []string) {
	existingBad := isBadTitle(existing)
	next := firstNonEmpty(nonBad(llmTitle), nonBad(pageTitle))
	switch {
	case existingBad && next != "":
		if existing != next {
			changed = append(changed, "title")
		}
		return next, changed
	case existingBad && next == "":
		if existing != "" {
			changed = append(changed, "title")
		}
		return "", changed
	case !existingBad && next != "" && force && existing != next:
		changed = append(changed, "title")
		return next, changed
	default:
		return existing, changed
	}
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
