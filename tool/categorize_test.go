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
			existing: []string{catPostmortem},
			want:     []string{catCloud, catConfigChange},
		},
		{
			name:     "skip already-present categories",
			body:     "Cascading failure across the cluster after a misconfiguration.",
			existing: []string{catPostmortem, catCascadingFailure},
			want:     []string{catConfigChange},
		},
		{
			name:     "no matches",
			body:     "Nothing in this body should trigger anything.",
			existing: []string{catPostmortem},
			want:     nil,
		},
		{
			name:     "ntp triggers time",
			body:     "An NTP misconfiguration confused our log timestamps.",
			existing: []string{catPostmortem},
			want:     []string{catConfigChange, catTime},
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
	got := mergeCategories([]string{catPostmortem}, []string{catCloud, catConfigChange})
	want := []string{catCloud, catConfigChange, catPostmortem}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCategories() = %v, want %v", got, want)
	}

	got = mergeCategories([]string{catPostmortem, catHardware}, []string{catHardware, catCloud})
	want = []string{catCloud, catPostmortem, catHardware}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCategories() with overlap = %v, want %v", got, want)
	}
}
