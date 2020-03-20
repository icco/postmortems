package postmortems

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/gernest/front"
	"github.com/goccy/go-yaml"
)

// Postmortem is a structural representation of a postmortem summary and its
// metadata.
type Postmortem struct {
	UUID        string    `yaml:"uuid"`
	URL         string    `yaml:"url"`
	StartTime   time.Time `yaml:"start_time"`
	EndTime     time.Time `yaml:"end_time"`
	Categories  []string  `yaml:"categories"`
	Company     string    `yaml:"company"`
	Product     string    `yaml:"product"`
	Description string    `yaml:"-"`
}

var (
	// Categories is a whitelist of valid categories that a postmortem can have.
	Categories = []string{
		"automation",
		"cascading-failure",
		"cloud",
		"config-change",
		"postmortem",
		"hardware",
		"security",
		"time",
		"undescriptive",
	}
)

// Parse turns an io stream into a Postmortem type.
func Parse(f io.Reader) (*Postmortem, error) {
	p := &Postmortem{}

	m := front.NewMatter()
	m.Handle("---", front.YAMLHandler)

	fm, body, err := m.Parse(f)
	if err != nil {
		return nil, err
	}

	if uuid, ok := fm["uuid"].(string); ok {
		p.UUID = uuid
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

// GenerateJSON outputs all content in JSON for parsing by our website.
func GenerateJSON(d string) error {
	baseDir := "./output"

	err := os.MkdirAll(baseDir, os.ModePerm)
	if err != nil {
		return err
	}

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

func ToYaml(pm *Postmortem) (string, error) {
	bytes, err := yaml.Marshal(pm)
	return string(bytes), err
}
