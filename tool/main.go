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
	"strings"
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
	log              = logging.Must(logging.NewLogger(postmortems.Service))
	action           = flag.String("action", "", "")
	dir              = flag.String("dir", "./data/", "")
	categorizeApply  = flag.Bool("apply", false, "categorize/enrich: write changes back into the markdown files (default: dry run)")
	categorizeWorker = flag.Int("workers", 8, "categorize: number of concurrent HTTP fetchers")
	categorizeTime   = flag.Duration("http-timeout", 15*time.Second, "categorize/enrich: per-URL fetch timeout")
	enrichOnly       = flag.String("only", "", "enrich: only process files whose name starts with this UUID prefix")
	enrichForce      = flag.Bool("force", false, "enrich: overwrite non-empty fields (default: only fill blanks)")
	enrichKeepDesc   = flag.Bool("keep-description", false, "enrich: preserve existing markdown body, only refresh metadata")
	enrichMaxAge     = flag.Duration("max-age", 720*time.Hour, "enrich: skip files whose source_fetched_at is newer than this")
	enrichWorkers    = flag.Int("enrich-workers", 4, "enrich: number of concurrent fetch+LLM workers")
	gcpProject       = flag.String("gcp-project", os.Getenv("GOOGLE_CLOUD_PROJECT"), "enrich: GCP project for Vertex AI (defaults to GOOGLE_CLOUD_PROJECT)")
	gcpLocation      = flag.String("gcp-location", "us-central1", "enrich: Vertex AI location/region")
	geminiModel      = flag.String("gemini-model", "gemini-2.5-flash", "enrich: Gemini model name")
	qs               = []*survey.Question{
		{
			Name:     "url",
			Prompt:   &survey.Input{Message: "URL of Postmortem?"},
			Validate: survey.ComposeValidators(survey.Required, IsURL()),
		},
		{
			Name:   "title",
			Prompt: &survey.Input{Message: "Title (optional, e.g. \"AWS S3 outage of 2017\")?"},
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
				Default:  catPostmortem,
				PageSize: len(postmortems.Categories),
			},
		},
		{
			Name: "keywords",
			Prompt: &survey.Input{
				Message: "Keywords (optional, comma-separated):",
				Help:    "Free-form tags, e.g. \"dns, bgp, eu-west-1\". Leave blank for none.",
			},
			Transform: keywordsTransformer,
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
categorize      Scrape each postmortem URL and suggest additional categories.
                Pass -apply to write suggestions back to the markdown files.
enrich          Fetch each postmortem source URL (with Wayback fallback), extract metadata,
                ask Gemini for incident times/product/expanded description, and write the
                merged result back. Requires GOOGLE_APPLICATION_CREDENTIALS and a GCP
                project. Pass -apply to write changes; -force to overwrite non-empty fields.
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
	case "categorize":
		var res []categorizeResult
		res, err = CategorizePostmortems(categorizeOptions{
			Dir:         *dir,
			Apply:       *categorizeApply,
			HTTPTimeout: *categorizeTime,
			Concurrency: *categorizeWorker,
		})
		if err == nil {
			printCategorizeReport(os.Stdout, res, *categorizeApply)
		}
	case "enrich":
		err = runEnrich()
	case "serve":
		err = Serve()
	default:
		log.Fatalw("not a valid action", "action", *action)
	}

	if err != nil {
		log.Fatalw("running action failed", zap.Error(err))
	}
}

// runEnrich wires the CLI flags into the enrich pipeline. Kept out of
// main() so the action's dependencies (Vertex AI client) aren't
// constructed for unrelated actions.
func runEnrich() error {
	ctx := context.Background()
	llm, err := NewGeminiClient(ctx, *gcpProject, *gcpLocation, *geminiModel)
	if err != nil {
		return err
	}
	defer func() { _ = llm.Close() }()

	res, err := EnrichPostmortems(ctx, enrichOptions{
		Dir:             *dir,
		Only:            *enrichOnly,
		Apply:           *categorizeApply,
		Force:           *enrichForce,
		KeepDescription: *enrichKeepDesc,
		MaxAge:          *enrichMaxAge,
		HTTPTimeout:     *categorizeTime,
		Concurrency:     *enrichWorkers,
		LLM:             llm,
	})
	if err != nil {
		return err
	}
	printEnrichReport(os.Stdout, res, *categorizeApply)
	return nil
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

// keywordsTransformer turns a comma-separated string entered into a
// survey.Input into a []string with whitespace trimmed and empty entries
// dropped, so the result can be assigned directly into the Keywords
// slice via survey.Ask.
func keywordsTransformer(ans interface{}) interface{} {
	str, ok := ans.(string)
	if !ok {
		return ans
	}
	var out []string
	for _, raw := range splitAndTrim(str, ",") {
		if raw == "" {
			continue
		}
		out = append(out, raw)
	}
	return out
}

// splitAndTrim splits s on sep and trims whitespace from each element.
// Defined here rather than pulling in strings.Split + a loop in two
// places to keep the keyword handling self-contained.
func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
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
