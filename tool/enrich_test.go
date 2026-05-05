package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icco/postmortems"
)

// fakeLLM is a stub LLMClient that returns a canned EnrichOutput so the
// orchestrator can be exercised without hitting Vertex AI.
type fakeLLM struct {
	resp EnrichOutput
	err  error
	last EnrichInput
}

func (f *fakeLLM) Enrich(_ context.Context, in EnrichInput) (EnrichOutput, error) {
	f.last = in
	return f.resp, f.err
}
func (f *fakeLLM) Close() error { return nil }

const sampleHTML = `<!doctype html><html><head>
<title>Backend Down: Postmortem</title>
<meta property="og:title" content="Backend Down: Postmortem">
<meta property="article:published_time" content="2017-03-01T00:00:00Z">
</head><body>
<p>Our service was down between 17:37 and 19:01 UTC on Feb 28, 2017.</p>
</body></html>`

const sampleEntry = `---
uuid: "11111111-1111-1111-1111-111111111111"
url: "%s"
company: "ExampleCo"
product: ""

---

A short blurb about the outage.
`

func writeSampleEntry(t *testing.T, dir, originURL string) string {
	t.Helper()
	body := strings.Replace(sampleEntry, "%s", originURL, 1)
	fp := filepath.Join(dir, "11111111-1111-1111-1111-111111111111.md")
	if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	return fp
}

// readPM is a tiny helper that round-trips an on-disk entry through
// postmortems.Parse so tests can assert against the same representation
// the rest of the codebase uses.
func readPM(t *testing.T, fp string) *postmortems.Postmortem {
	t.Helper()
	f, err := os.Open(fp) // #nosec G304 -- path is t.TempDir() in tests
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	pm, err := postmortems.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return pm
}

func TestEnrich_FillsBlanksAndPreservesSummary(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	fp := writeSampleEntry(t, dir, origin.URL)

	llm := &fakeLLM{resp: EnrichOutput{
		Title:               "ExampleCo backend outage of 2017",
		Product:             "Backend API",
		StartTime:           time.Date(2017, 2, 28, 17, 37, 0, 0, time.UTC),
		EndTime:             time.Date(2017, 2, 28, 19, 1, 0, 0, time.UTC),
		Keywords:            []string{"backend", "ExampleCo"},
		ExpandedDescription: "A longer multi-paragraph description of the incident.\n\nDetails follow.",
		Confidence:          "high",
	}}

	res, err := EnrichPostmortems(context.Background(), enrichOptions{
		Dir:         dir,
		Apply:       true,
		HTTPTimeout: 5 * time.Second,
		LLM:         llm,
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("EnrichPostmortems: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("results = %d, want 1", len(res))
	}
	if res[0].Err != nil {
		t.Fatalf("result error: %v", res[0].Err)
	}

	pm := readPM(t, fp)
	if pm.Title != "ExampleCo backend outage of 2017" {
		t.Errorf("Title = %q", pm.Title)
	}
	if pm.Product != "Backend API" {
		t.Errorf("Product = %q", pm.Product)
	}
	if pm.SourcePublishedAt.IsZero() {
		t.Errorf("SourcePublishedAt is zero")
	}
	if pm.SourceFetchedAt.IsZero() {
		t.Errorf("SourceFetchedAt is zero")
	}
	if pm.StartTime.IsZero() || pm.EndTime.IsZero() {
		t.Errorf("StartTime/EndTime not set: %v / %v", pm.StartTime, pm.EndTime)
	}
	if !strings.Contains(pm.Description, "longer multi-paragraph") {
		t.Errorf("Description not rewritten: %q", pm.Description)
	}
	if !strings.Contains(pm.Summary, "short blurb") {
		t.Errorf("Summary should preserve original blurb, got %q", pm.Summary)
	}
	if got := strings.Join(pm.Keywords, ","); !strings.Contains(got, "backend") {
		t.Errorf("Keywords missing 'backend': %v", pm.Keywords)
	}
}

// TestOnlyMatches checks the comma-separated UUID-prefix filter so we
// can target a known set of new files (e.g. just-imported entries)
// without re-enriching the rest of the corpus.
func TestOnlyMatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, only string
		want       bool
	}{
		{name: "abc.md", only: "", want: true},
		{name: "abc.md", only: "abc", want: true},
		{name: "abc.md", only: "xyz", want: false},
		{name: "abc.md", only: "xyz,abc", want: true},
		{name: "abc.md", only: " xyz , abc ", want: true},
		{name: "abc.md", only: ",,,", want: true},
		{name: "abc.md", only: "ab", want: true},
	}
	for _, tc := range cases {
		if got := onlyMatches(tc.name, tc.only); got != tc.want {
			t.Errorf("onlyMatches(%q,%q) = %v, want %v", tc.name, tc.only, got, tc.want)
		}
	}
}

