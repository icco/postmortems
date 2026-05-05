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

type availabilityResponse struct {
	ArchivedSnapshots struct {
		Closest struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
			Status    string `json:"status"`
		} `json:"closest"`
	} `json:"archived_snapshots"`
}

// fetcherWithMockedWayback returns a Fetcher pointed at a fresh
// httptest stand-in for the wayback availability endpoint. lookup is
// called with the request's url+timestamp query params; a non-empty
// return is encoded as {available:true,url:...}, "" as {available:false}.
func fetcherWithMockedWayback(t *testing.T, lookup func(target, timestamp string) string) *Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := lookup(r.URL.Query().Get("url"), r.URL.Query().Get("timestamp"))
		var resp availabilityResponse
		if snap != "" {
			resp.ArchivedSnapshots.Closest.Available = true
			resp.ArchivedSnapshots.Closest.URL = snap
			resp.ArchivedSnapshots.Closest.Status = "200"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	f := NewFetcher(5 * time.Second)
	f.AvailabilityURL = srv.URL
	return f
}

func TestFetcher_OriginSuccessRecordsArchive(t *testing.T) {
	t.Parallel()

	origin := httptest.NewServer(originHandler("<html>origin body</html>"))
	t.Cleanup(origin.Close)

	wantSnap := "http://web.archive.org/web/20220101000000/" + origin.URL
	f := fetcherWithMockedWayback(t, func(target, _ string) string {
		if target == origin.URL {
			return wantSnap
		}
		return ""
	})

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
	if !strings.HasPrefix(res.ArchiveURL, "https://web.archive.org/") {
		t.Errorf("ArchiveURL = %q; expected http→https rewrite", res.ArchiveURL)
	}
	if !strings.Contains(res.ArchiveURL, "if_/") {
		t.Errorf("ArchiveURL = %q; expected iframe-content rewrite (if_/)", res.ArchiveURL)
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

	// snap.URL doesn't match ParseWaybackURL, so the https/if_
	// rewrites are no-ops and the follow-up GET hits snap directly.
	f := fetcherWithMockedWayback(t, func(target, _ string) string {
		if target == origin.URL {
			return snap.URL
		}
		return ""
	})

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

	f := fetcherWithMockedWayback(t, func(_, _ string) string { return "" })

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
	dateSnap := "http://web.archive.org/web/20180806000000/" + origin.URL
	recentSnap := "http://web.archive.org/web/20260101000000/" + origin.URL

	f := fetcherWithMockedWayback(t, func(target, ts string) string {
		if target != origin.URL {
			return ""
		}
		if ts == publishedAt.Format("20060102") {
			return dateSnap
		}
		if ts == "" {
			return recentSnap
		}
		return ""
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
