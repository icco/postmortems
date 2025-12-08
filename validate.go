package postmortems

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// ValidateDir takes a directory path and validates every file in there.
func ValidateDir(d string) ([]*Postmortem, error) {
	var ret []*Postmortem
	err := filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
		// Failed to open path
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

// ValidateFile takes a file path and validates just that file.
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

	if p.Description == "" {
		return nil, fmt.Errorf("%s: description is empty", filename)
	}

	return p, nil
}

// CategoriesContain takes a string and decides
// if it is in the category whitelist.
func CategoriesContain(e string) bool {
	for _, a := range Categories {
		if a == e {
			return true
		}
	}

	return false
}
