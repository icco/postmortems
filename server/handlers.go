package server

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"text/template"

	"github.com/go-chi/chi"
	"github.com/icco/postmortems"
	blackfriday "gopkg.in/russross/blackfriday.v2"
)

var (
	dir *string
)

// New creates a new HTTP routing handler.
func New(d *string) http.Handler {
	dir = d

	r := chi.NewRouter()

	r.Get("/", indexHandler)
	r.Get("/postmortem/{id}", postmortemPageHandler)
	r.Get("/categories", categoriesPageHandler)
	r.Get("/category/{category}", categoryPageHandler)
	r.Get("/healthz", healthzHandler)

	return r
}

// healthzHandler serves an availability check endpoint.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/plain")

	if _, err := w.Write([]byte("ok.")); err != nil {
		log.Printf("Error writing response to healthz request")
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
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)

		return
	}

	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
	}
}

func categoriesPageHandler(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, r, "categories.html", postmortems.Categories)
}

func getPosmortemByCategory(pms []*postmortems.Postmortem, category string) []postmortems.Postmortem {
	var ctpm []postmortems.Postmortem

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
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)

		return
	}

	ctpm := getPosmortemByCategory(pms, ct)

	page := struct {
		Category    string
		Postmortems []postmortems.Postmortem
	}{
		Category:    ct,
		Postmortems: ctpm,
	}

	renderTemplate(w, r, "category.html", page)
}

func postmortemPageHandler(w http.ResponseWriter, r *http.Request) {
	pmID := chi.URLParam(r, "id")

	pm, err := LoadPostmortem(*dir, pmID+".md")
	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
	}

	// Convert Markdown formatting of descriptions to HTML.
	htmlDesc := blackfriday.Run([]byte(pm.Description))
	pm.Description = string(htmlDesc)

	renderTemplate(w, r, "postmortem.html", pm)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	pms, err := LoadPostmortems(*dir)
	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)

		return
	}

	renderTemplate(w, r, "index.html", pms)
}
