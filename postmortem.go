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
//
// StartTime and EndTime together describe the "Event Date Period" the
// postmortem is about (the from/to of the incident itself). Either may
// be the zero value to indicate that bound is unknown.
//
// The Source* fields and ArchiveURL are populated by the `enrich` tool
// from the upstream URL; they are omitempty so legacy files without
// them round-trip cleanly. Summary holds the original short blurb when
// the description body is rewritten with a longer extract from the
// source page.
type Postmortem struct {
	UUID              string    `yaml:"uuid"`
	URL               string    `yaml:"url"`
	ArchiveURL        string    `yaml:"archive_url,omitempty"`
	Title             string    `yaml:"title,omitempty"`
	StartTime         time.Time `yaml:"start_time,omitempty"`
	EndTime           time.Time `yaml:"end_time,omitempty"`
	Categories        []string  `yaml:"categories"`
	Keywords          []string  `yaml:"keywords,omitempty"`
	Company           string    `yaml:"company"`
	Product           string    `yaml:"product"`
	SourceAuthor      string    `yaml:"source_author,omitempty"`
	SourcePublishedAt time.Time `yaml:"source_published_at,omitempty"`
	SourceFetchedAt   time.Time `yaml:"source_fetched_at,omitempty"`
	Summary           string    `yaml:"summary,omitempty"`
	Description       string    `yaml:"-"`
}

// HasEventDates reports whether the postmortem has any event date
// information (start, end, or both).
func (pm *Postmortem) HasEventDates() bool {
	return !pm.StartTime.IsZero() || !pm.EndTime.IsZero()
}

// EventDatePeriod returns a human-readable rendering of the event
// from/to dates suitable for display next to a postmortem. Returns the
// empty string if neither bound is set, so callers (templates) can
// safely render it unconditionally.
func (pm *Postmortem) EventDatePeriod() string {
	const layout = "2006-01-02"
	s, e := pm.StartTime.UTC(), pm.EndTime.UTC()
	switch {
	case pm.StartTime.IsZero() && pm.EndTime.IsZero():
		return ""
	case pm.StartTime.IsZero():
		return "until " + e.Format(layout)
	case pm.EndTime.IsZero():
		return s.Format(layout)
	case s.Format(layout) == e.Format(layout):
		return s.Format(layout)
	default:
		return s.Format(layout) + " \u2013 " + e.Format(layout)
	}
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

// parseFrontmatterTime accepts the various shapes a YAML scalar can take
// for a date field (time.Time, an empty/missing value, or a string) and
// returns the parsed time.Time. The zero time is returned for missing
// or empty values; this matches the existing on-disk convention of
// `start_time: ""` meaning "unknown".
func parseFrontmatterTime(v interface{}) (time.Time, error) {
	switch x := v.(type) {
	case nil:
		return time.Time{}, nil
	case time.Time:
		return x, nil
	case string:
		if x == "" {
			return time.Time{}, nil
		}
		layouts := []string{time.RFC3339, "2006-01-02", "2006-01-02T15:04:05"}
		for _, l := range layouts {
			if t, err := time.Parse(l, x); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("could not parse time %q", x)
	default:
		return time.Time{}, fmt.Errorf("unexpected type %T for time field", v)
	}
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

	if uuid, ok := fm["uuid"].(string); ok {
		p.UUID = uuid
	}

	if t, err := parseFrontmatterTime(fm["start_time"]); err == nil {
		p.StartTime = t
	} else {
		return nil, fmt.Errorf("start_time: %w", err)
	}

	if t, err := parseFrontmatterTime(fm["end_time"]); err == nil {
		p.EndTime = t
	} else {
		return nil, fmt.Errorf("end_time: %w", err)
	}

	if t, err := parseFrontmatterTime(fm["source_published_at"]); err == nil {
		p.SourcePublishedAt = t
	} else {
		return nil, fmt.Errorf("source_published_at: %w", err)
	}

	if t, err := parseFrontmatterTime(fm["source_fetched_at"]); err == nil {
		p.SourceFetchedAt = t
	} else {
		return nil, fmt.Errorf("source_fetched_at: %w", err)
	}

	if url, ok := fm["url"].(string); ok {
		p.URL = url
	}

	if archiveURL, ok := fm["archive_url"].(string); ok {
		p.ArchiveURL = archiveURL
	}

	if title, ok := fm["title"].(string); ok {
		p.Title = title
	}

	if company, ok := fm["company"].(string); ok {
		p.Company = company
	}

	if product, ok := fm["product"].(string); ok {
		p.Product = product
	}

	if author, ok := fm["source_author"].(string); ok {
		p.SourceAuthor = author
	}

	if summary, ok := fm["summary"].(string); ok {
		p.Summary = summary
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
