package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gernest/front"
)

var (
	action = flag.String("action", "", "The action we should take. The two valid options are generate & validate.")
	dir    = flag.String("dir", "./data/", "The directory with markdown files for us to parse.")

	// Categories is a whitelist of valid categories that a postmortem can have.
	Categories = [...]string{
		"automation",
		"cascading-failure",
		"cloud",
		"config-change",
		"postmortem",
		"security",
		"time",
		"undescriptive",
	}
)

func main() {
	flag.Parse()

	if action == nil || *action == "" {
		log.Fatal("no action specified")
		return
	}

	if dir == nil || *dir == "" {
		log.Fatal("no directory specified")
		return
	}

	var err error
	switch *action {
	case "generate":
		err = Generate(*dir)
	case "validate":
		err = ValidateDir(*dir)
	default:
		log.Fatalf("%s is not a valid action", *action)
	}

	if err != nil {
		log.Fatal(err)
	}
}

// Postmortem is a structural representation of a postmortem summary and its
// metadata.
type Postmortem struct {
	URL         string
	StartTime   time.Time
	EndTime     time.Time
	Categories  []string
	Company     string
	Product     string
	Description string
}

// Parse turns an io stream into a Postmortem type.
func Parse(f io.Reader) (*Postmortem, error) {
	p := &Postmortem{}

	m := front.NewMatter()
	m.Handle("---", front.YAMLHandler)
	fm, body, err := m.Parse(f)
	if err != nil {
		return nil, err
	}

	if startTime, ok := fm["start_time"].(time.Time); ok {
		p.StartTime = startTime
	}

	if endTime, ok := fm["end_time"].(time.Time); ok {
		p.EndTime = endTime
	}

	if url, ok := fm["url"].(string); ok {
		p.URL = url
	}

	if company, ok := fm["company"].(string); ok {
		p.Company = company
	}

	if product, ok := fm["product"].(string); ok {
		p.Product = product
	}

	if cats, ok := fm["categories"].([]interface{}); ok {
		for _, c := range cats {
			if cat, ok := c.(string); ok {
				p.Categories = append(p.Categories, cat)
			}
		}
	}

	p.Description = body

	return p, nil
}

// Generate outputs all content in json for parsing by our website.
func Generate(d string) error {
	baseDir := "./output"
	os.MkdirAll(baseDir, os.ModePerm)

	fp := filepath.Join(baseDir, "categories.json")
	j, err := json.Marshal(Categories)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(fp, j, 0644)
	if err != nil {
		return err
	}

	return filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
		// Failed to open path
		if err != nil {
			return err
		}

		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}

			fName := filepath.Base(path)
			extName := filepath.Ext(path)
			id := fName[:len(fName)-len(extName)]

			p, err := Parse(f)
			if err != nil {
				return err
			}

			fp := filepath.Join(baseDir, fmt.Sprintf("%s.json", id))
			j, err := json.Marshal(p)
			if err != nil {
				return err
			}
			err = ioutil.WriteFile(fp, j, 0644)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// ValidateDir takes a directory path and validates every file in there.
func ValidateDir(d string) error {
	return filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
		// Failed to open path
		if err != nil {
			return err
		}

		if !info.IsDir() {
			err = ValidateFile(path)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// ValidateFile takes a file path and validates just that file.
func ValidateFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}

	p, err := Parse(f)
	if err != nil {
		return err
	}

	if p.URL == "" {
		return fmt.Errorf("%s: url is empty", filename)
	}

	_, err = url.Parse(p.URL)
	if err != nil {
		return err
	}

	for _, cat := range p.Categories {
		if !CategoriesContain(cat) {
			return fmt.Errorf("%s: %s is not a valid category", filename, cat)
		}
	}

	if p.Description == "" {
		return fmt.Errorf("%s: description is empty", filename)
	}

	return nil
}

// CategoriesContain takes a string and decides if it is in the category
// whitelist.
func CategoriesContain(e string) bool {
	for _, a := range Categories {
		if a == e {
			return true
		}
	}

	return false
}
