package postmortems

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// New entries whose upstream URL is itself a Wayback snapshot must
// land on disk pre-unwrapped (origin in url, snapshot in archive_url),
// so re-imports of the same line are a no-op.
func TestExtractPostmortems_NewWaybackEntryIsPreUnwrapped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "upstream.md")
	if err := os.WriteFile(src, []byte(
		"[Acme](https://web.archive.org/web/20220601000000/https://blog.acme.com/incident). The blurb.\n",
	), 0o600); err != nil {
		t.Fatalf("write upstream: %v", err)
	}

	report, err := ExtractPostmortems(src, dir)
	if err != nil {
		t.Fatalf("ExtractPostmortems: %v", err)
	}
	if got := len(report.Added); got != 1 {
		t.Fatalf("Added = %d, want 1", got)
	}
	pm := report.Added[0]
	if got, want := pm.URL, "https://blog.acme.com/incident"; got != want {
		t.Errorf("URL = %q, want %q (origin should be unwrapped)", got, want)
	}
	if got, want := pm.ArchiveURL, "https://web.archive.org/web/20220601000000if_/https://blog.acme.com/incident"; got != want {
		t.Errorf("ArchiveURL = %q, want %q", got, want)
	}

	// Re-import the same upstream line: should be a no-op now.
	report2, err := ExtractPostmortems(src, dir)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if len(report2.Added) != 0 {
		t.Errorf("re-import Added = %d, want 0; pre-unwrap state didn't round-trip",
			len(report2.Added))
	}
	if report2.SkippedExisting != 1 {
		t.Errorf("re-import SkippedExisting = %d, want 1", report2.SkippedExisting)
	}
}

// When upstream lists a Wayback snapshot for an entry we already have
// (in either origin or archive form), the import must skip it instead
// of creating a duplicate.
func TestExtractPostmortems_SkipsWhenUpstreamIsArchiveOfExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Case A: existing entry stores origin URL; upstream lists a
	// Wayback snapshot of that origin.
	existingA := `---
uuid: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
url: "https://blog.example.com/outage-2022"
categories:
- postmortem
company: "ExampleCo"

---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa.md"), []byte(existingA), 0o600); err != nil {
		t.Fatalf("seed A: %v", err)
	}

	// Case B: existing entry stores a Wayback snapshot in archive_url
	// (e.g. previous enrich pass) under a different timestamp than the
	// one upstream uses now.
	existingB := `---
uuid: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
url: "https://other.example.com/incident"
archive_url: "https://web.archive.org/web/20210101000000/https://other.example.com/incident"
categories:
- postmortem
company: "OtherCo"

---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb.md"), []byte(existingB), 0o600); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	// Case C: existing entry's URL is itself a Wayback snapshot (not
	// yet enriched/unwrapped); upstream lists the bare origin.
	existingC := `---
uuid: "cccccccc-cccc-cccc-cccc-cccccccccccc"
url: "https://web.archive.org/web/20180101000000/https://legacy.example.com/postmortem"
categories:
- postmortem
company: "LegacyCo"

---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "cccccccc-cccc-cccc-cccc-cccccccccccc.md"), []byte(existingC), 0o600); err != nil {
		t.Fatalf("seed C: %v", err)
	}

	upstream := strings.Join([]string{
		// matches A: origin known, upstream gives a snapshot
		"[ExampleCo](https://web.archive.org/web/20220601000000/https://blog.example.com/outage-2022). Same as A.",
		// matches B: same origin, different Wayback snapshot than archive_url
		"[OtherCo](https://web.archive.org/web/20240701000000/https://other.example.com/incident). Same as B.",
		// matches C: existing URL is wayback, upstream gives origin
		"[LegacyCo](https://legacy.example.com/postmortem). Same as C.",
	}, "\n")
	src := filepath.Join(t.TempDir(), "upstream.md")
	if err := os.WriteFile(src, []byte(upstream), 0o600); err != nil {
		t.Fatalf("write upstream: %v", err)
	}

	report, err := ExtractPostmortems(src, dir)
	if err != nil {
		t.Fatalf("ExtractPostmortems: %v", err)
	}
	if got := len(report.Added); got != 0 {
		t.Errorf("Added = %d, want 0; ExtractPostmortems treated archive vs origin as different",
			got)
	}
	if report.SkippedExisting != 3 {
		t.Errorf("SkippedExisting = %d, want 3", report.SkippedExisting)
	}
}

// ExtractPostmortems must leave existing entries untouched when the
// upstream URL canonicalises to the same resource, save fresh files
// for genuinely new URLs, and skip malformed multi-link lines.
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

	report, err := ExtractPostmortems(src, dir)
	if err != nil {
		t.Fatalf("ExtractPostmortems: %v", err)
	}
	if got := len(report.Added); got != 1 {
		t.Errorf("Added = %d, want 1", got)
	}
	if report.SkippedExisting != 1 {
		t.Errorf("SkippedExisting = %d, want 1", report.SkippedExisting)
	}
	if report.SkippedInvalid != 1 {
		t.Errorf("SkippedInvalid = %d, want 1 (the malformed combo line)", report.SkippedInvalid)
	}

	// Existing file: untouched.
	got, err := os.ReadFile(existingPath) // #nosec G304 -- path is t.TempDir() in tests
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
