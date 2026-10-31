//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStrategiesAcceptedByZapret2(t *testing.T) {
	dir := t.TempDir()
	if err := extractCommon(dir); err != nil {
		t.Fatal(err)
	}
	engine := filepath.Join(dir, "nfqws2")
	b, err := os.ReadFile("payload/linux/nfqws2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, b, 0755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"compatible", "balanced", "strong"} {
		t.Run(mode, func(t *testing.T) {
			args := append([]string{"--dry-run", "--qnum=300"}, strategyArgs(dir, mode)...)
			if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
				t.Fatalf("zapret2 rejected strategy: %v\n%s", err, out)
			}
		})
	}
}
