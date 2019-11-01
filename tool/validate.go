package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

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
