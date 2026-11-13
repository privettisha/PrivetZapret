package main

import (
	"context"
	"os"
	"testing"
	"time"
)

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

func TestReleaseRedirect(t *testing.T) {
	release, err := releaseFromLocation(releaseTagPrefix + "v0.6.3")
	if err != nil || release.TagName != "v0.6.3" {
		t.Fatalf("valid latest redirect: %#v, %v", release, err)
	}
	if got := assetURL(release, "SHA256SUMS.txt"); got != releaseDownloadPrefix+"v0.6.3/SHA256SUMS.txt" {
		t.Fatalf("asset URL: %q", got)
	}
	for _, location := range []string{"", "https://evil.example/releases/tag/v0.6.3", releaseTagPrefix + "v0.6.3/extra", releaseTagPrefix + "v0.6.3?download=1"} {
		if _, err := releaseFromLocation(location); err == nil {
			t.Fatalf("accepted untrusted redirect %q", location)
		}
	}
}

func TestLatestReleaseOnline(t *testing.T) {
	if os.Getenv("PRIVETZAPRET_TEST_ONLINE") != "1" {
		t.Skip("requires GitHub network access")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	release, err := fetchLatestRelease(ctx)
	if err != nil || !releaseTagPattern.MatchString(release.TagName) {
		t.Fatalf("latest release: %#v, %v", release, err)
	}
}
