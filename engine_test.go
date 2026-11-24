//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
			if err := writeSelectedHostlists(dir, defaultServices()); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--dry-run", "--qnum=300"}, strategyArgs(dir, mode, defaultServices())...)
			if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
				t.Fatalf("zapret2 rejected strategy: %v\n%s", err, out)
			}
		})
	}
}

func TestYoutubeProfilesAcceptedByZapret2(t *testing.T) {
	dir := t.TempDir()
	if err := extractCommon(dir); err != nil {
		t.Fatal(err)
	}
	if err := writeSelectedHostlists(dir, defaultServices()); err != nil {
		t.Fatal(err)
	}
	media, err := os.ReadFile(filepath.Join(dir, "selected-youtube-media.txt"))
	if err != nil || string(media) != "googlevideo.com\n" {
		t.Fatalf("YouTube media list must contain only googlevideo.com: %q, %v", media, err)
	}
	engine := filepath.Join(dir, "nfqws2")
	b, err := os.ReadFile("payload/linux/nfqws2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, b, 0755); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"standard", "split", "alternative", "tcp"} {
		t.Run(profile, func(t *testing.T) {
			args := append([]string{"--dry-run", "--qnum=300"}, strategyArgs(dir, "balanced", defaultServices(), profile)...)
			if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
				t.Fatalf("zapret2 rejected YouTube profile: %v\n%s", err, out)
			}
		})
	}
	if !strings.Contains(strings.Join(strategyArgs(dir, "balanced", defaultServices(), "tcp"), " "), "--lua-desync=drop") {
		t.Fatal("TCP fallback must suppress YouTube QUIC")
	}
	joined := strings.Join(strategyArgs(dir, "balanced", defaultServices(), "standard"), " ")
	if !strings.Contains(joined, "--hostlist="+filepath.Join(dir, "selected-youtube-media.txt")+" --payload=quic_initial --lua-desync=drop") {
		t.Fatal("YouTube media must fall back to TCP separately from the web page")
	}
	mediaTCP := "--filter-tcp=443 --filter-l7=tls --ipset-exclude-ip=2.26.225.138,31.76.251.3 --hostlist=" + filepath.Join(dir, "selected-youtube-media.txt")
	pageTCP := "--filter-tcp=443 --filter-l7=tls --ipset-exclude-ip=2.26.225.138,31.76.251.3 --hostlist=" + filepath.Join(dir, "selected-youtube.txt")
	if mediaAt, pageAt := strings.Index(joined, mediaTCP), strings.Index(joined, pageTCP); mediaAt < 0 || pageAt < 0 || mediaAt >= pageAt {
		t.Fatal("Googlevideo TCP must receive its own rule before the YouTube page")
	}
	if !strings.Contains(joined, "--lua-desync=hostfakesplit:host=www.google.com:ip_id=zero") {
		t.Fatal("standard Googlevideo strategy must include hostfakesplit")
	}
	splitArgs := strings.Join(strategyArgs(dir, "balanced", defaultServices(), "split"), " ")
	if !strings.Contains(splitArgs, "--lua-desync=multisplit:pos=1,midsld:seqovl=681:seqovl_pattern=fake_default_tls:ip_id=zero") {
		t.Fatal("split Googlevideo strategy must include TLS overlap")
	}
}

func TestFullCatalogStrategyAcceptedByZapret2(t *testing.T) {
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
	var all []string
	for _, service := range serviceCatalog {
		all = append(all, service.ID)
	}
	if err := writeSelectedHostlists(dir, all); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--dry-run", "--qnum=300"}, strategyArgs(dir, "balanced", all)...)
	if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		t.Fatalf("zapret2 rejected full catalogue: %v\n%s", err, out)
	}
	generic, err := os.ReadFile(filepath.Join(dir, "selected-generic.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"instagram.com", "reddit.com", "twitch.tv", "soundcloud.com"} {
		if !bytes.Contains(generic, []byte(expected+"\n")) {
			t.Fatalf("generic hostlist is missing %s", expected)
		}
	}
}

func TestExtendedConfigurationAcceptedByZapret2(t *testing.T) {
	dir := t.TempDir()
	if err := extractCommon(dir); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Mode: "balanced", Services: []string{"youtube", "discord", "signal", "roblox"},
		CustomDomains: []string{"example.com"}, ExcludeDomains: []string{"example.org"},
		AutoHostlist: true, GameMode: true,
		Profiles: map[string]string{"discord": "split", "signal": "alternative", "roblox": "tcp", "custom": "split"},
	}
	if err := writeSelectedHostlistsConfig(dir, cfg); err != nil {
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
	args := append([]string{"--dry-run", "--qnum=300"}, strategyArgsConfig(dir, cfg)...)
	if out, err := exec.Command(engine, args...).CombinedOutput(); err != nil {
		t.Fatalf("extended configuration rejected: %v\n%s", err, out)
	}
	joined := strings.Join(args, " ")
	for _, needle := range []string{"selected-signal.txt", "selected-roblox.txt", "selected-custom.txt", "selected-exclude.txt", "selected-auto.txt", "--filter-udp=3074,3478-3480,3659"} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("missing %s", needle)
		}
	}
	if !strings.Contains(joined, "--filter-udp=443 --filter-l7=quic --ipset-exclude-ip=2.26.225.138,31.76.251.3 --hostlist="+filepath.Join(dir, "selected-auto.txt")) {
		t.Fatal("auto-learned hosts must also be covered over QUIC")
	}
	if !strings.Contains(joined, "--filter-udp=443 --filter-l7=quic --ipset-exclude-ip=2.26.225.138,31.76.251.3 --hostlist-auto="+filepath.Join(dir, "selected-auto.txt")) {
		t.Fatal("QUIC failures must be learned for media hosts outside the page domain")
	}
	mediaParts := strings.SplitN(joined, "--hostlist="+filepath.Join(dir, "selected-discord-media.txt"), 2)
	if len(mediaParts) != 2 || !strings.Contains(strings.SplitN(mediaParts[1], " --new", 2)[0], "--lua-desync=drop") {
		t.Fatal("Discord media must fall back to TCP independently from chat")
	}
}

func TestPrivetDNSProxyAddressesAreExcludedFromDesync(t *testing.T) {
	args := strategyArgs(t.TempDir(), "balanced", defaultServices())
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--ipset-exclude-ip=2.26.225.138,31.76.251.3") {
		t.Fatal("PrivetDNS PL WEB proxy addresses are not excluded from desync")
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
