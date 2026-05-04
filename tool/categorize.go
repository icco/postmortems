package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/icco/postmortems"
)

// categoryPatterns maps a category name in postmortems.Categories to a
// set of case-insensitive regular expressions. If any pattern matches
// the body of a postmortem's source URL the category is suggested.
//
// We deliberately omit "postmortem" (every entry already has it) and
// "undescriptive" (a subjective judgement that should not be
// auto-applied).
var categoryPatterns = map[string][]*regexp.Regexp{
	"automation": {
		mustRegex(`\bautomation\b`),
		mustRegex(`\bautomated\b`),
		mustRegex(`\bauto[- ]?scaling\b`),
	},
	"cascading-failure": {
		mustRegex(`\bcascad(e|ing)\b`),
		mustRegex(`\bdomino\b`),
		mustRegex(`\bthundering herd\b`),
	},
	"cloud": {
		mustRegex(`\baws\b`),
		mustRegex(`\bamazon web services\b`),
		mustRegex(`\bgcp\b`),
		mustRegex(`\bgoogle cloud\b`),
		mustRegex(`\bazure\b`),
		mustRegex(`\bec2\b`),
		mustRegex(`\bs3\b`),
		mustRegex(`\bkubernetes\b`),
		mustRegex(`\bcloud provider\b`),
	},
	"config-change": {
		mustRegex(`\bconfig(uration)? change\b`),
		mustRegex(`\bbad config\b`),
		mustRegex(`\bmisconfigur(ation|ed)\b`),
		mustRegex(`\bdeploy(ment)?\b`),
		mustRegex(`\brollout\b`),
	},
	"hardware": {
		mustRegex(`\bhardware (failure|fault|issue)\b`),
		mustRegex(`\bdisk (failure|fault|fail)\b`),
		mustRegex(`\bssd (failure|fault)\b`),
		mustRegex(`\bnetwork card\b`),
		mustRegex(`\brouter (failure|fault)\b`),
		mustRegex(`\bpower (failure|outage|loss)\b`),
		mustRegex(`\bdata ?cent(re|er) (failure|outage)\b`),
	},
	"security": {
		mustRegex(`\bsecurity (incident|breach|advisory)\b`),
		mustRegex(`\bvulnerab(le|ility)\b`),
		mustRegex(`\bexploit(ed|ation)?\b`),
		mustRegex(`\bbreach\b`),
		mustRegex(`\bleaked? credential\b`),
		mustRegex(`\bcve-\d{4}-\d+`),
	},
	"time": {
		mustRegex(`\bntp\b`),
		mustRegex(`\btimezone\b`),
		mustRegex(`\bleap second\b`),
		mustRegex(`\bdaylight saving\b`),
		mustRegex(`\bclock skew\b`),
	},
}

func mustRegex(p string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + p)
}

// categorizeOptions configures the categorize action.
type categorizeOptions struct {
	Dir         string
	Apply       bool
	HTTPTimeout time.Duration
	Concurrency int
}

// categorizeResult is one tool report row.
type categorizeResult struct {
	Path        string
	URL         string
	Existing    []string
	Suggestions []string
	Err         error
	StatusCode  int
}

// CategorizePostmortems scrapes every postmortem source URL under dir,
// matches the body against categoryPatterns, and reports (or applies)
// suggested categories. It returns the per-file results so callers
// can render their own summaries; errors fetching individual URLs are
// reported on the result struct rather than aborting the whole run.
func CategorizePostmortems(opts categorizeOptions) ([]categorizeResult, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("dir is required")
	}
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 15 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}

	files, err := os.ReadDir(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	type job struct {
		path string
	}
	jobs := make(chan job)
	results := make(chan categorizeResult)

	client := &http.Client{Timeout: opts.HTTPTimeout}

	var wg sync.WaitGroup
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- processFile(client, j.path, opts.Apply)
			}
		}()
	}

	go func() {
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			jobs <- job{path: filepath.Join(opts.Dir, f.Name())}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var out []categorizeResult
	for r := range results {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// processFile fetches one postmortem's source URL, matches the body
// against categoryPatterns, and (when apply is true) merges the
// suggested categories back into the markdown file.
func processFile(client *http.Client, path string, apply bool) categorizeResult {
	res := categorizeResult{Path: path}
	f, err := os.Open(path) // #nosec G304 -- iterated path under the configured data dir
	if err != nil {
		res.Err = err
		return res
	}
	pm, err := postmortems.Parse(f)
	closeErr := f.Close()
	if err != nil {
		res.Err = err
		return res
	}
	if closeErr != nil {
		res.Err = closeErr
		return res
	}

	res.URL = pm.URL
	res.Existing = append(res.Existing, pm.Categories...)

	if pm.URL == "" {
		res.Err = fmt.Errorf("empty url")
		return res
	}

	body, status, err := fetchBody(client, pm.URL)
	res.StatusCode = status
	if err != nil {
		res.Err = err
		return res
	}

	res.Suggestions = matchCategories(body, pm.Categories)
	if !apply || len(res.Suggestions) == 0 {
		return res
	}

	merged := mergeCategories(pm.Categories, res.Suggestions)
	pm.Categories = merged
	dir := filepath.Dir(path)
	if err := pm.Save(dir); err != nil {
		res.Err = fmt.Errorf("save: %w", err)
	}
	return res
}

func fetchBody(client *http.Client, u string) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "icco-postmortems-categorizer/1.0 (+https://postmortems.app)")
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, fmt.Errorf("http %d", resp.StatusCode)
	}

	const maxRead = 4 * 1024 * 1024 // 4 MiB cap, sufficient for a long blog post.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(data), resp.StatusCode, nil
}

// matchCategories returns the categories whose patterns match the body
// and that are not already in existing.
func matchCategories(body string, existing []string) []string {
	have := map[string]bool{}
	for _, c := range existing {
		have[c] = true
	}

	var out []string
	for _, cat := range postmortems.Categories {
		patterns, ok := categoryPatterns[cat]
		if !ok {
			continue
		}
		if have[cat] {
			continue
		}
		for _, p := range patterns {
			if p.MatchString(body) {
				out = append(out, cat)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// mergeCategories returns the union of existing and suggestions in a
// deterministic order driven by postmortems.Categories.
func mergeCategories(existing, suggestions []string) []string {
	have := map[string]bool{}
	for _, c := range existing {
		have[c] = true
	}
	for _, c := range suggestions {
		have[c] = true
	}
	var out []string
	for _, c := range postmortems.Categories {
		if have[c] {
			out = append(out, c)
		}
	}
	for _, c := range existing {
		if !contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// printCategorizeReport writes a human-readable summary of res to w.
func printCategorizeReport(w io.Writer, res []categorizeResult, apply bool) {
	var changed, fetchErrs, total int
	for _, r := range res {
		total++
		if r.Err != nil {
			fetchErrs++
			fmt.Fprintf(w, "ERR  %s (%s): %v\n", filepath.Base(r.Path), r.URL, r.Err)
			continue
		}
		if len(r.Suggestions) == 0 {
			continue
		}
		changed++
		marker := "SUGGEST"
		if apply {
			marker = "APPLIED"
		}
		fmt.Fprintf(w, "%s %s -> %v\n", marker, filepath.Base(r.Path), r.Suggestions)
	}
	fmt.Fprintf(w, "\nprocessed=%d  with-suggestions=%d  fetch-errors=%d\n", total, changed, fetchErrs)
}
