package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const userAgent = "icco-postmortems-enricher/1.0 (+https://postmortems.app)"
const maxBody = 4 * 1024 * 1024 // 4 MiB

// waybackURL matches https?://web.archive.org/web/<timestamp>[flags]/<origin>.
// flags is the optional 1-3 char suffix Wayback uses to vary the
// rendering ("if_", "id_", "js_", etc.).
var waybackURL = regexp.MustCompile(`^https?://web\.archive\.org/web/(\d+)([a-z_]{0,4})/(https?://.+)$`)

// ParseWaybackURL splits a Wayback Machine snapshot URL into its
// (origin, snapshot) parts. snapshot is normalised to https and the
// `if_` flag so the iframe-content view (no Wayback chrome) is fetched.
// Returns ok=false if s isn't a Wayback snapshot URL.
func ParseWaybackURL(s string) (origin, snapshot string, ok bool) {
	s = strings.TrimSpace(s)
	m := waybackURL.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	ts, _, orig := m[1], m[2], m[3]
	return orig, "https://web.archive.org/web/" + ts + "if_/" + orig, true
}

// FetchResult is the outcome of one source fetch. ArchiveURL is set
// whenever Wayback has a snapshot, even on origin success.
type FetchResult struct {
	OriginURL    string
	OriginStatus int
	ArchiveURL   string
	FinalURL     string
	RawHTML      string
	FetchedAt    time.Time
	UsedArchive  bool
}

// Fetcher caches Wayback availability lookups per process so retries
// don't re-hit archive.org. Build one with NewFetcher.
type Fetcher struct {
	client *http.Client

	mu    sync.Mutex
	cache map[string]string // origin URL -> archive snapshot URL ("" = looked up, none available)
}

// NewFetcher returns a Fetcher with the given client timeout (15s if zero).
func NewFetcher(timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Fetcher{
		client: &http.Client{Timeout: timeout},
		cache:  map[string]string{},
	}
}

// Fetch GETs rawURL, falling back to the closest Wayback snapshot when
// the origin fails. ArchiveURL is recorded either way.
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
		return res, fmt.Errorf("origin failed and no wayback snapshot available: %w", originErr)
	}

	html, status, archiveErr := f.get(ctx, archive)
	if archiveErr != nil {
		return res, fmt.Errorf("origin failed and wayback fetch failed: %w", errors.Join(originErr, archiveErr))
	}
	if status < 200 || status >= 300 {
		return res, fmt.Errorf("origin failed and wayback returned status %d: %w", status, originErr)
	}
	res.RawHTML = html
	res.FinalURL = archive
	res.UsedArchive = true
	return res, nil
}

// GetRaw GETs rawURL directly with no Wayback fallback. Useful when
// the caller already knows which URL it wants (e.g. the archive
// snapshot itself) and wants to avoid the availability lookup.
func (f *Fetcher) GetRaw(ctx context.Context, rawURL string) (string, int, error) {
	return f.get(ctx, rawURL)
}

// get GETs rawURL with the standard user-agent and capped body.
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

// archiveLookup returns the closest Wayback snapshot URL ("" if none),
// caching results for the Fetcher's lifetime.
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

	// Wayback returns http:// even for HTTPS-capable snapshots; bump
	// it to skip the redirect on the follow-up GET.
	if strings.HasPrefix(snap, "http://web.archive.org/") {
		snap = "https://" + strings.TrimPrefix(snap, "http://")
	}
	// Rewrite to the iframe-content view so we get the original page
	// HTML instead of the Wayback wrapper (which would set the
	// extracted title to "Wayback Machine").
	if _, ifSnap, ok := ParseWaybackURL(snap); ok {
		snap = ifSnap
	}
	f.cacheStore(target, snap)
	return snap, nil
}

func (f *Fetcher) cacheStore(key, val string) {
	f.mu.Lock()
	f.cache[key] = val
	f.mu.Unlock()
}
