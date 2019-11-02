package main

import (
	"io"
	"time"

	"github.com/gernest/front"
)

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
