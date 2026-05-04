package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// userAgent is sent with every outbound HTTP request from the enrich
// pipeline so site owners can see where the traffic is coming from.
const userAgent = "icco-postmortems-enricher/1.0 (+https://postmortems.app)"

// maxBody caps the bytes we read from any single response. Postmortem
// articles are essays, not multi-megabyte downloads — anything bigger
// is almost certainly a bad redirect or a giant CDN page we can't make
// sense of anyway. The truncated body is still passed downstream so
// best-effort extraction can succeed.
const maxBody = 4 * 1024 * 1024 // 4 MiB

// FetchResult is the outcome of a single source fetch. RawHTML is
// always set when err is nil; ArchiveURL is set when a Wayback snapshot
// exists, regardless of whether origin succeeded. FinalURL records
// where the body actually came from so downstream tools can attribute
// content correctly.
type FetchResult struct {
	OriginURL    string
	OriginStatus int
	ArchiveURL   string
	FinalURL     string
	RawHTML      string
	FetchedAt    time.Time
	UsedArchive  bool
}

// Fetcher pairs an http.Client with a small per-process cache of
// Wayback availability lookups so re-running enrich during the same
// invocation does not hammer archive.org. The zero value is not safe
// to use; build one with NewFetcher.
type Fetcher struct {
	client *http.Client

	mu    sync.Mutex
	cache map[string]string // origin URL -> archive snapshot URL ("" = looked up, none available)
}

// NewFetcher returns a Fetcher whose underlying http.Client times out
// after the supplied duration. A zero or negative timeout falls back
// to 15 seconds, matching the categorize tool's default.
func NewFetcher(timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Fetcher{
		client: &http.Client{Timeout: timeout},
		cache:  map[string]string{},
	}
}

// Fetch retrieves rawURL, falling back to the Wayback Machine when the
// origin returns non-2xx or refuses to talk. Even on origin success the
// closest Wayback snapshot URL is recorded so dead links stay
// retrievable.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (FetchResult, error) {
	res := FetchResult{
		OriginURL: rawURL,
		FetchedAt: time.Now().UTC(),
	}
	if rawURL == "" {
		return res, fmt.Errorf("empty url")
	}

	archive, _ := f.archiveLookup(ctx, rawURL)
	res.ArchiveURL = archive

	html, status, originErr := f.get(ctx, rawURL)
	res.OriginStatus = status
	if originErr == nil {
		res.RawHTML = html
		res.FinalURL = rawURL
		return res, nil
	}

	if archive == "" {
		return res, fmt.Errorf("origin failed (%v) and no wayback snapshot available", originErr)
	}

	html, status, archiveErr := f.get(ctx, archive)
	if archiveErr != nil {
		return res, fmt.Errorf("origin failed (%v); wayback failed (%v)", originErr, archiveErr)
	}
	if status < 200 || status >= 300 {
		return res, fmt.Errorf("origin failed (%v); wayback returned status %d", originErr, status)
	}
	res.RawHTML = html
	res.FinalURL = archive
	res.UsedArchive = true
	return res, nil
}

// get is a thin wrapper around http.Get that enforces the user-agent,
// caps the response body, and returns the response status alongside
// any error. The body is closed before returning.
func (f *Fetcher) get(ctx context.Context, rawURL string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, fmt.Errorf("http %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(body), resp.StatusCode, nil
}

// archiveLookup hits the Wayback availability API and returns the
// closest snapshot URL (empty string if none). Results are cached in
// memory for the lifetime of the Fetcher to avoid re-querying the same
// URL on retries within a single run.
func (f *Fetcher) archiveLookup(ctx context.Context, target string) (string, error) {
	f.mu.Lock()
	if v, ok := f.cache[target]; ok {
		f.mu.Unlock()
		return v, nil
	}
	f.mu.Unlock()

	q := url.Values{"url": []string{target}}
	endpoint := "https://archive.org/wayback/available?" + q.Encode()

	body, status, err := f.get(ctx, endpoint)
	if err != nil {
		f.cacheStore(target, "")
		return "", err
	}
	if status != http.StatusOK {
		f.cacheStore(target, "")
		return "", fmt.Errorf("wayback availability returned %d", status)
	}

	var payload struct {
		ArchivedSnapshots struct {
			Closest struct {
				Available bool   `json:"available"`
				URL       string `json:"url"`
				Status    string `json:"status"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		f.cacheStore(target, "")
		return "", fmt.Errorf("parse wayback response: %w", err)
	}

	snap := payload.ArchivedSnapshots.Closest.URL
	if snap == "" || !payload.ArchivedSnapshots.Closest.Available {
		f.cacheStore(target, "")
		return "", nil
	}

	// Wayback returns http:// even for HTTPS-capable snapshots; bump it
	// up so we don't get a redirect on the follow-up GET.
	if strings.HasPrefix(snap, "http://web.archive.org/") {
		snap = "https://" + strings.TrimPrefix(snap, "http://")
	}
	f.cacheStore(target, snap)
	return snap, nil
}

func (f *Fetcher) cacheStore(key, val string) {
	f.mu.Lock()
	f.cache[key] = val
	f.mu.Unlock()
}
