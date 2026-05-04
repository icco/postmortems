package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/icco/postmortems"
	"google.golang.org/genai"
)

// EnrichInput is the bundle of context handed to the LLM. The LLM is
// instructed to ground its output in PageText and to cite PageTitle
// when relevant.
type EnrichInput struct {
	URL         string
	Company     string
	Existing    *postmortems.Postmortem
	PageTitle   string
	PageAuthor  string
	PageDate    time.Time
	PageText    string
	UsedArchive bool
}

// EnrichOutput is the structured result the LLM is asked to return.
// All fields are optional from the model's perspective; we instruct it
// to leave a field empty/zero rather than guess. Confidence is a free
// text label ("low" / "medium" / "high") that we surface in the report
// but don't otherwise act on.
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

// LLMClient abstracts the Gemini call so the orchestrator can be unit
// tested with a fake implementation that returns canned results.
type LLMClient interface {
	Enrich(ctx context.Context, in EnrichInput) (EnrichOutput, error)
	Close() error
}

// geminiClient is a thin wrapper around google.golang.org/genai
// configured for Vertex AI. Construction picks up Application Default
// Credentials, so callers only need GOOGLE_APPLICATION_CREDENTIALS (or
// `gcloud auth application-default login`) plus a project + location.
type geminiClient struct {
	client *genai.Client
	model  string
	cfg    *genai.GenerateContentConfig
}

// NewGeminiClient dials the Vertex AI endpoint for project/location and
// configures the named model to emit JSON matching enrichSchema.
func NewGeminiClient(ctx context.Context, project, location, modelName string) (LLMClient, error) {
	if project == "" {
		return nil, fmt.Errorf("gcp project is required (set -gcp-project or GOOGLE_CLOUD_PROJECT)")
	}
	if location == "" {
		location = "us-central1"
	}
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  project,
		Location: location,
	})
	if err != nil {
		return nil, fmt.Errorf("genai client: %w", err)
	}

	temp := float32(0.2)
	cfg := &genai.GenerateContentConfig{
		Temperature:      &temp,
		ResponseMIMEType: "application/json",
		ResponseSchema:   enrichSchema(),
	}

	return &geminiClient{client: client, model: modelName, cfg: cfg}, nil
}

func (g *geminiClient) Close() error { return nil }

// Enrich asks Gemini to extract structured metadata for a single
// postmortem and returns the parsed result. Empty/zero fields in the
// returned EnrichOutput indicate the model was unable to ground a
// value, not that it failed.
func (g *geminiClient) Enrich(ctx context.Context, in EnrichInput) (EnrichOutput, error) {
	prompt := buildPrompt(in)
	resp, err := g.client.Models.GenerateContent(ctx, g.model, []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}, g.cfg)
	if err != nil {
		return EnrichOutput{}, fmt.Errorf("generate: %w", err)
	}
	raw := strings.TrimSpace(resp.Text())
	if raw == "" {
		return EnrichOutput{}, fmt.Errorf("empty response from gemini")
	}

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

// cleanStrings trims whitespace, drops empties, and dedupes (keeping
// first occurrence) so noisy keyword arrays from the LLM round-trip
// cleanly into YAML.
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

// buildPrompt constructs the user message for the LLM. The schema we
// configured on the model already constrains the output shape, but we
// repeat the contract here so the model can reason about field
// semantics (especially "leave empty if unsure").
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
	fmt.Fprintf(&sb, "- Company: %s\n", nonEmpty(in.Company))
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
	if in.PageAuthor != "" {
		fmt.Fprintf(&sb, "- Page author: %s\n", in.PageAuthor)
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

func nonEmpty(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}

// enrichSchema describes the JSON shape we want Gemini to return. The
// SDK forwards this as the OpenAPI schema portion of the request and
// rejects responses that don't match — meaning we don't have to do
// schema validation ourselves.
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
