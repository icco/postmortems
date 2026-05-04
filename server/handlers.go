// Package server exposes the HTTP API for the postmortems site.
package server

import (
	"compress/flate"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/icco/gutil/logging"
	"github.com/icco/postmortems"
	"github.com/russross/blackfriday/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.uber.org/zap"
)

// serverName is the otelhttp span/metric scope.
const serverName = "postmortems"

// Options configures the HTTP router. MetricsHandler is mounted at /metrics.
type Options struct {
	Logger         *zap.SugaredLogger
	MetricsHandler http.Handler
	Dir            string
}

// postmortemView is a render-layer copy of Postmortem whose Description
// is template.HTML so html/template emits pre-rendered Markdown verbatim.
type postmortemView struct {
	UUID        string
	URL         string
	Title       string
	Company     string
	Product     string
	Categories  []string
	Description template.HTML // already sanitised by blackfriday
}

func toView(pm *postmortems.Postmortem) postmortemView {
	return postmortemView{
		UUID:        pm.UUID,
		URL:         pm.URL,
		Title:       pm.Title,
		Company:     pm.Company,
		Product:     pm.Product,
		Categories:  pm.Categories,
		Description: template.HTML(pm.Description), // #nosec G203 -- blackfriday output
	}
}

// New returns the HTTP handler, wrapped with otelhttp (excluding /metrics).
func New(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(logging.Middleware(opts.Logger.Desugar()))
	r.Use(routeTag)

	compressor := middleware.NewCompressor(flate.DefaultCompression)
	r.Use(compressor.Handler)

	r.Handle("/output/*", http.StripPrefix("/output/", http.FileServer(http.Dir("./output"))))

	r.Get("/", indexHandler(opts.Dir))
	r.Get("/about", aboutPageHandler)
	r.Get("/postmortem/{id}", postmortemPageHandler(opts.Dir))
	r.Get("/postmortem/{id}.json", postmortemJSONPageHandler)
	r.Get("/category/{category}", categoryPageHandler(opts.Dir))
	r.Get("/healthz", healthzHandler)

	if opts.MetricsHandler != nil {
		r.Method(http.MethodGet, "/metrics", opts.MetricsHandler)
	}

	fs := http.FileServer(http.Dir("static"))
	r.Handle("/*", fs)

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

// renderTemplate parses layout.html + view and writes the response.
// Uses html/template so {{ .Field }} interpolations are HTML-escaped.
func renderTemplate(w http.ResponseWriter, r *http.Request, view string, data interface{}) {
	l := logging.FromContext(r.Context())
	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", view)

	if _, err := os.Stat(fp); err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
	}

	tmpl, err := template.ParseFiles(lp, fp)
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

func getPosmortemByCategory(pms []*postmortems.Postmortem, category string) []postmortems.Postmortem {
	ctpm := []postmortems.Postmortem{}
	for _, pm := range pms {
		for _, c := range pm.Categories {
			if c == category {
				ctpm = append(ctpm, *pm)
			}
		}
	}
	return ctpm
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

		page := struct {
			Category    string
			Categories  []string
			Postmortems []postmortems.Postmortem
		}{
			Category:    ct,
			Categories:  postmortems.Categories,
			Postmortems: getPosmortemByCategory(pms, ct),
		}

		renderTemplate(w, r, "category.html", page)
	}
}

func aboutPageHandler(w http.ResponseWriter, r *http.Request) {
	page := struct {
		Categories []string
	}{
		Categories: postmortems.Categories,
	}
	renderTemplate(w, r, "about.html", page)
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
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}

		// Render Markdown -> HTML, then wrap in template.HTML to skip double-escaping.
		pm.Description = string(blackfriday.Run([]byte(pm.Description)))

		page := struct {
			Categories  []string
			Postmortems []postmortemView
		}{
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
			Categories  []string
			Postmortems []*postmortems.Postmortem
		}{
			Categories:  postmortems.Categories,
			Postmortems: pms,
		}

		renderTemplate(w, r, "index.html", page)
	}
}
