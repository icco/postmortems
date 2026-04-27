// Command pm operates on the postmortem corpus and serves the website.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/icco/gutil/logging"
	"github.com/icco/postmortems"
	"github.com/icco/postmortems/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"
)

var (
	log    = logging.Must(logging.NewLogger(postmortems.Service))
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

const (
	usageText = `pm [options...]
Options:
-action     The action we should take.
-dir        The directory with Markdown files for to extract or parse. Defaults to ./data

Actions:
extract         Extract postmortems from the collection and create separate files.
upstream-fetch  Download and extract postmortems from https://github.com/danluu/post-mortems.
generate        Generate JSON files from the postmortem Markdown files.
new             Create a new postmortem file.
validate        Validate the postmortem files in the directory.
serve           Serve the postmortem files in a small website.
`
	danluuReadme = "https://raw.githubusercontent.com/danluu/post-mortems/master/README.md"
	extractFile  = "./tmp/posts.md"
)

// Serve runs the HTTP server with otelhttp metrics exposed on /metrics.
func Serve() error {
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return fmt.Errorf("otel prometheus exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(mp)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mp.Shutdown(ctx); err != nil {
			log.Warnw("meter provider shutdown", zap.Error(err))
		}
	}()

	router := server.New(server.Options{
		Logger:         log,
		MetricsHandler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		Dir:            *dir,
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Infow("Starting up", "host", fmt.Sprintf("http://localhost:%s", port))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if action == nil || *action == "" {
		log.Warnw("no action specified")
		usage()
		return
	}

	if dir == nil || *dir == "" {
		log.Warnw("no directory specified")
		usage()
		return
	}

	var err error
	switch *action {
	case "extract":
		err = postmortems.ExtractPostmortems(extractFile, *dir)
	case "upstream-fetch":
		err = postmortems.ExtractPostmortems(danluuReadme, *dir)
	case "generate":
		err = postmortems.GenerateJSON(*dir)
	case "new":
		err = newPostmortem(*dir)
	case "validate":
		_, err = postmortems.ValidateDir(*dir)
	case "serve":
		err = Serve()
	default:
		log.Fatalw("not a valid action", "action", *action)
	}

	if err != nil {
		log.Fatalw("running action failed", zap.Error(err))
	}
}

func usage() {
	fmt.Print(usageText)
	os.Exit(0)
}

func newPostmortem(dir string) error {
	pm := postmortems.New()

	err := survey.Ask(qs, pm)
	if errors.Is(err, terminal.InterruptErr) {
		fmt.Println("interrupted")
		os.Exit(0)
	} else if err != nil {
		return fmt.Errorf("couldn't ask question: %w", err)
	}

	return pm.Save(dir)
}

// IsURL validates that a value parses as an absolute URL.
func IsURL() survey.Validator {
	return func(val interface{}) error {
		str, ok := val.(string)
		if !ok {
			return fmt.Errorf("could not decode string")
		}
		if _, err := url.Parse(str); err != nil {
			return fmt.Errorf("value is not a valid URL: %w", err)
		}
		return nil
	}
}