func TestEnrich_DryRunDoesNotWrite(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	fp := writeSampleEntry(t, dir, origin.URL)
	original, _ := os.ReadFile(fp) // #nosec G304 -- path is t.TempDir() in tests

	llm := &fakeLLM{resp: EnrichOutput{
		ExpandedDescription: "new body",
		Confidence:          "low",
	}}
	_, err := EnrichPostmortems(context.Background(), enrichOptions{
		Dir:         dir,
		Apply:       false,
		HTTPTimeout: 5 * time.Second,
		LLM:         llm,
		Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	after, _ := os.ReadFile(fp) // #nosec G304 -- path is t.TempDir() in tests
	if string(original) != string(after) {
		t.Errorf("dry-run modified file on disk")
	}
}

func TestEnrich_RespectsExistingFieldsWithoutForce(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	fp := filepath.Join(dir, "22222222-2222-2222-2222-222222222222.md")
	body := `---
uuid: "22222222-2222-2222-2222-222222222222"
url: "` + origin.URL + `"
title: "Hand-curated title"
product: "Hand-curated product"
company: "ExampleCo"

---

Hand-curated description.
`
	if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	llm := &fakeLLM{resp: EnrichOutput{
		Title:               "LLM-suggested title",
		Product:             "LLM-suggested product",
		ExpandedDescription: "LLM-rewritten description.",
		Confidence:          "medium",
	}}
	if _, err := EnrichPostmortems(context.Background(), enrichOptions{
		Dir: dir, Apply: true, HTTPTimeout: 5 * time.Second, LLM: llm, Concurrency: 1,
	}); err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	pm := readPM(t, fp)
	if pm.Title != "Hand-curated title" {
		t.Errorf("Title was overwritten: %q", pm.Title)
	}
	if pm.Product != "Hand-curated product" {
		t.Errorf("Product was overwritten: %q", pm.Product)
	}
	if !strings.Contains(pm.Description, "LLM-rewritten") {
		t.Errorf("Description should always be replaced unless -keep-description, got %q", pm.Description)
	}
}

func TestEnrich_KeepDescription(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	fp := writeSampleEntry(t, dir, origin.URL)

	llm := &fakeLLM{resp: EnrichOutput{
		ExpandedDescription: "Should-not-be-written body.",
		Confidence:          "medium",
	}}
	if _, err := EnrichPostmortems(context.Background(), enrichOptions{
		Dir: dir, Apply: true, KeepDescription: true,
		HTTPTimeout: 5 * time.Second, LLM: llm, Concurrency: 1,
	}); err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	pm := readPM(t, fp)
	if !strings.Contains(pm.Description, "short blurb") {
		t.Errorf("Description was rewritten despite -keep-description: %q", pm.Description)
	}
	if pm.Summary != "" {
		t.Errorf("Summary should remain empty when -keep-description, got %q", pm.Summary)
	}
}

func TestEnrich_MaxAgeSkipsFresh(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	fp := filepath.Join(dir, "33333333-3333-3333-3333-333333333333.md")
	body := `---
uuid: "33333333-3333-3333-3333-333333333333"
url: "` + origin.URL + `"
source_fetched_at: ` + time.Now().UTC().Format(time.RFC3339) + `
company: "ExampleCo"

---

Recently-enriched body.
`
	if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	llm := &fakeLLM{}
	res, err := EnrichPostmortems(context.Background(), enrichOptions{
		Dir: dir, Apply: true, MaxAge: time.Hour,
		HTTPTimeout: 5 * time.Second, LLM: llm, Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if len(res) != 1 || res[0].Skipped != "fresh" {
		t.Errorf("expected skipped=fresh, got %+v", res)
	}
	if llm.last.URL != "" {
		t.Errorf("LLM was called for a fresh entry")
	}
}

func TestEnrich_OnlyFiltersByUUID(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	for _, id := range []string{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"} {
		body := `---
uuid: "` + id + `"
url: "` + origin.URL + `"
company: "ExampleCo"

---

Body for ` + id + `.
`
		if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", id, err)
		}
	}

	llm := &fakeLLM{resp: EnrichOutput{ExpandedDescription: "x", Confidence: "low"}}
	res, err := EnrichPostmortems(context.Background(), enrichOptions{
		Dir: dir, Apply: false, Only: "aaaaaaaa",
		HTTPTimeout: 5 * time.Second, LLM: llm, Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if !strings.HasPrefix(filepath.Base(res[0].Path), "aaaaaaaa") {
		t.Errorf("filtered to wrong file: %s", res[0].Path)
	}
}

// TestEnrich_PrefersArchiveSnapshotNearPublishedAt verifies that a
// publication date discovered from page metadata triggers a refined
// Wayback lookup, and the date-targeted snapshot beats the recent one.
func TestEnrich_PrefersArchiveSnapshotNearPublishedAt(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	publishedAt := time.Date(2017, 3, 1, 0, 0, 0, 0, time.UTC) // matches sampleHTML

	dir := t.TempDir()
	fp := writeSampleEntry(t, dir, origin.URL)

	fetcher := fetcherWithMockedWayback(t, func(target, ts string) string {
		if target != origin.URL {
			return ""
		}
		if ts == publishedAt.Format("20060102") {
			return "http://web.archive.org/web/20170302000000/" + origin.URL
		}
		return "http://web.archive.org/web/20260101000000/" + origin.URL
	})

	llm := &fakeLLM{resp: EnrichOutput{
		ExpandedDescription: "An incident description that is clearly not junk.",
		Confidence:          "high",
	}}
	opts := enrichOptions{
		Dir: dir, Apply: true, HTTPTimeout: 5 * time.Second,
		LLM: llm, Concurrency: 1,
	}
	res := enrichOne(context.Background(), fetcher, opts, fp)
	if res.Err != nil {
		t.Fatalf("enrichOne: %v", res.Err)
	}

	pm := readPM(t, fp)
	want := "https://web.archive.org/web/20170302000000if_/" + origin.URL
	if pm.ArchiveURL != want {
		t.Errorf("pm.ArchiveURL = %q, want date-targeted %q", pm.ArchiveURL, want)
	}
}

// TestEnrich_UsesExistingPublishedAtForArchiveLookup verifies that an
// existing source_published_at seeds the initial Wayback lookup
// (rather than relying on post-extraction refinement). The mock
// asserts every availability query carries the expected timestamp.
func TestEnrich_UsesExistingPublishedAtForArchiveLookup(t *testing.T) {
	t.Parallel()

	// Strip published_time so the date can only come from frontmatter.
	html := strings.Replace(
		sampleHTML,
		`<meta property="article:published_time" content="2017-03-01T00:00:00Z">`,
		"", 1,
	)
	origin := httptest.NewServer(originHandler(html))
	t.Cleanup(origin.Close)

	publishedAt := time.Date(2018, 8, 5, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	fp := filepath.Join(dir, "55555555-5555-5555-5555-555555555555.md")
	body := `---
uuid: "55555555-5555-5555-5555-555555555555"
url: "` + origin.URL + `"
company: "ExampleCo"
source_published_at: ` + publishedAt.Format(time.RFC3339) + `

---

A short blurb.
`
	if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	wantTS := publishedAt.Format("20060102")
	fetcher := fetcherWithMockedWayback(t, func(target, ts string) string {
		if target != origin.URL {
			return ""
		}
		if ts != wantTS {
			t.Errorf("availability lookup ts = %q, want %q", ts, wantTS)
			return ""
		}
		return "http://web.archive.org/web/20180806000000/" + origin.URL
	})

	llm := &fakeLLM{resp: EnrichOutput{
		ExpandedDescription: "An incident description that is clearly not junk.",
		Confidence:          "high",
	}}
	opts := enrichOptions{
		Dir: dir, Apply: true, HTTPTimeout: 5 * time.Second,
		LLM: llm, Concurrency: 1,
	}
	res := enrichOne(context.Background(), fetcher, opts, fp)
	if res.Err != nil {
		t.Fatalf("enrichOne: %v", res.Err)
	}

	pm := readPM(t, fp)
	want := "https://web.archive.org/web/20180806000000if_/" + origin.URL
	if pm.ArchiveURL != want {
		t.Errorf("pm.ArchiveURL = %q, want %q", pm.ArchiveURL, want)
	}
}

func TestEnrich_RewritesWaybackURL(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()
	uuid := "44444444-4444-4444-4444-444444444444"
	wayback := "https://web.archive.org/web/20220101000000/" + origin.URL + "/post"
	body := `---
uuid: "` + uuid + `"
url: "` + wayback + `"
company: "ExampleCo"

---

A short blurb.
`
	fp := filepath.Join(dir, uuid+".md")
	if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	llm := &fakeLLM{resp: EnrichOutput{Confidence: "low"}}
	res, err := EnrichPostmortems(context.Background(), enrichOptions{
		Dir: dir, Apply: true, HTTPTimeout: 5 * time.Second, LLM: llm, Concurrency: 1,
	})
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if len(res) != 1 || res[0].Err != nil {
		t.Fatalf("res = %+v", res)
	}

	pm := readPM(t, fp)
	wantOrigin := origin.URL + "/post"
	if pm.URL != wantOrigin {
		t.Errorf("pm.URL = %q, want %q", pm.URL, wantOrigin)
	}
	wantArchive := "https://web.archive.org/web/20220101000000if_/" + origin.URL + "/post"
	if pm.ArchiveURL != wantArchive {
		t.Errorf("pm.ArchiveURL = %q, want %q", pm.ArchiveURL, wantArchive)
	}
	gotChanged := strings.Join(res[0].Changed, ",")
	if !strings.Contains(gotChanged, "url") || !strings.Contains(gotChanged, "archive_url") {
		t.Errorf("expected url and archive_url in changed list: %s", gotChanged)
	}
}

func TestIsBadTitle(t *testing.T) {
	t.Parallel()
	bad := []string{
		"", "  ", "Heroku Status", "AWS Status", "Cloudflare Status",
		"Wayback Machine", "Internet Archive", "Just a moment...",
		"Attention Required! | Cloudflare", "Access denied",
		"404", "Page not found", "Untitled Document",
		"PagerDuty Status Page", "Stripe Status Page",
		"Redirecting...", "Redirecting", "Redirecting…", "Redirect",
		"Help Center Closed", "Help Center",
		"Loading...", "Loading",
		"Reddit - Please wait for verification",
		"Please wait while we verify your browser",
		"Updates on the status of Stripe services",
		"403 Forbidden",
	}
	for _, s := range bad {
		if !isBadTitle(s) {
			t.Errorf("isBadTitle(%q) = false, want true", s)
		}
	}
	good := []string{
		"AWS S3 us-east-1 outage of February 2017",
		"GitHub Pages downtime, Sept 2018",
		"Status of GitLab during the great migration",
		"How we recovered from a status page outage",
		"Discord Connectivity Issues (March 2017)",
	}
	for _, s := range good {
		if isBadTitle(s) {
			t.Errorf("isBadTitle(%q) = true, want false", s)
		}
	}
}

func TestLooksLikeJunkDescription(t *testing.T) {
	t.Parallel()
	junk := []string{
		"The provided article text is the Dropbox Tech Blog homepage, not the specific postmortem.",
		"The provided text describes the mission of the Archive Team.",
		"The article does not contain any information regarding the incident.",
		"Therefore, details cannot be extracted from this source.",
		"The provided text is too short to generate a meaningful description.",
		"This is a marketing and product overview page for Google Cloud.",
		"The provided article text is a placeholder indicating the page is loading.",
		"The provided article text is a status page for PagerDuty.",
		"The provided article text is in raw PDF format and is not human-readable.",
		"The provided article text is limited to Wayback Machine capture metadata.",
		"Redirecting...",
		"This is a Cloudflare challenge page; we cannot read the content.",
		"The provided article is a comprehensive product page for Google Cloud Observability.",
		"It serves as a marketing and informational resource for Google Cloud's observability suite.",
		"The nature of the underlying failure is not detailed in the provided information.",
		"Information regarding remediation steps is not available from the provided summary.",
		"While specific remediation details are not provided, the incident would have necessitated a rollback.",
		"Specific details about the timeline are not provided in the source.",
	}
	for _, s := range junk {
		if !looksLikeJunkDescription(s) {
			t.Errorf("looksLikeJunkDescription(%q) = false, want true", s)
		}
	}
	real := []string{
		"On March 20, 2017, Discord experienced significant connectivity issues.",
		"The Swedish warship Vasa embarked on its maiden voyage on August 10, 1628.",
		"At 10:25 PM PDT, severe weather caused a utility power loss at a regional substation.",
	}
	for _, s := range real {
		if looksLikeJunkDescription(s) {
			t.Errorf("looksLikeJunkDescription(%q) = true, want false", s)
		}
	}
}

func TestApplyTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		existing, llm, p string
		force            bool
		wantTitle        string
		wantChanged      bool
	}{
		{"replace bad existing with good llm", "Heroku Status", "Heroku Postgres outage", "", false, "Heroku Postgres outage", true},
		{"wipe bad existing when no replacement", "Heroku Status", "", "Wayback Machine", false, "", true},
		{"keep good existing", "Real title", "LLM-suggested", "page-suggested", false, "Real title", false},
		{"force overrides good existing", "Real title", "LLM-suggested", "", true, "LLM-suggested", true},
		{"reject bad llm and bad page when existing empty", "", "Heroku Status", "Wayback Machine", false, "", false},
		{"page fallback when llm bad", "", "Wayback Machine", "Real page title", false, "Real page title", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := applyTitle(tc.existing, tc.llm, tc.p, tc.force, nil)
			if got != tc.wantTitle {
				t.Errorf("title = %q, want %q", got, tc.wantTitle)
			}
			gotChanged := false
			for _, c := range changed {
				if c == "title" {
					gotChanged = true
				}
			}
			if gotChanged != tc.wantChanged {
				t.Errorf("changed=%v, want %v (changed list: %v)", gotChanged, tc.wantChanged, changed)
			}
		})
	}
}

// TestMergeKeywords verifies the keyword union logic without touching
// disk or the network.
func TestMergeKeywords(t *testing.T) {
	t.Parallel()
	got, added := mergeKeywords([]string{"a", "b"}, []string{"B", "c"})
	if !added {
		t.Fatal("expected added=true")
	}
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}

	got, added = mergeKeywords([]string{"a"}, []string{"a"})
	if added {
		t.Errorf("expected no additions for duplicate, got added=true (%v)", got)
	}
}

// TestNewFetcher_DefaultTimeout asserts the zero/negative-input safety
// net so we don't accidentally ship a fetcher with no deadline.
func TestNewFetcher_DefaultTimeout(t *testing.T) {
	t.Parallel()
	f := NewFetcher(0)
	if f.client.Timeout == 0 {
		t.Errorf("expected default timeout > 0")
	}
}
