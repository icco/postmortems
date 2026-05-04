package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestMetricsEndpoint asserts otelhttp's HTTP server histogram lands
// on /metrics tagged with the chi route pattern.
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

func TestNotFoundHandler(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Logf("chdir back: %v", err)
		}
	})
	if err := os.Chdir(".."); err != nil {
		t.Fatalf("chdir to repo root: %v", err)
	}

	h := New(Options{Logger: zap.NewNop().Sugar()})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	t.Run("unknown path returns styled 404", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/this-route-does-not-exist") //nolint:noctx // test
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("close body: %v", err)
			}
		}()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("content-type = %q, want text/html", ct)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), "404") || !strings.Contains(string(body), "Not Found") {
			t.Errorf("body missing 404 marker; got:\n%s", body)
		}
		if !strings.Contains(string(body), "Postmortem Index") {
			t.Errorf("body missing layout header; got:\n%s", body)
		}
	})

	t.Run("missing postmortem returns styled 404", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/postmortem/00000000-0000-0000-0000-000000000000") //nolint:noctx // test
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("close body: %v", err)
			}
		}()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), "Not Found") {
			t.Errorf("body missing 404 marker; got:\n%s", body)
		}
	})
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
	testCategory    = "postmortem"
	testDescription = "A typo and a name conflict caused the installer to sometimes delete the *boot.ini* file on installation of an expansion for *EVE Online* - with [consequences.](https://www.youtube.com/watch?v=msXRFJ2ar_E)"
	amazonCompany   = "Amazon"
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
				Categories:  []string{testCategory},
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
					Categories:  []string{testCategory},
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

func TestCompanySlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"CCP Games", "ccp-games"},
		{"Healthcare.gov", "healthcare-gov"},
		{amazonCompany, "amazon"},
		{"  weird   spaces  ", "weird-spaces"},
		{"a/b/c", "a-b-c"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := CompanySlug(tc.in); got != tc.want {
			t.Errorf("CompanySlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCompanyPageHandler(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Logf("chdir back: %v", err)
		}
	})
	if err := os.Chdir(".."); err != nil {
		t.Fatalf("chdir to repo root: %v", err)
	}

	h := New(Options{Logger: zap.NewNop().Sugar(), Dir: "testdata"})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	t.Run("known company returns matches", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/company/ccp-games") //nolint:noctx // test
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("close body: %v", err)
			}
		}()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if !strings.Contains(string(body), "CCP Games") {
			t.Errorf("body missing CCP Games; got:\n%s", body)
		}
		if !strings.Contains(string(body), testUUID) {
			t.Errorf("body missing UUID; got:\n%s", body)
		}
	})

	t.Run("unknown company returns 404", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/company/does-not-exist") //nolint:noctx // test
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("close body: %v", err)
			}
		}()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}

func TestAboutPageHandler(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Logf("chdir back: %v", err)
		}
	})
	if err := os.Chdir(".."); err != nil {
		t.Fatalf("chdir to repo root: %v", err)
	}

	h := New(Options{Logger: zap.NewNop().Sugar(), Dir: "testdata"})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/about") //nolint:noctx // test
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	for _, want := range []string{"Stats", "Total postmortems", "Unique companies"} {
		if !strings.Contains(text, want) {
			t.Errorf("about page missing %q; got:\n%s", want, text)
		}
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
					UUID:        testUUID,
					URL:         testURL,
					Company:     testCompany,
					Categories:  []string{testCategory},
					Description: testDescription,
				},
				&postmortems.Postmortem{
					UUID:        "0ea35968-4578-408c-b4fd-8c6ccc3501b0",
					URL:         "http://aws.amazon.com/message/4372T8/",
					Company:     amazonCompany,
					Categories:  []string{"hardware"},
					Description: "At 10:25pm PDT on June 4, loss of power at an AWS Sydney facility resulting from severe weather in that area lead to disruption to a significant number of instances in an Availability Zone. Due to the signature of the power loss, power  isolation breakers did not engage, resulting in backup energy reserves draining into the degraded power grid.",
				},
			},
			want: []postmortems.Postmortem{
				postmortems.Postmortem{
					UUID:        testUUID,
					URL:         testURL,
					Company:     testCompany,
					Categories:  []string{testCategory},
					Description: testDescription,
				},
			},
		},
		{
			name:     "successfully managing no valid category",
			category: "not-valid",
			pms: []*postmortems.Postmortem{
				&postmortems.Postmortem{
					UUID:        testUUID,
					URL:         testURL,
					Company:     testCompany,
					Categories:  []string{testCategory},
					Description: testDescription,
				},
				&postmortems.Postmortem{
					UUID:        "0ea35968-4578-408c-b4fd-8c6ccc3501b0",
					URL:         "http://aws.amazon.com/message/4372T8/",
					Company:     amazonCompany,
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
