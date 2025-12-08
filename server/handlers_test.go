package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/icco/postmortems"
)

func TestHealthzCheckHandler(t *testing.T) {
	tests := []struct {
		name    string
		want    []byte
		wantErr bool
	}{
		{
			name:    "healthz HTTP 200",
			want:    []byte("ok."),
			wantErr: false,
		},
	}

	r, err := http.NewRequest(http.MethodGet, "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := httptest.NewRecorder()
			handler := http.HandlerFunc(healthzHandler)
			handler.ServeHTTP(w, r)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Unexpected status code %d", resp.StatusCode)
				return
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("failed to close response body: %v", err)
				}
			}()

			// Check the response body is what we expect.
			if diff := cmp.Diff(w.Body.Bytes(), tc.want); diff != "" {
				t.Errorf("healthzHandler returned unexpected body (-got +want):\n%s", diff)
				return
			}
		})
	}
}

func TestLoadPostmortem(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     *postmortems.Postmortem
		wantErr  bool
	}{
		{
			name:     "successfully loading postmortem",
			filename: "01494547-7ee9-4169-a0c0-d921fa309d83.md",
			want: &postmortems.Postmortem{
				UUID:        "01494547-7ee9-4169-a0c0-d921fa309d83",
				URL:         "http://community.eveonline.com/news/dev-blogs/about-the-boot.ini-issue/",
				Company:     "CCP Games",
				Categories:  []string{"postmortem"},
				Description: "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)",
			},
			wantErr: false,
		},
		{
			name:     "failing loading postmortem",
			filename: "postmortem_not_here.md",
			want:     nil,
			wantErr:  true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := LoadPostmortem("../testdata", tc.filename)
			if (err != nil) != tc.wantErr {
				t.Errorf("LoadPostmortem() returned unexpected error, error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("LoadPostmortem() returned unexpected results (-got +want):\n%s", diff)
			}
		})
	}
}

func TestLoadPostmortems(t *testing.T) {
	tests := []struct {
		name    string
		dir     string
		want    []*postmortems.Postmortem
		wantErr bool
	}{
		{
			name: "successfully loading postmortems",
			dir:  "../testdata",
			want: []*postmortems.Postmortem{
				&postmortems.Postmortem{
					UUID:        "01494547-7ee9-4169-a0c0-d921fa309d83",
					URL:         "http://community.eveonline.com/news/dev-blogs/about-the-boot.ini-issue/",
					Company:     "CCP Games",
					Categories:  []string{"postmortem"},
					Description: "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)"},
			},
			wantErr: false,
		},
		{
			name:    "failing loading postmortems",
			dir:     "no_testdata",
			want:    nil,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := LoadPostmortems(tc.dir)
			if (err != nil) != tc.wantErr {
				t.Errorf("LoadPostmortems() returned unexpected error, error = %v, wantErr %v", err, tc.wantErr)
				return
			}

			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("LoadPostmortems() returned unexpected results (-got +want):\n%s", diff)
			}
		})
	}
}

func TestGetPosmortemByCategory(t *testing.T) {
	tests := []struct {
		name     string
		category string
		pms      []*postmortems.Postmortem
		want     []postmortems.Postmortem
	}{
		{
			name:     "successfully loading postmortems",
			category: "postmortem",
			pms: []*postmortems.Postmortem{
				&postmortems.Postmortem{
					UUID:        "01494547-7ee9-4169-a0c0-d921fa309d83",
					URL:         "http://community.eveonline.com/news/dev-blogs/about-the-boot.ini-issue/",
					Company:     "CCP Games",
					Categories:  []string{"postmortem"},
					Description: "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)",
				},
				&postmortems.Postmortem{
					UUID:        "0ea35968-4578-408c-b4fd-8c6ccc3501b0",
					URL:         "http://aws.amazon.com/message/4372T8/",
					Company:     "Amazon",
					Categories:  []string{"hardware"},
					Description: "At 10:25pm PDT on June 4, loss of power at an AWS Sydney facility resulting from severe weather in that area lead to disruption to a significant number of instances in an Availability Zone. Due to the signature of the power loss, power  isolation breakers did not engage, resulting in backup energy reserves draining into the degraded power grid.",
				},
			},
			want: []postmortems.Postmortem{
				postmortems.Postmortem{
					UUID:        "01494547-7ee9-4169-a0c0-d921fa309d83",
					URL:         "http://community.eveonline.com/news/dev-blogs/about-the-boot.ini-issue/",
					Company:     "CCP Games",
					Categories:  []string{"postmortem"},
					Description: "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)",
				},
			},
		},
		{
			name:     "successfully managing no valid category",
			category: "not-valid",
			pms: []*postmortems.Postmortem{
				&postmortems.Postmortem{
					UUID:        "01494547-7ee9-4169-a0c0-d921fa309d83",
					URL:         "http://community.eveonline.com/news/dev-blogs/about-the-boot.ini-issue/",
					Company:     "CCP Games",
					Categories:  []string{"postmortem"},
					Description: "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)",
				},
				&postmortems.Postmortem{
					UUID:        "0ea35968-4578-408c-b4fd-8c6ccc3501b0",
					URL:         "http://aws.amazon.com/message/4372T8/",
					Company:     "Amazon",
					Categories:  []string{"hardware"},
					Description: "At 10:25pm PDT on June 4, loss of power at an AWS Sydney facility resulting from severe weather in that area lead to disruption to a significant number of instances in an Availability Zone. Due to the signature of the power loss, power  isolation breakers did not engage, resulting in backup energy reserves draining into the degraded power grid.",
				},
			},
			want: []postmortems.Postmortem{},
		},
		{
			name:     "empty postmortem list",
			category: "postmortem",
			pms:      []*postmortems.Postmortem{},
			want:     []postmortems.Postmortem{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := getPosmortemByCategory(tc.pms, tc.category)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("getPostmortemByCategory() returned unexpected results (-got +want):\n%s", diff)
			}
		})
	}
}
