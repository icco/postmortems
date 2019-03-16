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

func Generate(d string) error {
	return fmt.Errorf("not implemented")
}

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

func ValidateFile(filename string) error {
	//log.Printf("visited file: %q", filename)

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

	//log.Printf("%s: fm: %+v body: %+v", filename, fm, body)
	if url, ok := fm["url"].(string); !ok || url == "" {
		return fmt.Errorf("%s: URL is empty.", filename)
	}

	if body == "" {
		return fmt.Errorf("%s: Body / Description is empty.", filename)
	}

	return nil
}
