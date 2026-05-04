package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/icco/postmortems"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"
)

// TestMetricsEndpoint asserts the chi route pattern is tagged onto otelhttp metrics.
func TestMetricsEndpoint(t *testing.T) {
	reg := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(reg))
	if err != nil {
		t.Fatalf("otelprom.New: %v", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter provider shutdown: %v", err)
		}
	})

	h := New(Options{
		Logger:         zap.NewNop().Sugar(),
		MetricsHandler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz") //nolint:noctx // test
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Logf("drain healthz body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Logf("close healthz body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}

	mResp, err := http.Get(srv.URL + "/metrics") //nolint:noctx // test
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer func() {
		if err := mResp.Body.Close(); err != nil {
			t.Logf("close metrics body: %v", err)
		}
	}()
	if mResp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", mResp.StatusCode)
	}
	raw, err := io.ReadAll(mResp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	text := string(raw)

	for _, want := range []string{
		"http_server_request_duration_seconds",
		`http_route="/healthz"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics body missing %q\nbody:\n%s", want, text)
		}
	}
}

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

			if diff := cmp.Diff(w.Body.Bytes(), tc.want); diff != "" {
				t.Errorf("healthzHandler returned unexpected body (-got +want):\n%s", diff)
				return
			}
		})
	}
}

const (
	testUUID        = "01494547-7ee9-4169-a0c0-d921fa309d83"
	testURL         = "http://community.eveonline.com/news/dev-blogs/about-the-boot.ini-issue/"
	testCompany     = "CCP Games"
	testDescription = "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)"
)

func TestLoadPostmortem(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     *postmortems.Postmortem
		wantErr  bool
	}{
		{
			name:     "successfully loading postmortem",
			filename: testUUID + ".md",
			want: &postmortems.Postmortem{
				UUID:        testUUID,
				URL:         testURL,
				Company:     testCompany,
				Description: testDescription,
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
					UUID:        testUUID,
					URL:         testURL,
					Company:     testCompany,
					Description: testDescription},
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

// TestPageMetaTags asserts each route emits a unique canonical URL plus
// matching Open Graph and Twitter Card tags.
func TestPageMetaTags(t *testing.T) {
	t.Chdir("..")

	h := New(Options{
		Logger: zap.NewNop().Sugar(),
		Dir:    "testdata",
	})

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := http.Get(srv.URL + path) //nolint:noctx // test
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				t.Logf("close body: %v", cerr)
			}
		}()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body for %s: %v", path, err)
		}
		return resp.StatusCode, string(body)
	}

	t.Run("about", func(t *testing.T) {
		status, body := get("/about")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		mustContain(t, body, []string{
			`<meta property="og:url" content="https://postmortems.app/about">`,
			`<link rel="canonical" href="https://postmortems.app/about">`,
			`<meta property="og:title" content="About postmortems.app">`,
			`<meta property="og:type" content="website">`,
			`<meta name="twitter:url" content="https://postmortems.app/about">`,
		})
	})

	t.Run("postmortem-page", func(t *testing.T) {
		path := "/postmortem/" + testUUID
		status, body := get(path)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		mustContain(t, body, []string{
			`<meta property="og:type" content="article">`,
			`<meta property="og:url" content="https://postmortems.app/postmortem/` + testUUID + `">`,
			`<link rel="canonical" href="https://postmortems.app/postmortem/` + testUUID + `">`,
			`<meta property="og:title" content="CCP Games postmortem">`,
		})
	})
}

// mustContain fails the test if any needle is missing, surfacing all gaps at once.
func mustContain(t *testing.T, body string, needles []string) {
	t.Helper()
	var missing []string
	for _, n := range needles {
		if !strings.Contains(body, n) {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		t.Errorf("response body missing expected tags:\n  %s", strings.Join(missing, "\n  "))
	}
}

// TestAbsURL covers path normalisation for canonical/og:url builders.
func TestAbsURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "https://postmortems.app/"},
		{"/", "https://postmortems.app/"},
		{"/about", "https://postmortems.app/about"},
		{"about", "https://postmortems.app/about"},
		{"/postmortem/abc-123", "https://postmortems.app/postmortem/abc-123"},
	}
	for _, tc := range cases {
		if got := absURL(tc.in); got != tc.want {
			t.Errorf("absURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSummarizeMarkdown verifies OG descriptions strip Markdown and cut on a word boundary.
func TestSummarizeMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{
			name: "strips markdown link",
			in:   "A typo caused the [installer](https://example.com/x) to delete *boot.ini*.",
			max:  200,
			want: "A typo caused the installer to delete boot.ini.",
		},
		{
			name: "truncates at word boundary",
			in:   "alpha bravo charlie delta echo foxtrot golf hotel india juliet",
			max:  20,
			want: "alpha bravo charlie\u2026",
		},
		{
			name: "empty stays empty",
			in:   "   ",
			max:  100,
			want: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := summarizeMarkdown(tc.in, tc.max); got != tc.want {
				t.Errorf("summarizeMarkdown(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

func TestGetPostmortemsByCategory(t *testing.T) {
	tests := []struct {
		name     string
		category string
		pms      []*postmortems.Postmortem
		want     []postmortems.Postmortem
	}{
		{
			name:     "filters by category",
			category: "cloud",
			pms: []*postmortems.Postmortem{
				&postmortems.Postmortem{
					UUID:        testUUID,
					URL:         testURL,
					Company:     testCompany,
					Categories:  []string{"cloud"},
					Description: testDescription,
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
					UUID:        testUUID,
					URL:         testURL,
					Company:     testCompany,
					Categories:  []string{"cloud"},
					Description: testDescription,
				},
			},
		},
		{
			name:     "no matching category",
			category: "not-valid",
			pms: []*postmortems.Postmortem{
				&postmortems.Postmortem{
					UUID:        testUUID,
					URL:         testURL,
					Company:     testCompany,
					Categories:  []string{"cloud"},
					Description: testDescription,
				},
			},
			want: []postmortems.Postmortem{},
		},
		{
			name:     "empty postmortem list",
			category: "cloud",
			pms:      []*postmortems.Postmortem{},
			want:     []postmortems.Postmortem{},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := getPostmortemsByCategory(tc.pms, tc.category)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("getPostmortemsByCategory() returned unexpected results (-got +want):\n%s", diff)
			}
		})
	}
}
