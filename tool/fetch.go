package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
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

const (
	defaultCDXURL         = "https://web.archive.org/cdx/search/cdx"
	defaultSnapshotPrefix = "https://web.archive.org/web/"
)

// Fetcher issues source GETs and Wayback CDX lookups.
type Fetcher struct {
	client *http.Client

	// CDXURL and SnapshotPrefix are test seams; empty = production defaults.
	CDXURL         string
	SnapshotPrefix string

	// Logger receives best-effort failures. Nil = slog.Default.
	Logger *slog.Logger
}

func (f *Fetcher) logger() *slog.Logger {
	if f.Logger != nil {
		return f.Logger
	}
	return slog.Default()
}

// NewFetcher returns a Fetcher with the given client timeout (15s if zero).
func NewFetcher(timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Fetcher{
		client: &http.Client{Timeout: timeout},
		CDXURL: defaultCDXURL,
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

	archive, lookupErr := f.archiveLookup(ctx, rawURL, when)
	if lookupErr != nil {
		f.logger().Debug("wayback cdx lookup failed",
			"url", rawURL, "when", when, "err", lookupErr)
	}
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

// archiveLookup returns the closest 200-status Wayback snapshot URL
// for target, or "" if none. Zero when = most recent.
func (f *Fetcher) archiveLookup(ctx context.Context, target string, when time.Time) (string, error) {
	endpoint := f.CDXURL
	if endpoint == "" {
		endpoint = defaultCDXURL
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse cdx endpoint: %w", err)
	}
	q := url.Values{
		"url":    []string{target},
		"output": []string{"json"},
		"fl":     []string{"timestamp,original"},
		"filter": []string{"statuscode:200"},
		"limit":  []string{"1"},
	}
	if when.IsZero() {
		q.Set("limit", "-1")
		q.Set("fastLatest", "true")
	} else {
		q.Set("closest", when.UTC().Format("20060102"))
	}
	u.RawQuery = q.Encode()

	body, status, err := f.get(ctx, u.String())
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("cdx returned %d", status)
	}

	// CDX json: [[headers], [row], ...]. Empty match returns [] or just headers.
	var rows [][]string
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return "", fmt.Errorf("parse cdx response: %w", err)
	}
	if len(rows) < 2 || len(rows[1]) < 2 {
		return "", nil
	}

	timestamp, original := rows[1][0], rows[1][1]
	if _, err := strconv.ParseInt(timestamp, 10, 64); err != nil {
		return "", fmt.Errorf("cdx returned non-numeric timestamp %q", timestamp)
	}
	prefix := f.SnapshotPrefix
	if prefix == "" {
		prefix = defaultSnapshotPrefix
	}
	// if_/ selects the iframe view so we get the original page, not the wrapper.
	return prefix + timestamp + "if_/" + original, nil
}
