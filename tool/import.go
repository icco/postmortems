package main

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/icco/postmortems"
)

// danluuReadme is the default upstream source.
const danluuReadme = "https://raw.githubusercontent.com/danluu/post-mortems/master/README.md"

// importOptions configures one RunImport call.
type importOptions struct {
	Dir      string
	Source   string // URL or file path; empty means danluuReadme
	NoEnrich bool
	Enrich   enrichOptions
	Logger   *slog.Logger
}

// importResult is the consolidated outcome of an import + enrich pass.
type importResult struct {
	*postmortems.ImportReport
	Enriched      []enrichResult
	EnrichSkipped string // empty if enrich ran; otherwise the reason
}

// RunImport pulls the source list, additively saves any new entries,
// and (unless disabled) enriches just those new entries. Existing
// entries are never modified, so it is safe to run on a timer.
func RunImport(ctx context.Context, opts importOptions) (*importResult, error) {
	opts.Source = cmp.Or(opts.Source, danluuReadme)
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
