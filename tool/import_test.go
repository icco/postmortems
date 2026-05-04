package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunImport_AddsAndEnriches verifies the happy path: a brand-new
// upstream URL is saved and then enriched in one shot. Existing files
// matched by canonical URL are skipped without re-enrichment.
func TestRunImport_AddsAndEnriches(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler(sampleHTML))
	t.Cleanup(origin.Close)

	dir := t.TempDir()

	// Seed an existing entry so we can prove the import doesn't
	// re-touch it.
	keep := filepath.Join(dir, "11111111-1111-1111-1111-111111111111.md")
	body := `---
uuid: "11111111-1111-1111-1111-111111111111"
url: "https://existing.example/incident"
title: "Hand-curated"
categories:
- postmortem
company: "ExampleCo"

---

Hand-curated body.
`
	if err := os.WriteFile(keep, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	originalKeep, _ := os.ReadFile(keep) // #nosec G304 -- path is t.TempDir() in tests

	source := filepath.Join(t.TempDir(), "upstream.md")
	upstream := strings.Join([]string{
		// already present (matches under canonicalisation)
		"[ExampleCo](http://www.existing.example/incident/). Existing blurb.",
		// brand new entry served by the test origin
		"[NewCo](" + origin.URL + "). Brand new blurb.",
	}, "\n")
	if err := os.WriteFile(source, []byte(upstream), 0o600); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	llm := &fakeLLM{resp: EnrichOutput{
		Title:               "Brand new outage",
		ExpandedDescription: "Long description body.",
		Confidence:          "high",
	}}

	res, err := RunImport(context.Background(), importOptions{
		Dir:    dir,
		Source: source,
		Enrich: enrichOptions{
			HTTPTimeout: 5 * time.Second,
			Concurrency: 1,
			LLM:         llm,
		},
	})
	if err != nil {
		t.Fatalf("RunImport: %v", err)
	}
	if got := len(res.Added); got != 1 {
		t.Fatalf("Added = %d, want 1", got)
	}
	if res.SkippedExisting != 1 {
		t.Errorf("SkippedExisting = %d, want 1", res.SkippedExisting)
	}
	if got := len(res.Enriched); got != 1 {
		t.Errorf("enriched = %d, want 1", got)
	}
	if res.EnrichSkipped != "" {
		t.Errorf("EnrichSkipped = %q, want empty", res.EnrichSkipped)
	}

	// Existing file untouched.
	after, _ := os.ReadFile(keep) // #nosec G304 -- path is t.TempDir() in tests
	if string(originalKeep) != string(after) {
		t.Errorf("existing entry was modified by import")
	}

	// New file got enriched.
	pm := readPM(t, filepath.Join(dir, res.Added[0].UUID+".md"))
	if pm.Title != "Brand new outage" {
		t.Errorf("Title = %q, want enriched", pm.Title)
	}
	if !strings.Contains(pm.Description, "Long description") {
		t.Errorf("Description = %q, want enriched", pm.Description)
	}
	if pm.SourceFetchedAt.IsZero() {
		t.Errorf("SourceFetchedAt is zero; enrich didn't run")
	}
	if pm.Summary == "" {
		t.Errorf("Summary should preserve original blurb")
	}
}

// TestRunImport_NoNewEntries_SkipsEnrich proves that running import
// against a source whose entries already exist locally does no LLM
// work — the property that makes "run constantly" cheap.
func TestRunImport_NoNewEntries_SkipsEnrich(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := `---
uuid: "22222222-2222-2222-2222-222222222222"
url: "https://existing.example/incident"
categories:
- postmortem
company: "ExampleCo"

---

Body.
`
	if err := os.WriteFile(filepath.Join(dir, "22222222-2222-2222-2222-222222222222.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	source := filepath.Join(t.TempDir(), "upstream.md")
	if err := os.WriteFile(source, []byte("[ExampleCo](https://existing.example/incident). Same as existing.\n"), 0o600); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	llm := &fakeLLM{}
	res, err := RunImport(context.Background(), importOptions{
		Dir:    dir,
		Source: source,
		Enrich: enrichOptions{
			HTTPTimeout: 5 * time.Second,
			Concurrency: 1,
			LLM:         llm,
		},
	})
	if err != nil {
		t.Fatalf("RunImport: %v", err)
	}
	if len(res.Added) != 0 {
		t.Errorf("Added = %d, want 0", len(res.Added))
	}
	if res.EnrichSkipped != "no new entries" {
		t.Errorf("EnrichSkipped = %q, want \"no new entries\"", res.EnrichSkipped)
	}
}

// TestRunImport_NoLLMSkipsEnrich verifies the action degrades
// gracefully when no LLM client is provided (e.g. running in CI without
// GCP credentials): the new entry is still saved, just not enriched.
func TestRunImport_NoLLMSkipsEnrich(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(t.TempDir(), "upstream.md")
	if err := os.WriteFile(source, []byte("[NewCo](https://newco.example/x). Blurb.\n"), 0o600); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	res, err := RunImport(context.Background(), importOptions{
		Dir:    dir,
		Source: source,
	})
	if err != nil {
		t.Fatalf("RunImport: %v", err)
	}
	if got := len(res.Added); got != 1 {
		t.Errorf("Added = %d, want 1", got)
	}
	if res.EnrichSkipped == "" {
		t.Errorf("EnrichSkipped should be set when no LLM is available")
	}
}
