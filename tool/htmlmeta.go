package main

import (
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// PageMetadata is what we extract from an HTML body before handing it
// to the LLM. PlainText may be truncated.
type PageMetadata struct {
	Title       string
	Author      string
	PublishedAt time.Time
	PlainText   string
}

const maxPlainTextChars = 20000

// ExtractMetadata pulls title/author/published_at and visible text from
// htmlBody. OpenGraph beats <meta> beats <title>; JSON-LD wins for
// author/date. Returns partial results on parse error.
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

// absorbMeta updates running best values from a single <meta> tag.
// Higher-quality sources (og:title > twitter:title > <title>) win.
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

// absorbJSONLD scans a JSON-LD block for schema.org datePublished /
// author. Most CMSes emit this and it beats meta tags.
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

// walkJSONLD recurses through JSON-LD (object, list, or @graph) and
// fills author/publishedAt on first hit.
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

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// textOf concatenates n's descendant text verbatim.
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

// tryParseTime tries a handful of common layouts (ISO 8601, RFC1123,
// plain/slash dates) and returns the first match.
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

// stripTags is a regex-free fallback for when html.Parse fails.
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

// collapseWhitespace replaces runs of whitespace with a single space.
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

// truncate clips s to n bytes with an ellipsis when truncated.
func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
