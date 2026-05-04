package main

import (
	"reflect"
	"testing"
)

func TestMatchCategories(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		existing []string
		want     []string
	}{
		{
			name:     "cloud and config",
			body:     "We rolled out a bad config to our AWS EC2 fleet.",
			existing: []string{"postmortem"},
			want:     []string{"cloud", "config-change"},
		},
		{
			name:     "skip already-present categories",
			body:     "Cascading failure across the cluster after a misconfiguration.",
			existing: []string{"postmortem", "cascading-failure"},
			want:     []string{"config-change"},
		},
		{
			name:     "no matches",
			body:     "Nothing in this body should trigger anything.",
			existing: []string{"postmortem"},
			want:     nil,
		},
		{
			name:     "ntp triggers time",
			body:     "An NTP misconfiguration confused our log timestamps.",
			existing: []string{"postmortem"},
			want:     []string{"config-change", "time"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := matchCategories(tc.body, tc.existing)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("matchCategories() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeCategories(t *testing.T) {
	// Order is the declaration order in postmortems.Categories.
	got := mergeCategories([]string{"postmortem"}, []string{"cloud", "config-change"})
	want := []string{"cloud", "config-change", "postmortem"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCategories() = %v, want %v", got, want)
	}

	got = mergeCategories([]string{"postmortem", "hardware"}, []string{"hardware", "cloud"})
	want = []string{"cloud", "postmortem", "hardware"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCategories() with overlap = %v, want %v", got, want)
	}
}
