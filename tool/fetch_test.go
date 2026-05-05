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

func originHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}
}

func statusHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }
}

// fetcherWithMockedWayback returns a Fetcher whose CDXURL points at an
// httptest server. lookup gets the request's url+closest params and
// returns one (timestamp, original) row; empty timestamp = no match.
func fetcherWithMockedWayback(t *testing.T, lookup func(target, closest string) (timestamp, original string)) *Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts, orig := lookup(r.URL.Query().Get("url"), r.URL.Query().Get("closest"))
		w.Header().Set("Content-Type", "application/json")
		if ts == "" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		rows := [][]string{{"timestamp", "original"}, {ts, orig}}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	t.Cleanup(srv.Close)
	f := NewFetcher(5 * time.Second)
	f.CDXURL = srv.URL
	return f
}

func TestFetcher_OriginSuccessRecordsArchive(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler("<html>origin body</html>"))
	t.Cleanup(origin.Close)

	f := fetcherWithMockedWayback(t, func(target, _ string) (string, string) {
		if target == origin.URL {
			return "20220101000000", origin.URL
		}
		return "", ""
	})

	res, err := f.Fetch(context.Background(), origin.URL, time.Time{})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.UsedArchive {
		t.Errorf("UsedArchive = true; expected origin success path")
	}
	want := "https://web.archive.org/web/20220101000000if_/" + origin.URL
	if res.ArchiveURL != want {
		t.Errorf("ArchiveURL = %q, want %q", res.ArchiveURL, want)
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

	f := fetcherWithMockedWayback(t, func(target, _ string) (string, string) {
		if target == origin.URL {
			return "20220101000000", origin.URL
		}
		return "", ""
	})
	f.SnapshotPrefix = snap.URL + "/"

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
	if !strings.HasPrefix(res.FinalURL, snap.URL+"/") {
		t.Errorf("FinalURL = %q; want a path under %q", res.FinalURL, snap.URL)
	}
	if res.OriginStatus != http.StatusNotFound {
		t.Errorf("OriginStatus = %d, want 404", res.OriginStatus)
	}
}

func TestFetcher_OriginFailNoArchiveErrors(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(statusHandler(http.StatusNotFound))
	t.Cleanup(origin.Close)

	f := fetcherWithMockedWayback(t, func(_, _ string) (string, string) { return "", "" })

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

	publishedAt := time.Date(2018, 8, 5, 0, 0, 0, 0, time.UTC)

	f := fetcherWithMockedWayback(t, func(target, closest string) (string, string) {
		if target != origin.URL {
			return "", ""
		}
		if closest == publishedAt.UTC().Format("20060102") {
			return "20180806000000", origin.URL
		}
		if closest == "" {
			return "20260101000000", origin.URL
		}
		return "", ""
	})

	res, err := f.Fetch(context.Background(), origin.URL, publishedAt)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	wantDate := "https://web.archive.org/web/20180806000000if_/" + origin.URL
	if res.ArchiveURL != wantDate {
		t.Errorf("ArchiveURL = %q, want date-targeted %q", res.ArchiveURL, wantDate)
	}

	res2, err := f.Fetch(context.Background(), origin.URL, time.Time{})
	if err != nil {
		t.Fatalf("Fetch zero-time: %v", err)
	}
	wantRecent := "https://web.archive.org/web/20260101000000if_/" + origin.URL
	if res2.ArchiveURL != wantRecent {
		t.Errorf("ArchiveURL (zero when) = %q, want recent %q", res2.ArchiveURL, wantRecent)
	}
}

// Pins the statuscode:200 filter so an archived 404 never becomes archive_url.
func TestFetcher_CDXFiltersStatus200(t *testing.T) {
	t.Parallel()

	var sawFilter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawFilter = r.URL.Query().Get("filter")
		_, _ = w.Write([]byte("[]"))
	}))
	t.Cleanup(srv.Close)

	f := NewFetcher(5 * time.Second)
	f.CDXURL = srv.URL
	if _, err := f.archiveLookup(context.Background(), "https://example.com", time.Time{}); err != nil {
		t.Fatalf("archiveLookup: %v", err)
	}
	if sawFilter != "statuscode:200" {
		t.Errorf("filter = %q, want %q", sawFilter, "statuscode:200")
	}
}
