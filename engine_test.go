//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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

func TestWriteFileIfChangedLeavesIdenticalFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driver.sys")
	data := []byte("same driver")
	if err := writeFileIfChanged(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := writeFileIfChanged(path, bytes.Clone(data), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("identical file was unexpectedly rewritten")
	}
}
