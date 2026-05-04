package main

import (
	"regexp"
	"slices"
	"sort"

	"github.com/icco/postmortems"
)

// Category names used by both the matcher map and tests. Mirrors the
// strings in postmortems.Categories.
const (
	catAutomation       = "automation"
	catCascadingFailure = "cascading-failure"
	catCloud            = "cloud"
	catConfigChange     = "config-change"
	catHardware         = "hardware"
	catSecurity         = "security"
	catTime             = "time"
	catPostmortem       = "postmortem"
)

// categoryPatterns maps a category name in postmortems.Categories to a
// set of case-insensitive regular expressions. If any pattern matches
// the body of a postmortem's source URL the category is suggested.
//
// We deliberately omit "postmortem" (every entry already has it) and
// "undescriptive" (a subjective judgement that should not be
// auto-applied).
var categoryPatterns = map[string][]*regexp.Regexp{
	catAutomation: {
		mustRegex(`\bautomation\b`),
		mustRegex(`\bautomated\b`),
		mustRegex(`\bauto[- ]?scaling\b`),
	},
	catCascadingFailure: {
		mustRegex(`\bcascad(e|ing)\b`),
		mustRegex(`\bdomino\b`),
		mustRegex(`\bthundering herd\b`),
	},
	catCloud: {
		mustRegex(`\baws\b`),
		mustRegex(`\bamazon web services\b`),
		mustRegex(`\bgcp\b`),
		mustRegex(`\bgoogle cloud\b`),
		mustRegex(`\bazure\b`),
		mustRegex(`\bec2\b`),
		mustRegex(`\bs3\b`),
		mustRegex(`\bkubernetes\b`),
		mustRegex(`\bcloud provider\b`),
	},
	catConfigChange: {
		mustRegex(`\bconfig(uration)? change\b`),
		mustRegex(`\bbad config\b`),
		mustRegex(`\bmisconfigur(ation|ed)\b`),
		mustRegex(`\bdeploy(ment)?\b`),
		mustRegex(`\brollout\b`),
	},
	catHardware: {
		mustRegex(`\bhardware (failure|fault|issue)\b`),
		mustRegex(`\bdisk (failure|fault|fail)\b`),
		mustRegex(`\bssd (failure|fault)\b`),
		mustRegex(`\bnetwork card\b`),
		mustRegex(`\brouter (failure|fault)\b`),
		mustRegex(`\bpower (failure|outage|loss)\b`),
		mustRegex(`\bdata ?cent(re|er) (failure|outage)\b`),
	},
	catSecurity: {
		mustRegex(`\bsecurity (incident|breach|advisory)\b`),
		mustRegex(`\bvulnerab(le|ility)\b`),
		mustRegex(`\bexploit(ed|ation)?\b`),
		mustRegex(`\bbreach\b`),
		mustRegex(`\bleaked? credential\b`),
		mustRegex(`\bcve-\d{4}-\d+`),
	},
	catTime: {
		mustRegex(`\bntp\b`),
		mustRegex(`\btimezone\b`),
		mustRegex(`\bleap second\b`),
		mustRegex(`\bdaylight saving\b`),
		mustRegex(`\bclock skew\b`),
	},
}

func mustRegex(p string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + p)
}

// matchCategories returns the categories whose patterns match the body
// and that are not already in existing.
func matchCategories(body string, existing []string) []string {
	have := map[string]bool{}
	for _, c := range existing {
		have[c] = true
	}

	var out []string
	for _, cat := range postmortems.Categories {
		patterns, ok := categoryPatterns[cat]
		if !ok {
			continue
		}
		if have[cat] {
			continue
		}
		for _, p := range patterns {
			if p.MatchString(body) {
				out = append(out, cat)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// mergeCategories returns the union of existing and suggestions in a
// deterministic order driven by postmortems.Categories. Anything not in
// that whitelist (including legacy free-form tags) keeps its original
// relative position at the end of the list.
func mergeCategories(existing, suggestions []string) []string {
	have := map[string]bool{}
	for _, c := range existing {
		have[c] = true
	}
	for _, c := range suggestions {
		have[c] = true
	}
	var out []string
	for _, c := range postmortems.Categories {
		if have[c] {
			out = append(out, c)
		}
	}
	for _, c := range existing {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}
