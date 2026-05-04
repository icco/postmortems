package main

import (
	"strings"
	"testing"
	"time"
)

func TestExtractMetadata_OpenGraph(t *testing.T) {
	t.Parallel()
	body := `<!doctype html>
<html><head>
<title>Boring fallback</title>
<meta property="og:title" content="AWS S3 outage of February 2017">
<meta property="article:published_time" content="2017-03-01T12:00:00Z">
</head><body>
<p>Some text about <strong>S3</strong> failing.</p>
<script>alert("nope")</script>
<style>body{}</style>
</body></html>`
	got := ExtractMetadata(body)
	if got.Title != "AWS S3 outage of February 2017" {
		t.Errorf("Title = %q, want OpenGraph title", got.Title)
	}
	want := time.Date(2017, 3, 1, 12, 0, 0, 0, time.UTC)
	if !got.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v, want %v", got.PublishedAt, want)
	}
	if got.PlainText == "" || !strings.Contains(got.PlainText, "S3") {
		t.Errorf("PlainText missing expected content: %q", got.PlainText)
	}
	if strings.Contains(got.PlainText, "alert") || strings.Contains(got.PlainText, "body{}") {
		t.Errorf("PlainText should strip script/style content, got %q", got.PlainText)
	}
}

func TestExtractMetadata_JSONLDFallback(t *testing.T) {
	t.Parallel()
	body := `<!doctype html><html><head>
<title>Plain title</title>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"BlogPosting","headline":"Outage post-mortem","datePublished":"2020-08-15T08:30:00Z"}
</script>
</head><body><p>Body text.</p></body></html>`
	got := ExtractMetadata(body)
	if got.Title != "Plain title" {
		t.Errorf("Title = %q, want plain <title>", got.Title)
	}
	want := time.Date(2020, 8, 15, 8, 30, 0, 0, time.UTC)
	if !got.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v, want %v", got.PublishedAt, want)
	}
}

func TestExtractMetadata_TimeElement(t *testing.T) {
	t.Parallel()
	body := `<html><head><title>X</title></head>
<body><time datetime="2019-04-12">posted</time> hello</body></html>`
	got := ExtractMetadata(body)
	want := time.Date(2019, 4, 12, 0, 0, 0, 0, time.UTC)
	if !got.PublishedAt.Equal(want) {
		t.Errorf("PublishedAt = %v, want %v", got.PublishedAt, want)
	}
}

func TestExtractMetadata_Empty(t *testing.T) {
	t.Parallel()
	got := ExtractMetadata("")
	if got.Title != "" || !got.PublishedAt.IsZero() || got.PlainText != "" {
		t.Errorf("expected zero-value PageMetadata, got %+v", got)
	}
}

func TestCollapseWhitespace(t *testing.T) {
	t.Parallel()
	if got := collapseWhitespace("  hello\n\nworld\t\t"); got != "hello world" {
		t.Errorf("collapseWhitespace = %q, want %q", got, "hello world")
	}
}

func TestTryParseTime(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"2017-02-28T17:37:00Z":          true,
		"2017-02-28":                    true,
		"February 28, 2017":             true, // matches "January 2, 2006"
		"Feb 28, 2017":                  true,
		"":                              false,
		"not a date":                    false,
		"Tue, 28 Feb 2017 17:37:00 GMT": true,
	}
	for in, wantOK := range cases {
		_, ok := tryParseTime(in)
		if ok != wantOK {
			t.Errorf("tryParseTime(%q) ok=%v, want %v", in, ok, wantOK)
		}
	}
}
