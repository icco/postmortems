package postmortems

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractPostmortems_AdditiveImport asserts the extractor:
//   - leaves an existing entry alone when the upstream URL canonicalises
//     to the same resource (so enriched fields aren't clobbered),
//   - creates a fresh file for each new upstream URL,
//   - skips malformed lines that capture multiple URLs in one match.
func TestExtractPostmortems_AdditiveImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	existing := `---
uuid: "11111111-1111-1111-1111-111111111111"
url: "https://example.com/incident"
title: "Hand-curated title"
categories:
- postmortem
keywords:
- dns
company: "Example"
product: "Example API"

---

Long enriched body.
`
	existingPath := filepath.Join(dir, "11111111-1111-1111-1111-111111111111.md")
	if err := os.WriteFile(existingPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	upstream := strings.Join([]string{
		"# A List of Post-mortems!",
		"",
		// matches existing entry under canonicalisation (http vs https,
		// trailing slash, www).
		"[Example](http://www.example.com/incident/). Short blurb that should NOT replace the enriched body.",
		"",
		"[NewCo](https://newco.example/postmortem). Brand new entry.",
		"",
		// malformed multi-link entry: must be skipped, not saved as a
		// single garbage URL.
		"[Combo](https://a.example/one), [see also](https://a.example/two). Combined.",
		"",
	}, "\n")

	src := filepath.Join(t.TempDir(), "upstream.md")
	if err := os.WriteFile(src, []byte(upstream), 0o600); err != nil {
		t.Fatalf("write upstream: %v", err)
	}

	if err := ExtractPostmortems(src, dir); err != nil {
		t.Fatalf("ExtractPostmortems: %v", err)
	}

	// Existing file: untouched.
	got, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("read existing: %v", err)
	}
	if string(got) != existing {
		t.Errorf("existing file modified.\nwant:\n%s\ngot:\n%s", existing, string(got))
	}

	// Directory should contain the existing file plus exactly one new
	// entry (the malformed combo line is skipped).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var mds []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mds = append(mds, e.Name())
		}
	}
	if len(mds) != 2 {
		t.Fatalf("expected 2 .md files (1 existing + 1 new), got %d: %v", len(mds), mds)
	}

	var newPath string
	for _, m := range mds {
		if m != "11111111-1111-1111-1111-111111111111.md" {
			newPath = filepath.Join(dir, m)
		}
	}
	body, err := os.ReadFile(newPath) // #nosec G304
	if err != nil {
		t.Fatalf("read new: %v", err)
	}
	for _, want := range []string{
		"url: https://newco.example/postmortem",
		"company: NewCo",
		"Brand new entry.",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("new entry missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(string(body), "[see also]") {
		t.Errorf("new entry shouldn't be the malformed combo line:\n%s", body)
	}
}
