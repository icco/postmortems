package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func originHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}
}

func statusHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }
}

func TestFetcher_OriginSuccessRecordsArchive(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler("<html>origin body</html>"))
	t.Cleanup(origin.Close)

	f := NewFetcher(5 * time.Second)
	// Pre-seed the Wayback cache so the test never actually hits
	// archive.org.
	f.cacheStore(origin.URL, "https://web.archive.org/web/20220101000000if_/"+origin.URL)

	res, err := f.Fetch(context.Background(), origin.URL, time.Time{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.UsedArchive {
		t.Errorf("UsedArchive = true; expected origin success path")
	}
	if res.ArchiveURL == "" {
		t.Errorf("ArchiveURL not recorded on origin-success path")
	}
	if !strings.Contains(res.RawHTML, "origin body") {
		t.Errorf("RawHTML = %q, want origin body", res.RawHTML)
	}
	if res.OriginStatus != http.StatusOK {
		t.Errorf("OriginStatus = %d, want 200", res.OriginStatus)
	}
}

func TestFetcher_OriginFailFallsBackToArchive(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(statusHandler(http.StatusNotFound))
	t.Cleanup(origin.Close)

	snap := httptest.NewServer(originHandler("<html>snapshot body</html>"))
	t.Cleanup(snap.Close)

	f := NewFetcher(5 * time.Second)
	f.cacheStore(origin.URL, snap.URL)

	res, err := f.Fetch(context.Background(), origin.URL, time.Time{})
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

	if _, err := f.Fetch(context.Background(), origin.URL, time.Time{}); err == nil {
		t.Fatalf("Fetch: expected error when origin and archive both fail")
	}
}

func TestFetcher_EmptyURL(t *testing.T) {
	t.Parallel()

	f := NewFetcher(time.Second)
	if _, err := f.Fetch(context.Background(), "", time.Time{}); err == nil {
		t.Fatalf("Fetch(\"\"): expected error")
	}
}

func TestFetcher_DateTargetedSnapshotIsPreferred(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler("<html>origin body</html>"))
	t.Cleanup(origin.Close)

	f := NewFetcher(5 * time.Second)
	publishedAt := time.Date(2018, 8, 5, 0, 0, 0, 0, time.UTC)
	dateSnap := "https://web.archive.org/web/20180806000000if_/" + origin.URL
	recentSnap := "https://web.archive.org/web/20260101000000if_/" + origin.URL
	// Pre-seed both cache slots so the date-targeted lookup returns a
	// different (older) snapshot than the recent one. Requesting near
	// the publication date should pick the older snapshot.
	f.cacheStore(archiveCacheKey(origin.URL, publishedAt), dateSnap)
	f.cacheStore(archiveCacheKey(origin.URL, time.Time{}), recentSnap)

	res, err := f.Fetch(context.Background(), origin.URL, publishedAt)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.ArchiveURL != dateSnap {
		t.Errorf("ArchiveURL = %q, want date-targeted %q", res.ArchiveURL, dateSnap)
	}

	// Zero-time call returns the recent snapshot from the original cache slot.
	res2, err := f.Fetch(context.Background(), origin.URL, time.Time{})
	if err != nil {
		t.Fatalf("Fetch zero-time: %v", err)
	}
	if res2.ArchiveURL != recentSnap {
		t.Errorf("ArchiveURL (zero when) = %q, want recent %q", res2.ArchiveURL, recentSnap)
	}
}
