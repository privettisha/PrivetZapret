package main

import (
	"strings"
	"testing"
)

func TestModeChangesEveryYouTubeProfile(t *testing.T) {
	for _, profile := range []string{"standard", "split", "alternative", "tcp"} {
		var variants []string
		for _, mode := range []string{"compatible", "balanced", "strong"} {
			args := strategyArgsConfig("/tmp", Config{Mode: mode, YoutubeProfile: profile, Services: []string{"youtube"}})
			variants = append(variants, strings.Join(args, "\n"))
		}
		if variants[0] == variants[1] || variants[1] == variants[2] || variants[0] == variants[2] {
			t.Errorf("mode does not change YouTube profile %s", profile)
		}
	}
}
