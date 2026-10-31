package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigKeepsTelegramSecretAcrossModeChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, "balanced"); err != nil {
		t.Fatal(err)
	}
	first := loadConfig(path)
	if len(first.TelegramSecret) != 32 {
		t.Fatalf("unexpected Telegram secret: %q", first.TelegramSecret)
	}
	if err := saveConfig(path, "strong"); err != nil {
		t.Fatal(err)
	}
	second := loadConfig(path)
	if second.TelegramSecret != first.TelegramSecret || second.Mode != "strong" {
		t.Fatalf("config was not preserved: %#v -> %#v", first, second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("config permissions are too broad: %o", info.Mode().Perm())
	}
}

func TestTelegramProxyURLIsLocal(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	url := telegramProxyURL(secret)
	if !strings.Contains(url, "server=127.0.0.1") || !strings.Contains(url, "secret=dd"+secret) {
		t.Fatalf("unexpected Telegram URL: %s", url)
	}
}
