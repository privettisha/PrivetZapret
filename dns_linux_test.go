//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxDNSSnapshotRestoresSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "original")
	path := filepath.Join(dir, "resolv.conf")
	original := []byte("nameserver 9.9.9.9\n")
	if err := os.WriteFile(target, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	state, err := snapshotLinuxDNS(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Link != target || len(state.Original) != 1 || state.Original[0] != "9.9.9.9" {
		t.Fatalf("incorrect snapshot: %+v", state)
	}
	if err := replaceLinuxResolv(path, []byte(linuxDNSMarker), "", 0644, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	if err := replaceLinuxResolv(path, state.Contents, state.Link, os.FileMode(state.Mode), state.UID, state.GID); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(path)
	if err != nil || link != target {
		t.Fatalf("symlink not restored: %q, %v", link, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("content changed: %q, %v", data, err)
	}
}
