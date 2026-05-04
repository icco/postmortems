// Package server exposes the HTTP API for the postmortems site.
package server

import (
	"compress/flate"
	"encoding/xml"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/icco/gutil/logging"
	"github.com/icco/postmortems"
	"github.com/russross/blackfriday/v2"
	"github.com/unrolled/secure"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.uber.org/zap"
)

// canonicalBase is the public origin used for canonical links and og:url.
var canonicalBase = url.URL{Scheme: "https", Host: "postmortems.app"}

const siteTagline = "A public repository of incident reports with annotated metadata and summaries of public postmortem documents."

// PageMeta is the per-page SEO/social metadata layout.html consumes as `.Meta`.
// Path is site-relative; absURL turns it into a full canonical URL.
type PageMeta struct {
	Path        string
	Title       string
	Description string
	OGType      string // "website" (default) or "article"
}

const serverName = "postmortems"

// reportd receives Web Vitals + browser security reports. See https://reportd.natwelch.com.
var reportd = url.URL{Scheme: "https", Host: "reportd.natwelch.com"}

const reportdService = "postmortems"

// Options configures the HTTP router. MetricsHandler is mounted at /metrics.
type Options struct {
	Logger         *zap.SugaredLogger
	MetricsHandler http.Handler
	Dir            string
}

// postmortemView is a render-layer copy of Postmortem with Description as
// pre-rendered HTML.
type postmortemView struct {
	UUID            string
	URL             string
	Title           string
	Company         string
	Product         string
	Categories      []string
	Keywords        []string
	EventDatePeriod string
	Description     template.HTML // already sanitised by blackfriday
}

func toView(pm *postmortems.Postmortem) postmortemView {
	return postmortemView{
		UUID:            pm.UUID,
		URL:             pm.URL,
		Title:           pm.Title,
		Company:         pm.Company,
		Product:         pm.Product,
		Categories:      pm.Categories,
		Keywords:        pm.Keywords,
		EventDatePeriod: pm.EventDatePeriod(),
		Description:     template.HTML(pm.Description), // #nosec G203 -- blackfriday output
	}
}

// New returns the HTTP handler, wrapped with otelhttp (excluding /metrics).
func New(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(logging.Middleware(opts.Logger.Desugar()))
	r.Use(routeTag)
	r.Use(reportingHeaders)
	r.Use(secure.New(secureOptions()).Handler)

	compressor := middleware.NewCompressor(flate.DefaultCompression)
	r.Use(compressor.Handler)

	r.Handle("/output/*", http.StripPrefix("/output/", http.FileServer(http.Dir("./output"))))

	r.Get("/", indexHandler(opts.Dir))
	r.Get("/about", aboutPageHandler(opts.Dir))
	r.Get("/postmortem/{id}", postmortemPageHandler(opts.Dir))
	r.Get("/postmortem/{id}.json", postmortemJSONPageHandler)
	r.Get("/category/{category}", categoryPageHandler(opts.Dir))
	r.Get("/company/{company}", companyPageHandler(opts.Dir))
	r.Get("/healthz", healthzHandler)
	r.Get("/sitemap.xml", sitemapHandler(opts.Dir))

	if opts.MetricsHandler != nil {
		r.Method(http.MethodGet, "/metrics", opts.MetricsHandler)
	}

	r.NotFound(notFoundHandler)
	r.MethodNotAllowed(notFoundHandler)
	r.Handle("/*", staticOrNotFound("static"))

	return otelhttp.NewHandler(r, serverName,
		otelhttp.WithFilter(func(req *http.Request) bool {
			return req.URL.Path != "/metrics"
		}),
	)
}

// routeTag stamps the chi route pattern onto otelhttp metric labels.
func routeTag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		labeler, ok := otelhttp.LabelerFromContext(r.Context())
		if !ok {
			return
		}
		if pattern := chi.RouteContext(r.Context()).RoutePattern(); pattern != "" {
			labeler.Add(semconv.HTTPRoute(pattern))
		}
	})
}

func reportEndpoint() string    { return reportd.JoinPath("report", reportdService).String() }
func reportingEndpoint() string { return reportd.JoinPath("reporting", reportdService).String() }

