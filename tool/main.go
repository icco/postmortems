package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

var (
	action = flag.String("action", "", "")
	dir    = flag.String("dir", "./data/", "")

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

// Generate outputs all content in JSON for parsing by our website.
func Generate(d string) error {
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

func main() {
	flag.Usage = usage
	flag.Parse()

	if action == nil || *action == "" {
		fmt.Println("no action specified")
		usage()
		return
	}

	if dir == nil || *dir == "" {
		fmt.Println("no directory specified")
		usage()
		return
	}

	var err error

	switch *action {
	case "extract":
		err = ExtractPostmortems(*dir)
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

func usage() {
	fmt.Println(usageText)
	os.Exit(0)
}

var usageText = `pm [options...]
Options:
-action     The action we should take. The three valid options are extract, generate & validate.
-dir        The directory with Markdown files for to extract or parse. Defaults to ./data

Actions:
extract     Extract postmortems from the collection and create separate files.
generate    Generate JSON files from the postmortem Markdown files.
validate    Validate the postmortem files in the directory.
`
