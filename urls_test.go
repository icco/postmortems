package postmortems

import "testing"

func TestCanonicalURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, in, want string
	}{
		{
			name: "https + trim trailing slash",
			in:   "https://example.com/foo/",
			want: "https://example.com/foo",
		},
		{
			name: "lowercase host + drop www",
			in:   "https://WWW.Example.COM/Path",
			want: "https://example.com/Path",
		},
		{
			name: "http upgraded to https",
			in:   "http://example.com/path",
			want: "https://example.com/path",
		},
		{
			name: "wayback unwrapped",
			in:   "https://web.archive.org/web/20220124104632/https://example.com/foo/",
			want: "https://example.com/foo",
		},
		{
			name: "wayback unwrap with iframe flag",
			in:   "https://web.archive.org/web/20220124104632if_/https://example.com/foo",
			want: "https://example.com/foo",
		},
		{
			name: "fragment dropped",
			in:   "https://example.com/foo#section",
			want: "https://example.com/foo",
		},
		{
			name: "bare host",
			in:   "https://example.com/",
			want: "https://example.com",
		},
		{
			name: "preserves query",
			in:   "https://example.com/foo?q=1",
			want: "https://example.com/foo?q=1",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalURL(tc.in); got != tc.want {
				t.Errorf("CanonicalURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestURLsEquivalent(t *testing.T) {
	t.Parallel()
	if !URLsEquivalent(
		"http://www.example.com/foo/",
		"https://example.com/foo",
	) {
		t.Errorf("equal urls reported as different")
	}
	if !URLsEquivalent(
		"https://web.archive.org/web/20220124104632/https://example.com/foo",
		"https://example.com/foo",
	) {
		t.Errorf("wayback wrap should match unwrapped origin")
	}
	if URLsEquivalent("https://a.com/x", "https://b.com/x") {
		t.Errorf("different hosts should not match")
	}
	if URLsEquivalent("", "https://example.com") {
		t.Errorf("empty url should not match anything")
	}
}

func TestParseWaybackURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in           string
		wantOK       bool
		wantOrigin   string
		wantSnapshot string
	}{
		{
			in:           "https://web.archive.org/web/20220124104632/https://example.com/foo",
			wantOK:       true,
			wantOrigin:   "https://example.com/foo",
			wantSnapshot: "https://web.archive.org/web/20220124104632if_/https://example.com/foo",
		},
		{
			in:           "http://web.archive.org/web/20160610080136/https://www.example.org/x?q=1",
			wantOK:       true,
			wantOrigin:   "https://www.example.org/x?q=1",
			wantSnapshot: "https://web.archive.org/web/20160610080136if_/https://www.example.org/x?q=1",
		},
		{
			in:           "https://web.archive.org/web/20220621124002if_/https://example.com/already-iframe",
			wantOK:       true,
			wantOrigin:   "https://example.com/already-iframe",
			wantSnapshot: "https://web.archive.org/web/20220621124002if_/https://example.com/already-iframe",
		},
		{in: "https://example.com/", wantOK: false},
		{in: "", wantOK: false},
	}
	for _, tc := range cases {
		origin, snap, ok := ParseWaybackURL(tc.in)
		if ok != tc.wantOK {
			t.Errorf("ParseWaybackURL(%q) ok=%v, want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if origin != tc.wantOrigin {
			t.Errorf("origin = %q, want %q", origin, tc.wantOrigin)
		}
		if snap != tc.wantSnapshot {
			t.Errorf("snapshot = %q, want %q", snap, tc.wantSnapshot)
		}
	}
}

func TestLooksLikeSingleURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com/foo", true},
		{"http://example.com/foo?q=1", true},
		{"https://example.com/foo), [see also](https://other.com/x", false},
		{"not a url", false},
		{"https://example.com/foo bar", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeSingleURL(tc.in); got != tc.want {
			t.Errorf("looksLikeSingleURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
