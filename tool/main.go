package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/icco/postmortems"
	"github.com/icco/postmortems/server"
)

var (
	action = flag.String("action", "", "")
	dir    = flag.String("dir", "./data/", "")
	qs     = []*survey.Question{
		{
			Name:     "url",
			Prompt:   &survey.Input{Message: "URL of Postmortem?"},
			Validate: survey.ComposeValidators(survey.Required, IsURL()),
		},
		{
			Name:      "company",
			Prompt:    &survey.Input{Message: "Company?"},
			Validate:  survey.Required,
			Transform: survey.Title,
		},
		{
			Name:     "description",
			Prompt:   &survey.Multiline{Message: "Short summary (in markdown):"},
			Validate: survey.Required,
		},
		{
			Name:   "product",
			Prompt: &survey.Input{Message: "Product?"},
		},
		{
			Name: "categories",
			Prompt: &survey.MultiSelect{
				Message:  "Select categories:",
				Options:  postmortems.Categories,
				Default:  "postmortem",
				PageSize: len(postmortems.Categories),
			},
		},
	}
)

const usageText = `pm [options...]
Options:
-action     The action we should take.
-dir        The directory with Markdown files for to extract or parse. Defaults to ./data

Actions:
extract     Extract postmortems from the collection and create separate files.
generate    Generate JSON files from the postmortem Markdown files.
new         Create a new postmortem file.
validate    Validate the postmortem files in the directory.
serve       Serve the postmortem files in a small website.
`

// Serve serves the content of the website.
func Serve() error {
	router := server.New(dir)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server listening on http://0.0.0.0:%s", port)

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
		err = postmortems.GenerateJSON(*dir)
	case "new":
		err = newPostmortem(*dir)
	case "validate":
		_, err = postmortems.ValidateDir(*dir)
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

func newPostmortem(dir string) error {
	pm := postmortems.New()

	err := survey.Ask(qs, pm)
	if err == terminal.InterruptErr {
		fmt.Println("interrupted")
		os.Exit(0)
	} else if err != nil {
		return fmt.Errorf("couldn't ask question: %w", err)
	}

	return pm.Save(dir)
}

// IsURL creates a validator that makes sure it's a parsable URL.
func IsURL() survey.Validator {
	return func(val interface{}) error {
		str, ok := val.(string)
		if !ok {
			return fmt.Errorf("could not decode string")
		}

		_, err := url.Parse(str)
		if err != nil {
			return fmt.Errorf("value is not a valid URL: %w", err)
		}

		return nil
	}
}
