package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/icco/postmortems"
)

const userAgent = "icco-postmortems-enricher/1.0 (+https://postmortems.app)"
const maxBody = 4 * 1024 * 1024 // 4 MiB

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

const defaultAvailabilityURL = "https://archive.org/wayback/available"

// Fetcher issues source GETs and Wayback availability lookups.
type Fetcher struct {
	client *http.Client

	// AvailabilityURL overrides the Wayback availability endpoint
	// (used by tests). Empty means defaultAvailabilityURL.
	AvailabilityURL string
}

// NewFetcher returns a Fetcher with the given client timeout (15s if zero).
func NewFetcher(timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Fetcher{
		client:          &http.Client{Timeout: timeout},
		AvailabilityURL: defaultAvailabilityURL,
	}
}

// Fetch GETs rawURL, falling back to the closest Wayback snapshot when
// the origin fails. ArchiveURL is recorded either way; when targets the
// Wayback lookup at a date (zero = most recent snapshot).
func (f *Fetcher) Fetch(ctx context.Context, rawURL string, when time.Time) (FetchResult, error) {
	res := FetchResult{
		OriginURL: rawURL,
		FetchedAt: time.Now().UTC(),
	}
	if rawURL == "" {
		return res, fmt.Errorf("empty url")
	}

	archive, _ := f.archiveLookup(ctx, rawURL, when)
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

// ArchiveSnapshot returns the Wayback snapshot URL closest to when, or
// "" if none. Used by the enricher to refine ArchiveURL after a
// publication date is discovered from the page.
func (f *Fetcher) ArchiveSnapshot(ctx context.Context, target string, when time.Time) (string, error) {
	return f.archiveLookup(ctx, target, when)
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

// archiveLookup returns the Wayback snapshot URL closest to when, or
// "" if none. Zero when = most recent.
func (f *Fetcher) archiveLookup(ctx context.Context, target string, when time.Time) (string, error) {
	endpoint := f.AvailabilityURL
	if endpoint == "" {
		endpoint = defaultAvailabilityURL
	}
	q := url.Values{"url": []string{target}}
	if !when.IsZero() {
		q.Set("timestamp", when.UTC().Format("20060102"))
	}
	endpoint += "?" + q.Encode()

	body, status, err := f.get(ctx, endpoint)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
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
		return "", fmt.Errorf("parse wayback response: %w", err)
	}

	snap := payload.ArchivedSnapshots.Closest.URL
	if snap == "" || !payload.ArchivedSnapshots.Closest.Available {
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
	if _, ifSnap, ok := postmortems.ParseWaybackURL(snap); ok {
		snap = ifSnap
	}
	return snap, nil
}
