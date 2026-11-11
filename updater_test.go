package main

import "testing"

func TestVersionOrdering(t *testing.T) {
	for _, tc := range []struct {
		candidate, current string
		newer              bool
	}{
		{"v0.6.1", "0.6.0", true}, {"v0.6.0", "0.6.0", false},
		{"v0.5.9", "0.6.0", false}, {"v1.0.0", "0.9.9", true},
	} {
		if got := newerVersion(tc.candidate, tc.current); got != tc.newer {
			t.Fatalf("%s vs %s: got %v", tc.candidate, tc.current, got)
		}
	}
}
