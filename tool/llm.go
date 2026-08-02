package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/icco/gutil/vertex"
	"github.com/icco/postmortems"
	"google.golang.org/genai"
)

// EnrichInput is the context handed to the LLM for one postmortem.
type EnrichInput struct {
	URL         string
	Company     string
	Existing    *postmortems.Postmortem
	PageTitle   string
	PageDate    time.Time
	PageText    string
	UsedArchive bool
}

// EnrichOutput is the structured result the LLM returns. Fields are
// empty/zero when the model couldn't ground them.
type EnrichOutput struct {
	Title               string
	Product             string
	StartTime           time.Time
	EndTime             time.Time
	Keywords            []string
	ExpandedDescription string
	Confidence          string
	Notes               string
}

// LLMClient lets tests stub the Gemini call.
type LLMClient interface {
	Enrich(ctx context.Context, in EnrichInput) (EnrichOutput, error)
	Close() error
}

type geminiClient struct {
	v *vertex.Client
}

// NewGeminiClient builds a Vertex AI client. Uses ADC for auth.
func NewGeminiClient(ctx context.Context, project, location, modelName string) (LLMClient, error) {
	if project == "" {
		return nil, fmt.Errorf("gcp project is required (set -gcp-project or GOOGLE_CLOUD_PROJECT)")
	}

	v, err := vertex.New(ctx, vertex.Config{
		Project:  project,
		Location: cmp.Or(location, vertex.DefaultLocation),
		Model:    cmp.Or(modelName, vertex.DefaultModel),
	})
	if err != nil {
		return nil, fmt.Errorf("create vertex client: %w", err)
	}
	return &geminiClient{v: v}, nil
}

func (g *geminiClient) Close() error { return nil }

