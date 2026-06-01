// Package postmortems extracts, parses, validates, and represents incident
// postmortems stored as Markdown files with YAML front matter.
package postmortems

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	guuid "github.com/google/uuid"
)

var (
	re       = regexp.MustCompile(`^\[(.+?)\]\((.+?)\)\. (.+)$`)
	bodyTmpl = `---
{{ yaml . }}
---

{{ .Description }}
`
)

// ImportReport summarises one ExtractPostmortems run.
type ImportReport struct {
	Source          string        // URL or file path that was read
	Added           []*Postmortem // entries newly written to disk
	SkippedExisting int           // upstream entries already in dir
	SkippedInvalid  int           // malformed/rejected upstream lines
}

// ExtractPostmortems writes each postmortem entry from loc into its own
// file under dir. The import is additive: entries whose canonical URL
// already exists in dir are skipped, so previously enriched fields are
// preserved. The returned report lists freshly-saved entries so callers
// can chain follow-up work without rescanning the directory.
func ExtractPostmortems(ctx context.Context, loc string, dir string) (*ImportReport, error) {
	posts, err := ValidateDir(dir)
	if err != nil {
		return nil, err
	}

	have := make(map[string]bool, len(posts)*2)
	for _, p := range posts {
		if p.URL != "" {
			have[CanonicalURL(p.URL)] = true
		}
		if p.ArchiveURL != "" {
			have[CanonicalURL(p.ArchiveURL)] = true
		}
	}

	var data []byte
	switch {
	case isURL(loc):
		reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		req, rerr := http.NewRequestWithContext(reqCtx, http.MethodGet, loc, nil)
		if rerr != nil {
			return nil, fmt.Errorf("could not build request for %q: %w", loc, rerr)
		}
		// #nosec G107 -- loc is supplied by the operator running the importer.
		resp, gerr := http.DefaultClient.Do(req)
		if gerr != nil {
			return nil, fmt.Errorf("could not get %q: %w", loc, gerr)
		}
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				log.Warnw("failed to close response body", "error", cerr)
			}
		}()

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("could not read response body: %w", err)
		}
	case isFile(loc):
		data, err = os.ReadFile(loc) // #nosec G304
		if err != nil {
			return nil, fmt.Errorf("error opening file %q: %w", loc, err)
		}
	default:
		return nil, fmt.Errorf("%q is not a file or a url", loc)
	}

	report := &ImportReport{Source: loc}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		matches := re.FindStringSubmatch(scanner.Text())
		if matches == nil {
			continue
		}
		company := matches[1]
		rawURL := matches[2]
		desc := matches[3]
		if !looksLikeSingleURL(rawURL) {
			report.SkippedInvalid++
			log.Warnw("skipping malformed entry", "url", rawURL, "company", company)
			continue
		}

		canon := CanonicalURL(rawURL)
		if have[canon] {
			report.SkippedExisting++
			continue
		}

		// Pre-unwrap Wayback snapshots so the file shape matches the
		// post-enrich representation and re-imports stay idempotent.
		entryURL := rawURL
		var archiveURL string
		if origin, snapshot, ok := ParseWaybackURL(rawURL); ok {
			entryURL = origin
			archiveURL = snapshot
		}

		pm := &Postmortem{
			UUID:        guuid.New().String(),
			URL:         entryURL,
			ArchiveURL:  archiveURL,
			Company:     company,
			Description: desc,
		}
		if pm.Company == "" || pm.Description == "" {
			report.SkippedInvalid++
			continue
		}

		if err := pm.Save(dir); err != nil {
			return nil, fmt.Errorf("error saving postmortem file: %w", err)
		}
		have[canon] = true
		report.Added = append(report.Added, pm)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	log.Infow("extracted postmortems",
		"source", loc,
		"added", len(report.Added),
		"skipped_existing", report.SkippedExisting,
		"skipped_invalid", report.SkippedInvalid,
	)

	return report, nil
}

func isURL(tgt string) bool {
	u, err := url.Parse(tgt)
	if err != nil {
		return false
	}

	return u.IsAbs() && u.Hostname() != ""
}

func isFile(tgt string) bool {
	info, err := os.Stat(tgt)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