// reportingHeaders sets Report-To and Reporting-Endpoints so browser
// reports land in reportd. CSP itself is set by unrolled/secure.
func reportingHeaders(next http.Handler) http.Handler {
	reportTo := fmt.Sprintf(`{"group":"default","max_age":10886400,"endpoints":[{"url":%q}]}`, reportEndpoint())
	reportingEndpoints := fmt.Sprintf(`default=%q`, reportingEndpoint())

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Report-To", reportTo)
		h.Set("Reporting-Endpoints", reportingEndpoints)

		next.ServeHTTP(w, r)
	})
}

// secureOptions matches the icco/reportd + icco/inspiration setup and opens the
// CSP for the Tailwind/daisyUI CDN and the unpkg-hosted web-vitals module.
func secureOptions() secure.Options {
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://unpkg.com",
		"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
		"img-src 'self' data:",
		"font-src 'self' data: https://cdn.jsdelivr.net",
		fmt.Sprintf("connect-src 'self' %s https://cdn.jsdelivr.net", reportd.String()),
		"object-src 'none'",
		"base-uri 'self'",
		"frame-ancestors 'none'",
		fmt.Sprintf("report-uri %s", reportEndpoint()),
		"report-to default",
	}, "; ")

	return secure.Options{
		// Cloud Run terminates TLS; trust X-Forwarded-Proto so HSTS fires at the
		// edge instead of looping locally.
		SSLRedirect:          false,
		SSLProxyHeaders:      map[string]string{"X-Forwarded-Proto": "https"},
		STSSeconds:           63072000, // 2 years
		STSIncludeSubdomains: true,
		STSPreload:           true,
		ForceSTSHeader:       false,

		FrameDeny:          true,
		ContentTypeNosniff: true,
		BrowserXssFilter:   true,
		ReferrerPolicy:     "strict-origin-when-cross-origin",
		PermissionsPolicy:  "geolocation=(), midi=(), sync-xhr=(), microphone=(), camera=(), magnetometer=(), gyroscope=(), fullscreen=(), payment=(), usb=()",

		ContentSecurityPolicy: csp,
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok.")); err != nil {
		logging.FromContext(r.Context()).Errorw("write healthz", zap.Error(err))
	}
}

// LoadPostmortem reads a single postmortem from dir.
func LoadPostmortem(dir, filename string) (*postmortems.Postmortem, error) {
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
		return nil, fmt.Errorf("invalid postmortem filename: %q", filename)
	}
	filename = filepath.Base(filename)
	f, err := os.Open(filepath.Join(dir, filename)) // #nosec G304 -- filename validated above
	if err != nil {
		return nil, fmt.Errorf("error opening postmortem: %w", err)
	}

	pm, err := postmortems.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("error parsing file %s: %w", f.Name(), err)
	}

	return pm, nil
}

// LoadPostmortems reads every postmortem under dir.
func LoadPostmortems(dir string) ([]*postmortems.Postmortem, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("error opening data folder: %w", err)
	}

	var pms []*postmortems.Postmortem
	for _, path := range files {
		pm, err := LoadPostmortem(dir, path.Name())
		if err != nil {
			return nil, err
		}
		pms = append(pms, pm)
	}

	return pms, nil
}

var templateFuncs = template.FuncMap{
	"companySlug":   CompanySlug,
	"categoryDesc":  describeCategory,
	"categoryEmoji": categoryEmoji,
	"prettifyText":  prettifyText,
	"firstNonEmpty": firstNonEmpty,
	"absURL":        absURL,
}

// absURL resolves a site-relative path against canonicalBase.
func absURL(p string) string {
	if p == "" {
		p = "/"
	}
	return canonicalBase.JoinPath(p).String()
}

var (
	markdownLinkRE = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	htmlTagRE      = regexp.MustCompile(`<[^>]+>`)
)

