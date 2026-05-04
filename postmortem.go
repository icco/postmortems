package postmortems

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/gernest/front"
	"github.com/goccy/go-yaml"
	guuid "github.com/google/uuid"
	"github.com/icco/gutil/logging"
)

// Postmortem is a postmortem summary plus its metadata.
type Postmortem struct {
	UUID        string    `yaml:"uuid"`
	URL         string    `yaml:"url"`
	StartTime   time.Time `yaml:"start_time,omitempty"`
	EndTime     time.Time `yaml:"end_time,omitempty"`
	Categories  []string  `yaml:"categories"`
	Keywords    []string  `yaml:"keywords,omitempty"`
	Company     string    `yaml:"company"`
	Product     string    `yaml:"product"`
	Description string    `yaml:"-"`
}

const categoryPostmortem = "postmortem"

var (
	// Categories is a whitelist of valid categories that a postmortem can have.
	Categories = []string{
		"automation",
		"cascading-failure",
		"cloud",
		"config-change",
		categoryPostmortem,
		"hardware",
		"security",
		"time",
		"undescriptive",
	}

	// Service defines the service this runs in on GCP.
	Service = "postmortems"

	log = logging.Must(logging.NewLogger(Service))
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

	if kws, ok := fm["keywords"].([]interface{}); ok {
		for _, k := range kws {
			if kw, ok := k.(string); ok {
				p.Keywords = append(p.Keywords, kw)
			}
		}
	}

	p.Description = body

	return p, nil
}

// GenerateJSON outputs all content in JSON for parsing by our website.
func GenerateJSON(d string) error {
	baseDir := "./output"

	err := os.MkdirAll(baseDir, 0755) // #nosec G301 -- world-readable so non-root serve process can traverse
	if err != nil {
		return err
	}

	fp := filepath.Join(baseDir, "categories.json")

	j, err := json.Marshal(Categories)
	if err != nil {
		return err
	}

	err = os.WriteFile(fp, j, 0644) // #nosec G306 -- world-readable so non-root serve process can read
	if err != nil {
		return err
	}

	return filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			f, err := os.Open(path) // #nosec G304,G122
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
			err = os.WriteFile(fp, j, 0644) // #nosec G306 -- world-readable so non-root serve process can read
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// ToYaml transforms a postmortem into yaml for the frontmatter.
func ToYaml(pm *Postmortem) (string, error) {
	bytes, err := yaml.Marshal(pm)
	return string(bytes), err
}

// New generates a new postmortem with a fresh uuid.
func New() *Postmortem {
	id := guuid.New()
	return &Postmortem{UUID: id.String()}
}

// Save writes pm to dir as <UUID>.md.
func (pm *Postmortem) Save(dir string) error {
	var data bytes.Buffer

	fm := template.New("PostmortemTemplate")
	fm = fm.Funcs(template.FuncMap{"yaml": ToYaml})

	fm, err := fm.Parse(bodyTmpl)
	if err != nil {
		return err
	}

	if err := fm.Execute(&data, pm); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, pm.UUID+".md"), data.Bytes(), 0600); err != nil {
		return fmt.Errorf("error writing file: %w", err)
	}

	log.Debugw("saved pm", "pm", pm, "data", data.String())

	return nil
}
