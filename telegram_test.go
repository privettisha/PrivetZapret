package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelegramMediaRouteAndFallbackHostlist(t *testing.T) {
	args := telegramProxyArgs(t.TempDir())
	var redirects []string
	for i, arg := range args {
		if arg == "--dc-ip" {
			if i+1 >= len(args) {
				t.Fatalf("missing DC route in Telegram args: %v", args)
			}
			redirects = append(redirects, args[i+1])
		}
	}
	if len(redirects) != 1 || redirects[0] != "4:149.154.167.220" {
		t.Fatalf("Telegram must redirect only DC4: %v", redirects)
	}
}

func TestTelegramFallbackDomainsReachZapretHostlist(t *testing.T) {
	dir := t.TempDir()
	if err := writeSelectedHostlistsConfig(dir, Config{Services: []string{"telegram"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "selected-telegram.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range []string{"pclead.co.uk", "fixtelega.co.uk", "web.telegram.org"} {
		if !strings.Contains(string(data), domain+"\n") {
			t.Errorf("Telegram hostlist misses %s", domain)
		}
	}
}

func TestTelegramRoutesUseDifferentUpstreamPaths(t *testing.T) {
	direct := strings.Join(telegramProxyArgsConfig(t.TempDir(), "direct"), " ")
	cloudflare := strings.Join(telegramProxyArgsConfig(t.TempDir(), "cloudflare"), " ")
	if !strings.Contains(direct, "--dc-ip 2:149.154.167.220") || !strings.Contains(direct, "--dc-ip 4:149.154.167.220") {
		t.Fatalf("direct route does not include both DCs: %s", direct)
	}
	if !strings.Contains(cloudflare, "--dc-ip --pool-size 0") {
		t.Fatalf("fallback route still creates a direct pool: %s", cloudflare)
	}
}
