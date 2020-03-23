package postmortems

import (
	"os"
	"path/filepath"
	"testing"

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
				Categories:  []string{"postmortem"},
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
