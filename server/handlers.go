package server

import (
	"compress/flate"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"text/template"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/icco/gutil/logging"
	"github.com/icco/gutil/otel"
	"github.com/icco/postmortems"
	"github.com/russross/blackfriday/v2"
	"go.uber.org/zap"
)

var (
	dir *string
	log = logging.Must(logging.NewLogger(postmortems.Service))
)

// New creates a new HTTP routing handler.
func New(d *string) http.Handler {
	dir = d

	r := chi.NewRouter()
	r.Use(otel.Middleware)
	r.Use(middleware.RealIP)
	r.Use(logging.Middleware(log.Desugar(), postmortems.GCPProject))

	compressor := middleware.NewCompressor(flate.DefaultCompression)
	r.Use(compressor.Handler)

	fs := http.FileServer(http.Dir("static"))
	r.Handle("/*", fs)

	r.Handle("/output/*", http.StripPrefix("/output/", http.FileServer(http.Dir("./output"))))

	r.Get("/", indexHandler)
	r.Get("/about", aboutPageHandler)
	r.Get("/postmortem/{id}", postmortemPageHandler)
	r.Get("/postmortem/{id}.json", postmortemJSONPageHandler)
	r.Get("/category/{category}", categoryPageHandler)
	r.Get("/healthz", healthzHandler)

	return r
}

// healthzHandler serves an availability check endpoint.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/plain")

	if _, err := w.Write([]byte("ok.")); err != nil {
		log.Errorw("error writing response to healthz request", zap.Error(err))
	}
}

// LoadPostmortem loads the postmortem data in memory.
func LoadPostmortem(dir, filename string) (*postmortems.Postmortem, error) {
	f, err := os.Open(filepath.Join(dir, filename))
	if err != nil {
		return nil, fmt.Errorf("error opening postmortem: %w", err)
	}

	pm, err := postmortems.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("error parsing file %s: %w", f.Name(), err)
	}

	return pm, nil
}

// LoadPostmortems parses the postmortem files
// and returns a slice with their content.
func LoadPostmortems(dir string) ([]*postmortems.Postmortem, error) {
	files, err := ioutil.ReadDir(dir)
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

// renderTemplate renders the template and its respective data.
func renderTemplate(w http.ResponseWriter, r *http.Request, view string, data interface{}) {
	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", view)

	_, err := os.Stat(fp)
	if err != nil {
		if os.IsNotExist(err) {
			// Template doesn't exist, return 404.
			http.NotFound(w, r)
			return
		}
	}

	tmpl, err := template.ParseFiles(lp, fp)
	if err != nil {
		log.Errorw("template parse error", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		log.Errorw("template execute error", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// getPosmortemByCategory return postmortem by category.
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

func categoryPageHandler(w http.ResponseWriter, r *http.Request) {
	ct := chi.URLParam(r, "category")

	pms, err := LoadPostmortems(*dir)
	if err != nil {
		log.Errorw("load postmortems", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	ctpm := getPosmortemByCategory(pms, ct)

	page := struct {
		Category    string
		Categories  []string
		Postmortems []postmortems.Postmortem
	}{
		Category:    ct,
		Categories:  postmortems.Categories,
		Postmortems: ctpm,
	}

	renderTemplate(w, r, "category.html", page)
}

func aboutPageHandler(w http.ResponseWriter, r *http.Request) {
	page := struct {
		Categories []string
	}{
		Categories: postmortems.Categories,
	}

	renderTemplate(w, r, "about.html", page)
}

func postmortemPageHandler(w http.ResponseWriter, r *http.Request) {
	pmID := chi.URLParam(r, "id")

	pm, err := LoadPostmortem(*dir, pmID+".md")
	if err != nil {
		log.Warnw("load postmortem", "pmid", pmID, zap.Error(err))
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)

		return
	}

	// Convert Markdown formatting of descriptions to HTML.
	htmlDesc := blackfriday.Run([]byte(pm.Description))
	pm.Description = string(htmlDesc)

	page := struct {
		Categories  []string
		Postmortems []*postmortems.Postmortem
	}{
		Categories:  postmortems.Categories,
		Postmortems: []*postmortems.Postmortem{pm},
	}

	renderTemplate(w, r, "postmortem.html", page)
}

func postmortemJSONPageHandler(w http.ResponseWriter, r *http.Request) {
	pmID := chi.URLParam(r, "id")

	jsonPM := pmID + ".json"

	data, err := ioutil.ReadFile(filepath.Join("output/", jsonPM))
	if err != nil {
		log.Errorw("load postmortem json", "pmid", pmID, zap.Error(err))
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(data); err != nil {
		log.Errorw("error writing response to postmortem JSON request", zap.Error(err))
	}
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	pms, err := LoadPostmortems(*dir)
	if err != nil {
		log.Errorw("load postmortems", zap.Error(err))
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
