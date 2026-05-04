package main

import (
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// PageMetadata is the structured information we lift out of a fetched
// HTML body before handing it to the LLM. PlainText is best-effort
// visible text suitable for prompt context; it may be truncated.
type PageMetadata struct {
	Title       string
	Author      string
	PublishedAt time.Time
	PlainText   string
}

// maxPlainTextChars caps the body slice we feed to Gemini. Most flash
// models comfortably handle this much input, and most postmortem write
// ups summarise themselves in the first few thousand characters.
const maxPlainTextChars = 20000

// ExtractMetadata walks the parsed HTML once, capturing the highest
// quality version of each metadata field (OpenGraph beats <meta>,
// JSON-LD beats both for date/author) and accumulating visible text.
// Errors parsing the HTML degrade gracefully — we return whatever we
// managed to collect.
func ExtractMetadata(htmlBody string) PageMetadata {
	out := PageMetadata{}
	if strings.TrimSpace(htmlBody) == "" {
		return out
	}

	doc, err := html.Parse(strings.NewReader(htmlBody))
	if err != nil {
		out.PlainText = truncate(stripTags(htmlBody), maxPlainTextChars)
		return out
	}

	var (
		title       string
		ogTitle     string
		twTitle     string
		author      string
		publishedAt time.Time
	)

	var text strings.Builder
	skipDepth := 0

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript" || n.Data == "template"):
			if n.Data == "script" && attr(n, "type") == "application/ld+json" {
				absorbJSONLD(n, &author, &publishedAt)
			}
			skipDepth++
			defer func() { skipDepth-- }()
		case n.Type == html.ElementNode && n.Data == "title" && title == "":
			title = strings.TrimSpace(textOf(n))
		case n.Type == html.ElementNode && n.Data == "meta":
			absorbMeta(n, &ogTitle, &twTitle, &author, &publishedAt)
		case n.Type == html.ElementNode && n.Data == "time" && publishedAt.IsZero():
			if dt := attr(n, "datetime"); dt != "" {
				if t, ok := tryParseTime(dt); ok {
					publishedAt = t
				}
			}
		case n.Type == html.TextNode && skipDepth == 0:
			s := strings.TrimSpace(n.Data)
			if s != "" {
				text.WriteString(s)
				text.WriteByte(' ')
				if text.Len() > maxPlainTextChars*2 {
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	switch {
	case ogTitle != "":
		out.Title = ogTitle
	case twTitle != "":
		out.Title = twTitle
	default:
		out.Title = title
	}
	out.Author = author
	out.PublishedAt = publishedAt
	out.PlainText = truncate(collapseWhitespace(text.String()), maxPlainTextChars)
	return out
}

// absorbMeta inspects a single <meta> tag and updates the running
// best-known values for title/author/publishedAt. Lower-quality fields
// are not allowed to overwrite higher-quality ones (e.g. og:title wins
// over twitter:title which wins over <title>; we already prioritise
// JSON-LD for author/date elsewhere).
func absorbMeta(n *html.Node, ogTitle, twTitle, author *string, publishedAt *time.Time) {
	property := strings.ToLower(attr(n, "property"))
	name := strings.ToLower(attr(n, "name"))
	content := strings.TrimSpace(attr(n, "content"))
	if content == "" {
		return
	}
	switch {
	case property == "og:title" && *ogTitle == "":
		*ogTitle = content
	case (name == "twitter:title" || property == "twitter:title") && *twTitle == "":
		*twTitle = content
	case (name == "author" || property == "article:author" || property == "book:author") && *author == "":
		*author = content
	case (property == "article:published_time" || name == "article:published_time" ||
		property == "datepublished" || name == "datepublished" ||
		name == "pubdate" || name == "publish-date" || name == "date") && publishedAt.IsZero():
		if t, ok := tryParseTime(content); ok {
			*publishedAt = t
		}
	}
}

// absorbJSONLD scans a <script type="application/ld+json"> block for
// schema.org BlogPosting / NewsArticle / Article style metadata. Many
// CMSes (WordPress, Ghost, Medium) emit this and it's the most reliable
// source for `datePublished` and structured author data.
func absorbJSONLD(n *html.Node, author *string, publishedAt *time.Time) {
	raw := strings.TrimSpace(textOf(n))
	if raw == "" {
		return
	}
	var any interface{}
	if err := json.Unmarshal([]byte(raw), &any); err != nil {
		return
	}
	walkJSONLD(any, author, publishedAt)
}

// walkJSONLD recursively descends arbitrary JSON-LD structures (some
// pages emit a list, some a graph, some a single object) and pulls out
// `datePublished` / `author` whenever they appear.
func walkJSONLD(v interface{}, author *string, publishedAt *time.Time) {
	switch x := v.(type) {
	case map[string]interface{}:
		if publishedAt.IsZero() {
			if s, ok := x["datePublished"].(string); ok {
				if t, ok := tryParseTime(s); ok {
					*publishedAt = t
				}
			}
		}
		if *author == "" {
			switch a := x["author"].(type) {
			case string:
				*author = a
			case map[string]interface{}:
				if name, ok := a["name"].(string); ok {
					*author = name
				}
			case []interface{}:
				for _, item := range a {
					if m, ok := item.(map[string]interface{}); ok {
						if name, ok := m["name"].(string); ok {
							*author = name
							break
						}
					}
					if s, ok := item.(string); ok {
						*author = s
						break
					}
				}
			}
		}
		if g, ok := x["@graph"]; ok {
			walkJSONLD(g, author, publishedAt)
		}
	case []interface{}:
		for _, item := range x {
			walkJSONLD(item, author, publishedAt)
		}
	}
}

// attr returns the value of attribute key on n, or "" if absent.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// textOf concatenates all descendant text of n verbatim. Used for
// elements (e.g. <title>, JSON-LD scripts) where we want raw content
// rather than skipping over them.
func textOf(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
		for d := c.FirstChild; d != nil; d = d.NextSibling {
			walk(d)
		}
	}
	walk(n)
	return sb.String()
}

// tryParseTime walks a small set of common date layouts (ISO 8601,
// RFC1123, plain dates, slash-separated, and a few CMS quirks) and
// returns the parsed time on the first match. Returns false when none
// of the layouts apply.
func tryParseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"02 Jan 2006",
		"January 2, 2006",
		"Jan 2, 2006",
		"01/02/2006",
		"2006/01/02",
		time.RFC1123,
		time.RFC1123Z,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// stripTags is a fallback used when html.Parse fails outright. It
// removes anything between '<' and '>' and collapses whitespace; not
// pretty, but enough to keep the LLM call from receiving raw HTML.
func stripTags(s string) string {
	var sb strings.Builder
	skip := false
	for _, r := range s {
		switch r {
		case '<':
			skip = true
		case '>':
			skip = false
		default:
			if !skip {
				sb.WriteRune(r)
			}
		}
	}
	return collapseWhitespace(sb.String())
}

// collapseWhitespace replaces any run of whitespace (including
// newlines) with a single space. Cheaper than a regex and produces the
// same shape of output for our purposes.
func collapseWhitespace(s string) string {
	var sb strings.Builder
	prevSpace := true
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			if !prevSpace {
				sb.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		sb.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(sb.String())
}

// truncate returns s shortened to at most n runes, suffixing an
// ellipsis when truncation actually happens so the LLM can tell.
func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
