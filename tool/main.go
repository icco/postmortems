// Command pm operates on the postmortem corpus and serves the website.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
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
	log            = logging.Must(logging.NewLogger(postmortems.Service))
	action         = flag.String("action", "", "")
	dir            = flag.String("dir", "./data/", "")
	importSource   = flag.String("source", "", "import: URL or file path to read entries from (default: danluu/post-mortems README)")
	importNoEnrich = flag.Bool("no-enrich", false, "import: skip the enrich step on newly added entries")
	enrichApply    = flag.Bool("apply", false, "enrich: write changes back into the markdown files (default: dry run)")
	enrichTimeout  = flag.Duration("http-timeout", 15*time.Second, "enrich: per-URL fetch timeout")
	enrichOnly     = flag.String("only", "", "enrich: comma-separated list of UUID prefixes to process (default: all files)")
	enrichForce    = flag.Bool("force", false, "enrich: overwrite non-empty fields (default: only fill blanks)")
	enrichKeepDesc = flag.Bool("keep-description", false, "enrich: preserve existing markdown body, only refresh metadata")
	enrichMaxAge   = flag.Duration("max-age", 720*time.Hour, "enrich: skip files whose source_fetched_at is newer than this")
	enrichWorkers  = flag.Int("enrich-workers", 4, "enrich: number of concurrent fetch+LLM workers")
	gcpProject     = flag.String("gcp-project", os.Getenv("GOOGLE_CLOUD_PROJECT"), "enrich: GCP project for Vertex AI (defaults to GOOGLE_CLOUD_PROJECT)")
	gcpLocation    = flag.String("gcp-location", "us-central1", "enrich: Vertex AI location/region")
	geminiModel    = flag.String("gemini-model", "gemini-2.5-flash", "enrich: Gemini model name")
	qs             = []*survey.Question{
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

const usageText = `pm [options...]
Options:
-action     The action we should take.
-dir        The directory with Markdown files for to extract or parse. Defaults to ./data

Actions:
import          Additively pull entries from -source (default: danluu/post-mortems
                README) and enrich any new ones via Gemini. Idempotent and safe to
                run repeatedly. Pass -source=PATH, -no-enrich, or any enrich flag.
generate        Generate JSON files from the postmortem Markdown files.
new             Create a new postmortem file.
validate        Validate the postmortem files in the directory.
serve           Serve the postmortem files in a small website.
enrich          Fetch each postmortem source URL (with Wayback fallback), extract metadata,
                run regex-based category suggestions, ask Gemini for incident
                times/product/expanded description, and write the merged result back.
                Requires GOOGLE_APPLICATION_CREDENTIALS and a GCP project. Pass -apply to
                write changes; -force to overwrite non-empty fields.
`

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
	case "import":
		err = runImport()
	case "generate":
		err = postmortems.GenerateJSON(*dir)
	case "new":
		err = newPostmortem(*dir)
	case "validate":
		_, err = postmortems.ValidateDir(*dir)
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

// runImport wires CLI flags into the import pipeline. The LLM is
// constructed best-effort: when credentials are missing the import
// still runs and the enrich step is skipped with a warning.
func runImport() error {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	opts := importOptions{
		Dir:      *dir,
		Source:   *importSource,
		NoEnrich: *importNoEnrich,
		Logger:   logger,
		Enrich: enrichOptions{
			Force:           *enrichForce,
			KeepDescription: *enrichKeepDesc,
			MaxAge:          *enrichMaxAge,
			HTTPTimeout:     *enrichTimeout,
			Concurrency:     *enrichWorkers,
			Logger:          logger,
		},
	}

	if !*importNoEnrich {
		llm, err := NewGeminiClient(ctx, *gcpProject, *gcpLocation, *geminiModel)
		if err != nil {
			logger.Warn("LLM unavailable, skipping enrich step", "err", err)
		} else {
			defer func() { _ = llm.Close() }()
			opts.Enrich.LLM = llm
		}
	}

	_, err := RunImport(ctx, opts)
	return err
}

// runEnrich wires CLI flags into the enrich pipeline.
func runEnrich() error {
	ctx := context.Background()
	llm, err := NewGeminiClient(ctx, *gcpProject, *gcpLocation, *geminiModel)
	if err != nil {
		return err
	}
	defer func() { _ = llm.Close() }()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	res, err := EnrichPostmortems(ctx, enrichOptions{
		Dir:             *dir,
		Only:            *enrichOnly,
		Apply:           *enrichApply,
		Force:           *enrichForce,
		KeepDescription: *enrichKeepDesc,
		MaxAge:          *enrichMaxAge,
		HTTPTimeout:     *enrichTimeout,
		Concurrency:     *enrichWorkers,
		LLM:             llm,
		Logger:          logger,
	})
	if err != nil {
		return err
	}
	LogEnrichReport(logger, res, *enrichApply)
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

// keywordsTransformer turns the comma-separated string entered into a
// survey.Input into a []string suitable for the Keywords field.
func keywordsTransformer(ans any) any {
	str, ok := ans.(string)
	if !ok {
		return ans
	}
	return splitCSV(str)
}

// IsURL validates that a value parses as an absolute URL.
func IsURL() survey.Validator {
	return func(val any) error {
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
