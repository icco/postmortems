package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

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

// Generate outputs all content in json for parsing by our website.
func Generate(d string) error {
	return fmt.Errorf("not implemented")
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

	m := front.NewMatter()
	m.Handle("---", front.YAMLHandler)
	fm, body, err := m.Parse(f)
	if err != nil {
		return err
	}

	if url, ok := fm["url"].(string); !ok || url == "" {
		return fmt.Errorf("%s: url is empty", filename)
	}

	if cats, ok := fm["categories"].([]interface{}); ok {
		for _, c := range cats {
			if cat, ok := c.(string); ok {
				if !CategoriesContain(cat) {
					return fmt.Errorf("%s: %s is not a valid category", filename, cat)
				}
			}
		}
	}

	if body == "" {
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
