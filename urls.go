package postmortems

import (
	"net/url"
	"regexp"
	"strings"
)

// waybackSnapshot matches https?://web.archive.org/web/<timestamp>[flags]/<origin>.
// The optional 1-3 char flags suffix ("if_", "id_", "js_", etc.) varies
// the snapshot rendering; we always rewrite to "if_" on the way out.
var waybackSnapshot = regexp.MustCompile(`^https?://web\.archive\.org/web/(\d+)[a-z_]{0,4}/(https?://.+)$`)

// ParseWaybackURL splits a Wayback Machine snapshot URL into its
// (origin, snapshot, true) parts. The snapshot is normalised to https
// and the `if_` flag so a follow-up fetch retrieves the iframe-content
// view without Wayback chrome. Returns ok=false if s isn't a snapshot
// URL.
func ParseWaybackURL(s string) (origin, snapshot string, ok bool) {
	m := waybackSnapshot.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", "", false
	}
	return m[2], "https://web.archive.org/web/" + m[1] + "if_/" + m[2], true
}

// CanonicalURL returns a normalised form of u suitable for equality
// checks against curated postmortem URLs: Wayback snapshots are
// unwrapped to their origin, the scheme is forced to https, the host is
// lowercased with a leading "www." stripped, fragments are dropped, and
// any trailing slash on the path is removed. On parse failure the
// lowercased, trimmed input is returned.
func CanonicalURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if origin, _, ok := ParseWaybackURL(u); ok {
		u = origin
	}

	parsed, err := url.Parse(u)
	if err != nil || !parsed.IsAbs() {
		return strings.ToLower(strings.TrimRight(u, "/"))
	}

	parsed.Scheme = "https"
	parsed.Host = strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

// URLsEquivalent reports whether a and b refer to the same logical
// resource after canonicalisation.
func URLsEquivalent(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return CanonicalURL(a) == CanonicalURL(b)
}

// looksLikeSingleURL reports whether s parses as one absolute URL
// without internal markdown noise like "), [see also](" that the
// danluu/post-mortems regex sometimes captures from multi-link
// entries.
func looksLikeSingleURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, " \t\r\n\")(]") {
		return false
	}
	parsed, err := url.Parse(s)
	if err != nil {
		return false
	}
	return parsed.IsAbs() && parsed.Host != ""
}
