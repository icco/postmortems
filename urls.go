package postmortems

import (
	"net/url"
	"regexp"
	"strings"
)

// waybackSnapshot matches https?://web.archive.org/web/<timestamp>[flags]/<origin>.
// flags is the optional 1-3 char suffix Wayback uses to vary the
// rendering ("if_", "id_", "js_", etc.). Kept in sync with the
// equivalent regex in tool/fetch.go: changes here should be made there
// too so both packages agree on what counts as a snapshot URL.
var waybackSnapshot = regexp.MustCompile(`^https?://web\.archive\.org/web/(\d+)([a-z_]{0,4})/(https?://.+)$`)

// UnwrapWaybackURL returns the origin URL embedded in a Wayback
// snapshot. If s isn't a snapshot URL, it's returned unchanged.
func UnwrapWaybackURL(s string) string {
	s = strings.TrimSpace(s)
	if m := waybackSnapshot.FindStringSubmatch(s); m != nil {
		return m[3]
	}
	return s
}

// unwrapWayback splits a Wayback snapshot URL into (origin, snapshot,
// true). The snapshot is normalised to https + the `if_` flag so a
// follow-up fetch retrieves the iframe-content view without Wayback
// chrome. Returns ok=false if s isn't a snapshot URL.
func unwrapWayback(s string) (origin, snapshot string, ok bool) {
	s = strings.TrimSpace(s)
	m := waybackSnapshot.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	ts, orig := m[1], m[3]
	return orig, "https://web.archive.org/web/" + ts + "if_/" + orig, true
}

// CanonicalURL returns a normalised form of u suitable for equality
// checks against curated postmortem URLs. The output is not meant to be
// fetched; it just needs to be stable for the same logical resource.
//
// Normalisations applied (in order):
//   - trim surrounding whitespace
//   - unwrap a Wayback snapshot to its origin
//   - lowercase scheme and host, drop a leading "www." on the host
//   - drop the URL fragment
//   - force scheme to "https" so http/https variants compare equal
//   - drop a trailing slash on the path
//
// On parse failure the lowercased, trimmed input is returned.
func CanonicalURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	u = UnwrapWaybackURL(u)

	parsed, err := url.Parse(u)
	if err != nil || !parsed.IsAbs() {
		return strings.ToLower(strings.TrimRight(u, "/"))
	}

	parsed.Scheme = "https"
	host := strings.ToLower(parsed.Host)
	host = strings.TrimPrefix(host, "www.")
	parsed.Host = host
	parsed.Fragment = ""

	out := parsed.String()
	if strings.HasSuffix(out, "/") && parsed.Path != "/" {
		out = strings.TrimRight(out, "/")
	}
	if out == parsed.Scheme+"://"+host+"/" {
		out = parsed.Scheme + "://" + host
	}
	return out
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