// plainText turns Markdown or rendered HTML into a single line of readable text
// suitable for an OG description.
func plainText(s string) string {
	s = markdownLinkRE.ReplaceAllString(s, "$1")
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = strings.NewReplacer("*", "", "_", "", "`", "", "#", "", ">", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// truncateAtWord clips s to at most maxRunes on a word boundary, ellipsising
// when it cuts.
func truncateAtWord(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	cut := maxRunes
	for cut > 0 && !unicode.IsSpace(runes[cut]) {
		cut--
	}
	if cut == 0 {
		cut = maxRunes
	}
	return strings.TrimRight(string(runes[:cut]), " \t,.;:") + "\u2026"
}

// summarizeMarkdown returns an OG-friendly truncated plain-text summary.
func summarizeMarkdown(md string, maxRunes int) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	return truncateAtWord(plainText(md), maxRunes)
}

// renderTemplate parses layout.html + view and writes the response.
func renderTemplate(w http.ResponseWriter, r *http.Request, view string, data any) {
	l := logging.FromContext(r.Context())
	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", view)

	if _, err := os.Stat(fp); err != nil {
		if os.IsNotExist(err) {
			notFoundHandler(w, r)
			return
		}
	}

	tmpl, err := template.New("layout.html").Funcs(templateFuncs).ParseFiles(lp, fp)
	if err != nil {
		l.Errorw("template parse error", "view", view, zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		l.Errorw("template execute error", "view", view, zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// notFoundHandler writes the styled 404 page, falling back to plain text on
// template failure.
func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	l := logging.FromContext(r.Context())
	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", "404.html")

	tmpl, err := template.New("layout.html").Funcs(templateFuncs).ParseFiles(lp, fp)
	if err != nil {
		l.Errorw("404 template parse error", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	page := struct {
		Meta       PageMeta
		Categories []string
	}{
		Meta: PageMeta{
			// Empty Path so layout.html canonical falls back to "/".
			Path:        "",
			Title:       "404 \u2014 Not Found",
			Description: "The page you were looking for could not be found.",
			OGType:      "website",
		},
		Categories: postmortems.Categories,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := tmpl.ExecuteTemplate(w, "layout", page); err != nil {
		l.Errorw("404 template execute error", zap.Error(err))
	}
}

// staticOrNotFound serves files from dir, falling back to the styled 404 page
// for missing paths.
func staticOrNotFound(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean("/" + r.URL.Path)
		full := filepath.Join(dir, clean)
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			notFoundHandler(w, r)
			return
		}
		fs.ServeHTTP(w, r)
	})
}

func getPostmortemsByCategory(pms []*postmortems.Postmortem, category string) []postmortems.Postmortem {
	out := []postmortems.Postmortem{}
	for _, pm := range pms {
		for _, c := range pm.Categories {
			if c == category {
				out = append(out, *pm)
				break
			}
		}
	}
	return out
}

// CompanySlug turns a company name into a URL-safe slug for /company/{slug}.
func CompanySlug(c string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(c) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func getPostmortemsByCompanySlug(pms []*postmortems.Postmortem, slug string) []postmortems.Postmortem {
	out := []postmortems.Postmortem{}
	for _, pm := range pms {
		if CompanySlug(pm.Company) == slug {
			out = append(out, *pm)
		}
	}
	return out
}

// labeledCount is a (name, count) pair used by templates for top-N lists.
type labeledCount struct {
	Name  string
	Slug  string // populated for company entries
	Count int
}

// dateRange tracks min/max event dates for company/category headers.
type dateRange struct {
	Earliest time.Time
	Latest   time.Time
	HasAny   bool
}

func (d dateRange) String() string {
	if !d.HasAny {
		return ""
	}
	const layout = "Jan 2006"
	es, ls := d.Earliest.Format(layout), d.Latest.Format(layout)
	if es == ls {
		return es
	}
	return es + " \u2013 " + ls
}

// SpanYears returns the number of calendar years covered, or 0 if unknown.
func (d dateRange) SpanYears() int {
	if !d.HasAny {
		return 0
	}
	return d.Latest.Year() - d.Earliest.Year() + 1
}

func computeDateRange(pms []postmortems.Postmortem) dateRange {
	var dr dateRange
	consider := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if !dr.HasAny {
			dr.Earliest, dr.Latest, dr.HasAny = t, t, true
			return
		}
		if t.Before(dr.Earliest) {
			dr.Earliest = t
		}
		if t.After(dr.Latest) {
			dr.Latest = t
		}
	}
	for _, pm := range pms {
		consider(pm.StartTime)
		consider(pm.EndTime)
	}
	return dr
}

// topLabeledCounts returns counts sorted by count desc, name asc; limit<=0 returns all.
func topLabeledCounts(counts map[string]int, limit int) []labeledCount {
	out := make([]labeledCount, 0, len(counts))
	for k, v := range counts {
		if k == "" {
			continue
		}
		out = append(out, labeledCount{Name: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// topCompanies is topLabeledCounts with CompanySlug attached on each entry.
func topCompanies(counts map[string]int, limit int) []labeledCount {
	out := topLabeledCounts(counts, limit)
	for i := range out {
		out[i].Slug = CompanySlug(out[i].Name)
	}
	return out
}

// sortPostmortems orders by event date desc, undated last, then title/company.
func sortPostmortems(pms []postmortems.Postmortem) {
	sort.SliceStable(pms, func(i, j int) bool {
		ai, aj := pms[i].StartTime, pms[j].StartTime
		switch {
		case !ai.IsZero() && aj.IsZero():
			return true
		case ai.IsZero() && !aj.IsZero():
			return false
		case !ai.IsZero() && !aj.IsZero() && !ai.Equal(aj):
			return ai.After(aj)
		}
		ki := strings.ToLower(firstNonEmpty(pms[i].Title, pms[i].Company))
		kj := strings.ToLower(firstNonEmpty(pms[j].Title, pms[j].Company))
		return ki < kj
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// prettifyText turns "cascading-failure" into "Cascading Failure".
func prettifyText(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// categoryDescriptions are blurbs shown on the category page header.
var categoryDescriptions = map[string]string{
	"automation":        "Incidents caused or amplified by automated systems acting incorrectly, too aggressively, or without enough human review.",
	"cascading-failure": "One small failure that snowballed: retries, thundering herds, or thread pool exhaustion that took out adjacent services.",
	"cloud":             "Outages of, or caused by, public cloud providers (AWS, GCP, Azure, etc.) and their managed services.",
	"config-change":     "Bad configuration pushed to production: feature flags, network ACLs, IAM policies, build settings, and routing rules.",
	"postmortem":        "All entries with a published post-incident review. The default category every postmortem belongs to.",
	"hardware":          "Disks, NICs, power, cooling, fibre cuts, and other physical-layer faults that took systems offline.",
	"security":          "Outages caused by security incidents, mitigations, or hardening rollouts (revoked certs, blocked traffic, expired credentials).",
	"time":              "NTP, leap seconds, timezone bugs, clock drift, and timestamp serialisation issues.",
	"undescriptive":     "Brief blurbs without enough text to categorise meaningfully \u2014 candidates for follow-up enrichment.",
}

func describeCategory(c string) string {
	if d, ok := categoryDescriptions[c]; ok {
		return d
	}
	return "Postmortems tagged with \"" + c + "\"."
}

// categoryEmoji returns a per-category emoji for the page header.
func categoryEmoji(c string) string {
	switch c {
	case "automation":
		return "\U0001F916" // robot
	case "cascading-failure":
		return "\U0001F30A" // wave
	case "cloud":
		return "\u2601\uFE0F" // cloud
	case "config-change":
		return "\u2699\uFE0F" // gear
	case "hardware":
		return "\U0001F5A5\uFE0F" // desktop
	case "security":
		return "\U0001F512" // lock
	case "time":
		return "\u23F1\uFE0F" // stopwatch
	case "postmortem":
		return "\U0001F4D6" // book
	case "undescriptive":
		return "\u2754" // white question mark
	}
	return "\U0001F4DD" // memo
}

func companyPageHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logging.FromContext(r.Context())
		slug := chi.URLParam(r, "company")

		pms, err := LoadPostmortems(dir)
		if err != nil {
			l.Errorw("load postmortems", "company", slug, zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		matches := getPostmortemsByCompanySlug(pms, slug)
		if len(matches) == 0 {
			notFoundHandler(w, r)
			return
		}

		sortPostmortems(matches)

		categoryCounts := map[string]int{}
		productCounts := map[string]int{}
		for _, pm := range matches {
			for _, c := range pm.Categories {
				categoryCounts[c]++
			}
			if pm.Product != "" {
				productCounts[pm.Product]++
			}
		}
		dr := computeDateRange(matches)

		company := matches[0].Company
		var desc string
		if dr.HasAny {
			desc = fmt.Sprintf("%d %s from %s, spanning %s.",
				len(matches), pluralize("postmortem", len(matches)), company, dr.String())
		} else {
			desc = fmt.Sprintf("%d %s from %s.",
				len(matches), pluralize("postmortem", len(matches)), company)
		}

		page := struct {
			Meta           PageMeta
			Company        string
			Slug           string
			Categories     []string
			CategoryCounts []labeledCount
			Products       []labeledCount
			DateRange      string
			SpanYears      int
			Postmortems    []postmortems.Postmortem
		}{
			Meta: PageMeta{
				Path:        "/company/" + slug,
				Title:       company + " postmortems",
				Description: desc,
				OGType:      "website",
			},
			Company:        company,
			Slug:           slug,
			Categories:     postmortems.Categories,
			CategoryCounts: topLabeledCounts(categoryCounts, 0),
			Products:       topLabeledCounts(productCounts, 6),
			DateRange:      dr.String(),
			SpanYears:      dr.SpanYears(),
			Postmortems:    matches,
		}

		renderTemplate(w, r, "company.html", page)
	}
}

// pluralize returns word with an "s" appended when n != 1.
func pluralize(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func categoryPageHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logging.FromContext(r.Context())
		ct := chi.URLParam(r, "category")

		pms, err := LoadPostmortems(dir)
		if err != nil {
			l.Errorw("load postmortems", "category", ct, zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		matches := getPostmortemsByCategory(pms, ct)
		sortPostmortems(matches)

		companyCounts := map[string]int{}
		keywordCounts := map[string]int{}
		coCategoryCounts := map[string]int{}
		for _, pm := range matches {
			if pm.Company != "" {
				companyCounts[pm.Company]++
			}
			for _, kw := range pm.Keywords {
				keywordCounts[strings.ToLower(kw)]++
			}
			for _, c := range pm.Categories {
				if c == ct {
					continue
				}
				coCategoryCounts[c]++
			}
		}
		dr := computeDateRange(matches)

		page := struct {
			Meta           PageMeta
			Category       string
			Description    string
			Emoji          string
			Categories     []string
			Companies      []labeledCount
			Keywords       []labeledCount
			CoCategories   []labeledCount
			DateRange      string
			SpanYears      int
			TotalCompanies int
			Postmortems    []postmortems.Postmortem
		}{
			Meta: PageMeta{
				Path:        "/category/" + ct,
				Title:       prettifyText(ct) + " postmortems",
				Description: describeCategory(ct),
				OGType:      "website",
			},
			Category:       ct,
			Description:    describeCategory(ct),
			Emoji:          categoryEmoji(ct),
			Categories:     postmortems.Categories,
			Companies:      topCompanies(companyCounts, 8),
			Keywords:       topLabeledCounts(keywordCounts, 16),
			CoCategories:   topLabeledCounts(coCategoryCounts, 6),
			DateRange:      dr.String(),
			SpanYears:      dr.SpanYears(),
			TotalCompanies: len(companyCounts),
			Postmortems:    matches,
		}

		renderTemplate(w, r, "category.html", page)
	}
}

func aboutPageHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logging.FromContext(r.Context())

		pms, err := LoadPostmortems(dir)
		if err != nil {
			l.Errorw("load postmortems", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		companies := map[string]struct{}{}
		categoryCounts := map[string]int{}
		for _, pm := range pms {
			companies[pm.Company] = struct{}{}
			for _, c := range pm.Categories {
				categoryCounts[c]++
			}
		}

		page := struct {
			Meta           PageMeta
			Categories     []string
			TotalCount     int
			CompanyCount   int
			CategoryCounts map[string]int
		}{
			Meta: PageMeta{
				Path:        "/about",
				Title:       "About postmortems.app",
				Description: "A public, machine-readable corpus of post-incident reviews from companies across the industry, with categories, time data, and room for in-depth analysis.",
				OGType:      "website",
			},
			Categories:     postmortems.Categories,
			TotalCount:     len(pms),
			CompanyCount:   len(companies),
			CategoryCounts: categoryCounts,
		}
		renderTemplate(w, r, "about.html", page)
	}
}

func postmortemPageHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logging.FromContext(r.Context())
		pmID := chi.URLParam(r, "id")

		if strings.Contains(pmID, "/") || strings.Contains(pmID, "\\") || strings.Contains(pmID, "..") {
			http.Error(w, "Invalid postmortem ID", http.StatusBadRequest)
			return
		}

		pm, err := LoadPostmortem(dir, pmID+".md")
		if err != nil {
			l.Warnw("load postmortem", "pmid", pmID, zap.Error(err))
			notFoundHandler(w, r)
			return
		}

		// Summarize from the un-rendered Markdown so we don't have to peel
		// blackfriday's HTML wrappers back off.
		metaSource := pm.Summary
		if metaSource == "" {
			metaSource = pm.Description
		}
		metaDesc := summarizeMarkdown(metaSource, 200)

		pm.Description = string(blackfriday.Run([]byte(pm.Description)))

		page := struct {
			Meta        PageMeta
			Categories  []string
			Postmortems []postmortemView
		}{
			Meta: PageMeta{
				Path:        "/postmortem/" + pm.UUID,
				Title:       firstNonEmpty(pm.Title, pm.Company+" postmortem"),
				Description: metaDesc,
				OGType:      "article",
			},
			Categories:  postmortems.Categories,
			Postmortems: []postmortemView{toView(pm)},
		}

		renderTemplate(w, r, "postmortem.html", page)
	}
}

func postmortemJSONPageHandler(w http.ResponseWriter, r *http.Request) {
	l := logging.FromContext(r.Context())
	pmID := chi.URLParam(r, "id")

	if strings.Contains(pmID, "/") || strings.Contains(pmID, "\\") || strings.Contains(pmID, "..") {
		http.Error(w, "Invalid postmortem ID", http.StatusBadRequest)
		return
	}

	jsonPM := filepath.Base(pmID + ".json")
	data, err := os.ReadFile(filepath.Join("output", jsonPM)) // #nosec G304 -- jsonPM sanitized via filepath.Base
	if err != nil {
		l.Errorw("load postmortem json", "pmid", pmID, zap.Error(err))
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil { // #nosec G705 -- Content-Type is application/json
		l.Errorw("write postmortem json", "pmid", pmID, zap.Error(err))
	}
}

func indexHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logging.FromContext(r.Context())
		pms, err := LoadPostmortems(dir)
		if err != nil {
			l.Errorw("load postmortems", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		page := struct {
			Meta        PageMeta
			Categories  []string
			Postmortems []*postmortems.Postmortem
		}{
			Meta: PageMeta{
				Path:        "/",
				Title:       "Postmortem Index",
				Description: siteTagline,
				OGType:      "website",
			},
			Categories:  postmortems.Categories,
			Postmortems: pms,
		}

		renderTemplate(w, r, "index.html", page)
	}
}

// sitemapURL is a single <url> entry in the sitemap.
type sitemapURL struct {
	Loc        string `xml:"loc"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

// sitemapURLSet is the root element of sitemap.xml.
type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// baseURL derives scheme+host from the request, honouring X-Forwarded-Proto.
func baseURL(r *http.Request) url.URL {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return url.URL{Scheme: scheme, Host: r.Host}
}

// sitemapHandler generates sitemap.xml for postmortems, categories, companies
// and static pages.
func sitemapHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := logging.FromContext(r.Context())

		pms, err := LoadPostmortems(dir)
		if err != nil {
			l.Errorw("sitemap: load postmortems", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		base := baseURL(r)
		at := func(p ...string) string { return base.JoinPath(p...).String() }

		urls := []sitemapURL{
			{Loc: at("/"), ChangeFreq: "daily", Priority: "1.0"},
			{Loc: at("about"), ChangeFreq: "monthly", Priority: "0.5"},
		}
		for _, cat := range postmortems.Categories {
			urls = append(urls, sitemapURL{
				Loc:        at("category", cat),
				ChangeFreq: "weekly",
				Priority:   "0.6",
			})
		}
		seen := map[string]bool{}
		for _, pm := range pms {
			slug := CompanySlug(pm.Company)
			if slug != "" && !seen[slug] {
				seen[slug] = true
				urls = append(urls, sitemapURL{
					Loc:        at("company", slug),
					ChangeFreq: "weekly",
					Priority:   "0.6",
				})
			}
		}
		for _, pm := range pms {
			urls = append(urls, sitemapURL{
				Loc:        at("postmortem", pm.UUID),
				ChangeFreq: "monthly",
				Priority:   "0.8",
			})
		}

		urlset := sitemapURLSet{
			Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
			URLs:  urls,
		}

		// Buffer so an encode error doesn't leave a half-written 200 response.
		var buf strings.Builder
		buf.WriteString(xml.Header)
		enc := xml.NewEncoder(&buf)
		enc.Indent("", "  ")
		if err := enc.Encode(urlset); err != nil {
			l.Errorw("sitemap: encode xml", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(buf.String())); err != nil {
			l.Errorw("sitemap: write response", zap.Error(err))
		}
	}
}
