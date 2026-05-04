package postmortems

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"

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
	SkippedExisting int           // upstream entries already in dir (canonical URL match)
	SkippedInvalid  int           // upstream lines rejected (malformed URL etc.)
}

// ExtractPostmortems imports each postmortem entry from loc into its
// own file under dir. The import is additive: an entry whose URL
// canonicalises to one already present in dir is skipped, so previously
// enriched fields (title, dates, archive URL, summary, expanded body
// etc.) are preserved across re-imports. The returned report lists the
// freshly-saved entries so callers can chain follow-up work (e.g.
// enrichment) without re-scanning the directory.
func ExtractPostmortems(loc string, dir string) (*ImportReport, error) {
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
	if isURL(loc) {
		// #nosec G107
		resp, err := http.Get(loc)
		if err != nil {
			return nil, fmt.Errorf("could not get %q: %w", loc, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Warnw("failed to close response body", "error", err)
			}
		}()

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("could not read response body: %w", err)
		}
	} else if isFile(loc) {
		data, err = os.ReadFile(loc) // #nosec G304
		if err != nil {
			return nil, fmt.Errorf("error opening file %q: %w", loc, err)
		}
	} else {
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

		// If the upstream URL is itself a Wayback snapshot, store the
		// origin URL in `url:` and the snapshot in `archive_url:` from
		// the start. This matches the post-enrich representation, so
		// the file shape is the same whether or not the LLM step ever
		// runs and re-imports stay idempotent.
		entryURL := rawURL
		var archiveURL string
		if origin, snapshot, ok := unwrapWayback(rawURL); ok {
			entryURL = origin
			archiveURL = snapshot
		}

		pm := &Postmortem{
			UUID:        guuid.New().String(),
			URL:         entryURL,
			ArchiveURL:  archiveURL,
			Company:     company,
			Description: desc,
			Categories:  []string{categoryPostmortem},
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
