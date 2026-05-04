package postmortems

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// ValidateDir validates every file under d.
func ValidateDir(d string) ([]*Postmortem, error) {
	var ret []*Postmortem
	err := filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			p, err := ValidateFile(path)
			if err != nil {
				return err
			}
			ret = append(ret, p)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return ret, err
}

// ValidateFile validates a single postmortem file.
func ValidateFile(filename string) (*Postmortem, error) {
	f, err := os.Open(filename) // #nosec G304
	if err != nil {
		return nil, err
	}

	p, err := Parse(f)
	if err != nil {
		return nil, err
	}

	if p.URL == "" {
		return nil, fmt.Errorf("%s: url is empty", filename)
	}

	_, err = url.Parse(p.URL)
	if err != nil {
		return nil, err
	}

	for _, cat := range p.Categories {
		if !CategoriesContain(cat) {
			return nil, fmt.Errorf("%s: %s is not a valid category", filename, cat)
		}
	}

	for _, kw := range p.Keywords {
		if kw == "" {
			return nil, fmt.Errorf("%s: keyword is empty", filename)
		}
	}

	if p.Description == "" {
		return nil, fmt.Errorf("%s: description is empty", filename)
	}

	return p, nil
}

// CategoriesContain reports whether e is in the category whitelist.
func CategoriesContain(e string) bool {
	for _, a := range Categories {
		if a == e {
			return true
		}
	}

	return false
}
