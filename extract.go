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

// ExtractPostmortems imports each postmortem entry from loc into its
// own file under dir. The import is additive: an entry whose URL
// canonicalises to one already present in dir is skipped, so previously
// enriched fields (title, dates, archive URL, summary, expanded body
// etc.) are preserved across re-imports.
func ExtractPostmortems(loc string, dir string) error {
	posts, err := ValidateDir(dir)
	if err != nil {
		return err
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
			return fmt.Errorf("could not get %q: %w", loc, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Warnw("failed to close response body", "error", err)
			}
		}()

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("could not read response body: %w", err)
		}
	} else if isFile(loc) {
		data, err = os.ReadFile(loc) // #nosec G304
		if err != nil {
			return fmt.Errorf("error opening file %q: %w", loc, err)
		}
	} else {
		return fmt.Errorf("%q is not a file or a url", loc)
	}

	var added, skipped, invalid int
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
			invalid++
			log.Warnw("skipping malformed entry", "url", rawURL, "company", company)
			continue
		}

		canon := CanonicalURL(rawURL)
		if have[canon] {
			skipped++
			continue
		}

		pm := &Postmortem{
			UUID:        guuid.New().String(),
			URL:         rawURL,
			Company:     company,
			Description: desc,
			Categories:  []string{categoryPostmortem},
		}
		if pm.Company == "" || pm.Description == "" {
			invalid++
			continue
		}

		if err := pm.Save(dir); err != nil {
			return fmt.Errorf("error saving postmortem file: %w", err)
		}
		have[canon] = true
		added++
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	log.Infow("extracted postmortems",
		"source", loc,
		"added", added,
		"skipped_existing", skipped,
		"skipped_invalid", invalid,
	)

	return nil
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