// Enrich asks Gemini for structured metadata for one postmortem.
func (g *geminiClient) Enrich(ctx context.Context, in EnrichInput) (EnrichOutput, error) {
	resp, err := g.v.Generate(ctx, vertex.Request{
		Parts:       vertex.Text(buildPrompt(in)),
		Schema:      enrichSchema(),
		Temperature: vertex.Temperature(0.2),
	})
	switch {
	case errors.Is(err, vertex.ErrEmptyResponse):
		return EnrichOutput{}, fmt.Errorf("empty response from gemini")
	case err != nil:
		return EnrichOutput{}, fmt.Errorf("generate: %w", err)
	}
	raw := resp.Text

	// Unmarshalled here rather than via vertex.GenerateJSON so a model that
	// answers with prose shows up in the error. These pages are arbitrary
	// scraped HTML; knowing what came back is most of the debugging.
	var parsed struct {
		Title               string   `json:"title"`
		Product             string   `json:"product"`
		StartTime           string   `json:"start_time"`
		EndTime             string   `json:"end_time"`
		Keywords            []string `json:"keywords"`
		ExpandedDescription string   `json:"expanded_description"`
		Confidence          string   `json:"confidence"`
		Notes               string   `json:"notes"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return EnrichOutput{}, fmt.Errorf("parse llm json (%q): %w", truncate(raw, 200), err)
	}

	out := EnrichOutput{
		Title:               strings.TrimSpace(parsed.Title),
		Product:             strings.TrimSpace(parsed.Product),
		Keywords:            cleanStrings(parsed.Keywords),
		ExpandedDescription: strings.TrimSpace(parsed.ExpandedDescription),
		Confidence:          strings.ToLower(strings.TrimSpace(parsed.Confidence)),
		Notes:               strings.TrimSpace(parsed.Notes),
	}
	if t, ok := tryParseTime(parsed.StartTime); ok {
		out.StartTime = t
	}
	if t, ok := tryParseTime(parsed.EndTime); ok {
		out.EndTime = t
	}
	if !out.StartTime.IsZero() && !out.EndTime.IsZero() && out.EndTime.Before(out.StartTime) {
		// Surface the inversion as a no-op rather than corrupting the
		// file; the merge layer will leave both unchanged.
		out.StartTime = time.Time{}
		out.EndTime = time.Time{}
	}
	return out, nil
}

// cleanStrings trims, drops empties, and dedupes.
func cleanStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// buildPrompt constructs the user message for the LLM.
func buildPrompt(in EnrichInput) string {
	var sb strings.Builder
	sb.WriteString("You are extracting structured metadata about a published postmortem (incident write-up).\n")
	sb.WriteString("Read the article text below and respond with a single JSON object that strictly matches the schema.\n\n")

	sb.WriteString("Rules:\n")
	sb.WriteString("- Use ONLY information present in the article. Do not invent dates, products, or names.\n")
	sb.WriteString("- start_time and end_time refer to the INCIDENT itself (when the outage/issue began and was resolved), not when the article was published. Use RFC3339 (e.g. 2017-02-28T17:37:00Z). Leave empty if unknown.\n")
	sb.WriteString("- product is the specific service or feature that failed (e.g. \"S3\", \"GitHub Pages\", \"BigQuery\"). Leave empty if the article only names the company.\n")
	sb.WriteString("- expanded_description is markdown, 3 to 6 short paragraphs, neutral tone, covering: timeline, what failed, root cause, customer impact, and remediation. Do not include headings or front-matter.\n")
	sb.WriteString("- title is a concise human-readable name for the incident, e.g. \"AWS S3 us-east-1 outage of February 2017\".\n")
	sb.WriteString("- keywords is up to 8 short tags useful for indexing (services, regions, technologies). Lowercase, no punctuation.\n")
	sb.WriteString("- confidence is one of \"low\", \"medium\", \"high\" reflecting how grounded your answers are in the source.\n")
	sb.WriteString("- notes is optional free text explaining anything that needed to be left blank.\n\n")

	sb.WriteString("Existing entry context:\n")
	fmt.Fprintf(&sb, "- Company: %s\n", cmp.Or(in.Company, "(unknown)"))
	fmt.Fprintf(&sb, "- Source URL: %s\n", in.URL)
	if in.Existing != nil {
		if in.Existing.Title != "" {
			fmt.Fprintf(&sb, "- Existing title: %s\n", in.Existing.Title)
		}
		if in.Existing.Product != "" {
			fmt.Fprintf(&sb, "- Existing product: %s\n", in.Existing.Product)
		}
		if in.Existing.Description != "" {
			fmt.Fprintf(&sb, "- Existing one-line summary: %s\n", strings.TrimSpace(in.Existing.Description))
		}
	}
	if in.UsedArchive {
		sb.WriteString("- Note: source was retrieved from the Wayback Machine because the original URL is dead.\n")
	}
	if !in.PageDate.IsZero() {
		fmt.Fprintf(&sb, "- Page published date (publication, not incident): %s\n", in.PageDate.UTC().Format(time.RFC3339))
	}
	if in.PageTitle != "" {
		fmt.Fprintf(&sb, "- Page title: %s\n", in.PageTitle)
	}

	sb.WriteString("\nArticle text (may be truncated):\n")
	sb.WriteString("---\n")
	sb.WriteString(in.PageText)
	sb.WriteString("\n---\n")
	return sb.String()
}

// enrichSchema is the OpenAPI schema for the Gemini JSON response.
func enrichSchema() *genai.Schema {
	str := func(desc string) *genai.Schema {
		return &genai.Schema{Type: genai.TypeString, Description: desc}
	}
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"title":                str("Concise human-readable incident title; empty if not derivable."),
			"product":              str("Specific failing service/feature; empty if unknown."),
			"start_time":           str("RFC3339 incident start time; empty if unknown."),
			"end_time":             str("RFC3339 incident end time; empty if unknown."),
			"expanded_description": str("Markdown description, 3-6 short paragraphs."),
			"confidence":           str("One of low/medium/high."),
			"notes":                str("Optional free-text notes."),
			"keywords": {
				Type:        genai.TypeArray,
				Description: "Up to 8 short lowercase tags.",
				Items:       &genai.Schema{Type: genai.TypeString},
			},
		},
		Required: []string{"expanded_description", "confidence"},
	}
}
