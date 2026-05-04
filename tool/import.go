package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/icco/postmortems"
)

// danluuReadme is the canonical upstream postmortem list. Imports
// without a -source flag pull from this URL.
const danluuReadme = "https://raw.githubusercontent.com/danluu/post-mortems/master/README.md"

// importOptions configures one RunImport call. NoEnrich and missing
// LLMs are both treated as "skip the enrich step" so the action stays
// useful in CI/cron environments without GCP credentials.
type importOptions struct {
	Dir      string
	Source   string // URL or file path; empty means danluu upstream
	NoEnrich bool

	// Enrich is applied (with Apply=true and Only set to the
	// freshly-added UUIDs) when NoEnrich is false and Enrich.LLM is
	// non-nil. Other Enrich fields (Force, KeepDescription, MaxAge,
	// HTTPTimeout, Concurrency, Logger) carry the caller's preferences
	// through unchanged.
	Enrich enrichOptions

	Logger *slog.Logger
}

// importResult is the consolidated outcome of an import + enrich pass.
type importResult struct {
	*postmortems.ImportReport
	Enriched      []enrichResult
	EnrichSkipped string // empty if enrich ran; otherwise the reason
}

// RunImport pulls the source list, additively saves any new entries,
// and (unless disabled) enriches just those new entries via the LLM.
// Existing entries are never modified, so this is safe to run on a
// timer.
func RunImport(ctx context.Context, opts importOptions) (*importResult, error) {
	if opts.Source == "" {
		opts.Source = danluuReadme
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	opts.Logger.Info("import starting", "source", opts.Source, "dir", opts.Dir)

	report, err := postmortems.ExtractPostmortems(opts.Source, opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	out := &importResult{ImportReport: report}

	opts.Logger.Info("import added entries",
		"added", len(report.Added),
		"skipped_existing", report.SkippedExisting,
		"skipped_invalid", report.SkippedInvalid,
	)

	if len(report.Added) == 0 {
		out.EnrichSkipped = "no new entries"
		return out, nil
	}
	if opts.NoEnrich {
		out.EnrichSkipped = "no-enrich flag set"
		return out, nil
	}
	if opts.Enrich.LLM == nil {
		opts.Logger.Warn("LLM client not configured; skipping enrich step (set GOOGLE_APPLICATION_CREDENTIALS + GOOGLE_CLOUD_PROJECT or pass -no-enrich)")
		out.EnrichSkipped = "no llm client"
		return out, nil
	}

	uuids := make([]string, 0, len(report.Added))
	for _, pm := range report.Added {
		uuids = append(uuids, pm.UUID)
	}
	enrichOpts := opts.Enrich
	enrichOpts.Dir = opts.Dir
	enrichOpts.Apply = true
	enrichOpts.Only = strings.Join(uuids, ",")
	if enrichOpts.Logger == nil {
		enrichOpts.Logger = opts.Logger
	}

	res, err := EnrichPostmortems(ctx, enrichOpts)
	if err != nil {
		return out, fmt.Errorf("enrich: %w", err)
	}
	out.Enriched = res
	LogEnrichReport(enrichOpts.Logger, res, true)
	return out, nil
}
