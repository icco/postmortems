package postmortems

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		filepath string
		want     *Postmortem
		wantErr  bool
	}{
		{
			name:     "successfully parsing Markdown file",
			filepath: "testdata/01494547-7ee9-4169-a0c0-d921fa309d83.md",
			want: &Postmortem{
				UUID:        "01494547-7ee9-4169-a0c0-d921fa309d83",
				URL:         "http://community.eveonline.com/news/dev-blogs/about-the-boot.ini-issue/",
				Company:     "CCP Games",
				Categories:  []string{categoryPostmortem},
				Description: "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)",
			},
			wantErr: false,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, err := os.Open(filepath.Join(tc.filepath))
			if err != nil {
				t.Errorf("error opening postmortem: %v", err)
				return
			}

			got, err := Parse(f)
			if (err != nil) != tc.wantErr {
				t.Errorf("Parse() failed parsing file %s: %v", f.Name(), err)
				return
			}

			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("Parse() returned unexpected results (-got +want):\n%s", diff)
			}
		})
	}
}

func TestParseTitle(t *testing.T) {
	t.Parallel()

	const body = `---
uuid: "abc"
url: "https://example.com/postmortem"
title: "Example outage of 2024"
company: "Example Inc"
categories:
- postmortem

---

Example body.
`

	got, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Title != "Example outage of 2024" {
		t.Errorf("Title = %q, want %q", got.Title, "Example outage of 2024")
	}
}

func TestEventDatePeriod(t *testing.T) {
	t.Parallel()

	mar15 := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)
	mar15local := time.Date(2024, 3, 15, 23, 30, 0, 0, time.FixedZone("test", 4*60*60))
	mar20 := time.Date(2024, 3, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		pm   Postmortem
		want string
	}{
		{name: "both zero", pm: Postmortem{}, want: ""},
		{name: "only start", pm: Postmortem{StartTime: mar15}, want: "2024-03-15"},
		{name: "only end", pm: Postmortem{EndTime: mar20}, want: "until 2024-03-20"},
		{name: "same day", pm: Postmortem{StartTime: mar15, EndTime: mar15local}, want: "2024-03-15"},
		{name: "range", pm: Postmortem{StartTime: mar15, EndTime: mar20}, want: "2024-03-15 \u2013 2024-03-20"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.pm.EventDatePeriod(); got != tc.want {
				t.Errorf("EventDatePeriod() = %q, want %q", got, tc.want)
			}
		})
	}
}
