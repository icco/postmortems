package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// originHandler returns a 200 with the supplied body.
func originHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}
}

// statusHandler returns the given status with no body.
func statusHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }
}

// waybackResponse renders the Wayback availability JSON for tests. An
// empty snapshotURL produces an "unavailable" response.
func waybackResponse(snapshotURL string) string {
	if snapshotURL == "" {
		return `{"archived_snapshots":{}}`
	}
	payload := map[string]any{
		"archived_snapshots": map[string]any{
			"closest": map[string]any{
				"available": true,
				"url":       snapshotURL,
				"status":    "200",
			},
		},
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func TestFetcher_OriginSuccessRecordsArchive(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler("<html>origin body</html>"))
	t.Cleanup(origin.Close)

	snap := httptest.NewServer(originHandler("<html>snapshot body</html>"))
	t.Cleanup(snap.Close)

	wayback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(waybackResponse(snap.URL)))
	}))
	t.Cleanup(wayback.Close)

	f := NewFetcher(5 * time.Second)
	// Manually seed the cache with a known result so we don't actually
	// hit archive.org in a unit test.
	f.cacheStore(origin.URL, snap.URL)

	res, err := f.Fetch(context.Background(), origin.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.UsedArchive {
		t.Errorf("UsedArchive = true; expected origin success path")
	}
	if res.ArchiveURL != snap.URL {
		t.Errorf("ArchiveURL = %q, want %q", res.ArchiveURL, snap.URL)
	}
	if !strings.Contains(res.RawHTML, "origin body") {
		t.Errorf("RawHTML = %q, want origin body", res.RawHTML)
	}
	if res.OriginStatus != http.StatusOK {
		t.Errorf("OriginStatus = %d, want 200", res.OriginStatus)
	}
	_ = wayback // unused but documents intent
}

func TestFetcher_OriginFailFallsBackToArchive(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(statusHandler(http.StatusNotFound))
	t.Cleanup(origin.Close)

	snap := httptest.NewServer(originHandler("<html>snapshot body</html>"))
	t.Cleanup(snap.Close)

	f := NewFetcher(5 * time.Second)
	f.cacheStore(origin.URL, snap.URL)

	res, err := f.Fetch(context.Background(), origin.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.UsedArchive {
		t.Errorf("UsedArchive = false; expected fallback path")
	}
	if !strings.Contains(res.RawHTML, "snapshot body") {
		t.Errorf("RawHTML = %q, want snapshot body", res.RawHTML)
	}
	if res.FinalURL != snap.URL {
		t.Errorf("FinalURL = %q, want %q", res.FinalURL, snap.URL)
	}
	if res.OriginStatus != http.StatusNotFound {
		t.Errorf("OriginStatus = %d, want 404", res.OriginStatus)
	}
}

func TestFetcher_OriginFailNoArchiveErrors(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(statusHandler(http.StatusNotFound))
	t.Cleanup(origin.Close)

	f := NewFetcher(5 * time.Second)
	f.cacheStore(origin.URL, "") // simulate "wayback has nothing"

	if _, err := f.Fetch(context.Background(), origin.URL); err == nil {
		t.Fatalf("Fetch: expected error when origin and archive both fail")
	}
}

func TestFetcher_EmptyURL(t *testing.T) {
	t.Parallel()

	f := NewFetcher(time.Second)
	if _, err := f.Fetch(context.Background(), ""); err == nil {
		t.Fatalf("Fetch(\"\"): expected error")
	}
}

func TestParseWaybackURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in           string
		wantOK       bool
		wantOrigin   string
		wantSnapshot string
	}{
		{
			in:           "https://web.archive.org/web/20220124104632/https://example.com/foo",
			wantOK:       true,
			wantOrigin:   "https://example.com/foo",
			wantSnapshot: "https://web.archive.org/web/20220124104632if_/https://example.com/foo",
		},
		{
			in:           "http://web.archive.org/web/20160610080136/https://www.example.org/x?q=1",
			wantOK:       true,
			wantOrigin:   "https://www.example.org/x?q=1",
			wantSnapshot: "https://web.archive.org/web/20160610080136if_/https://www.example.org/x?q=1",
		},
		{
			in:           "https://web.archive.org/web/20220621124002if_/https://example.com/already-iframe",
			wantOK:       true,
			wantOrigin:   "https://example.com/already-iframe",
			wantSnapshot: "https://web.archive.org/web/20220621124002if_/https://example.com/already-iframe",
		},
		{in: "https://example.com/", wantOK: false},
		{in: "", wantOK: false},
	}
	for _, tc := range cases {
		origin, snap, ok := ParseWaybackURL(tc.in)
		if ok != tc.wantOK {
			t.Errorf("ParseWaybackURL(%q) ok=%v, want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if origin != tc.wantOrigin {
			t.Errorf("origin = %q, want %q", origin, tc.wantOrigin)
		}
		if snap != tc.wantSnapshot {
			t.Errorf("snapshot = %q, want %q", snap, tc.wantSnapshot)
		}
	}
}
