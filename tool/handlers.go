package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"text/template"

	"github.com/gorilla/mux"
	blackfriday "gopkg.in/russross/blackfriday.v2"
)

// createHandlers sets the routes for the web server.
func createHandlers() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/", indexHandler).Methods("GET")
	router.HandleFunc("/postmortem/{id}", postmortemPageHandler).Methods("GET")
	router.HandleFunc("/categories", categoriesPageHandler).Methods("GET")
	router.HandleFunc("/category/{category}", categoryPageHandler).Methods("GET")
	router.HandleFunc("/healthz", healthzHandler).Methods("GET")

	return router
}

// healthzHandler serves an availability check endpoint.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/plain")

	if _, err := w.Write([]byte("ok.")); err != nil {
		log.Printf("Error writing response to healthz request")
	}
}

// loadPostmortem loads the postmortem data in memory.
func loadPostmortem(dir, filename string) (*Postmortem, error) {
	f, err := os.Open(filepath.Join(dir, filename))
	if err != nil {
		return nil, fmt.Errorf("error opening postmortem: %w", err)
	}

	pm, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("error parsing file %s: %w", f.Name(), err)
	}

	return pm, nil
}

// loadPostmortems parses the postmortem files
// and returns a slice with their content.
func loadPostmortems(dir string) ([]*Postmortem, error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("error opening data folder: %w", err)
	}

	var pms []*Postmortem

	for _, path := range files {
		pm, err := loadPostmortem(dir, path.Name())
		if err != nil {
			return nil, err
		}

		pms = append(pms, pm)
	}

	return pms, nil
}

func categoriesPageHandler(w http.ResponseWriter, r *http.Request) {
	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", "categories.html")

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

	if err := tmpl.ExecuteTemplate(w, "layout", Categories); err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
	}
}

func getPosmortemByCategory(pms []*Postmortem, category string) []Postmortem {
	var ctpm []Postmortem

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
	ct := mux.Vars(r)["category"]

	pms, err := loadPostmortems(*dir)
	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)

		return
	}

	ctpm := getPosmortemByCategory(pms, ct)

	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", "category.html")

	_, err = os.Stat(fp)
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

	page := struct {
		Category    string
		Postmortems []Postmortem
	}{
		Category:    ct,
		Postmortems: ctpm,
	}

	if err := tmpl.ExecuteTemplate(w, "layout", page); err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
	}
}

func postmortemPageHandler(w http.ResponseWriter, r *http.Request) {
	pmID := mux.Vars(r)["id"]

	pm, err := loadPostmortem(*dir, pmID+".md")
	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
	}

	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", "postmortem.html")

	_, err = os.Stat(fp)
	if err != nil {
		if os.IsNotExist(err) {
			// Template doesn't exist, return 404.
			http.NotFound(w, r)
			return
		}
	}

	// Convert Markdown formatting of descriptions to HTML.
	htmlDesc := blackfriday.Run([]byte(pm.Description))
	pm.Description = string(htmlDesc)

	tmpl, err := template.ParseFiles(lp, fp)
	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)

		return
	}

	if err := tmpl.ExecuteTemplate(w, "layout", pm); err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
	}
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	pms, err := loadPostmortems(*dir)
	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)

		return
	}

	lp := filepath.Join("templates", "layout.html")
	fp := filepath.Join("templates", "index.html")

	_, err = os.Stat(fp)
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

	if err := tmpl.ExecuteTemplate(w, "layout", pms); err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(500), http.StatusInternalServerError)
	}
}
