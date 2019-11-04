package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/icco/postmortems"
)

var (
	action = flag.String("action", "", "")
	dir    = flag.String("dir", "./data/", "")
)

// Serve serves the content of the website.
func Serve() error {
	router := server.New()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server listening on 0.0.0.0:%s\n", time.Now(), port)
	return http.ListenAndServe(":"+port, router)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if action == nil || *action == "" {
		log.Print("no action specified")
		usage()

		return
	}

	if dir == nil || *dir == "" {
		log.Print("no directory specified")
		usage()

		return
	}

	var err error

	switch *action {
	case "extract":
		err = postmortems.ExtractPostmortems(*dir)
	case "generate":
		err = postmortems.Generate(*dir)
	case "validate":
		err = postmortems.ValidateDir(*dir)
	case "serve":
		err = Serve()
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
serve       Serve the postmortem files in a small website.
`
