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
			existing: nil,
			want:     []string{catCloud, catConfigChange},
		},
		{
			name:     "skip already-present categories",
			body:     "Cascading failure across the cluster after a misconfiguration.",
			existing: []string{catCascadingFailure},
			want:     []string{catConfigChange},
		},
		{
			name:     "no matches",
			body:     "Nothing in this body should trigger anything.",
			existing: nil,
			want:     nil,
		},
		{
			name:     "ntp triggers time",
			body:     "An NTP misconfiguration confused our log timestamps.",
			existing: nil,
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
	// Order follows the declaration order in postmortems.Categories.
	got := mergeCategories(nil, []string{catCloud, catConfigChange})
	want := []string{catCloud, catConfigChange}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCategories() = %v, want %v", got, want)
	}

	got = mergeCategories([]string{catHardware}, []string{catHardware, catCloud})
	want = []string{catCloud, catHardware}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeCategories() with overlap = %v, want %v", got, want)
	}
}
