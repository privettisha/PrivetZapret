//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvenLinuxStrategyAcceptedByWorkingEngine(t *testing.T) {
	dir := t.TempDir()
	if err := extractCommon(dir); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Mode: "balanced", Services: defaultServices(), YoutubeProfile: "standard"}
	if err := writeSelectedHostlistsConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(dir, "nfqws")
	data, err := os.ReadFile("payload/linux/nfqws")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, data, 0755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--dry-run", "--qnum=300"}, provenLinuxStrategyArgs(dir, cfg)...)
	if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		t.Fatalf("working nfqws rejected profile: %v\n%s", err, out)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"--dpi-desync=fake --dpi-desync-repeats=6",
		"--ip-id=zero --dpi-desync=hostfakesplit --dpi-desync-repeats=4 --dpi-desync-fooling=ts --dpi-desync-hostfakesplit-mod=host=www.google.com",
		"--hostlist=" + filepath.Join(dir, "selected-all.txt"),
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("working YouTube profile lost %q", expected)
		}
	}
	hostlist, err := os.ReadFile(filepath.Join(dir, "selected-all.txt"))
	if err != nil || !strings.Contains(string(hostlist), "googlevideo.com\n") {
		t.Fatalf("working hostlist does not cover Googlevideo: %v", err)
	}
}
