package main

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed payload/common/*
var commonPayload embed.FS

var commonFiles = []string{
	"zapret-lib.lua",
	"zapret-antidpi.lua",
	"quic_initial_www_google_com.bin",
	"youtube.txt",
	"discord.txt",
	"telegram.txt",
	"ZAPRET2-LICENSE.txt",
	"TG-WS-PROXY-LICENSE.txt",
	"windivert_part.discord_media.txt",
	"windivert_part.quic_initial_ietf.txt",
}

func extractCommon(dir string) error {
	for _, name := range commonFiles {
		b, err := commonPayload.ReadFile("payload/common/" + name)
		if err != nil {
			return err
		}
		if err := writeFileIfChanged(filepath.Join(dir, name), b, 0644); err != nil {
			return err
		}
	}
	return nil
}

// writeFileIfChanged deliberately leaves an identical file untouched. This is
// important on Windows: a loaded WinDivert driver can keep its .sys file open
// even when no user-visible process remains.
func writeFileIfChanged(path string, data []byte, mode os.FileMode) error {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func strategyArgs(dir, mode string, services []string, youtubeProfile ...string) []string {
	cfg := Config{Mode: mode, Services: services}
	if len(youtubeProfile) > 0 {
		cfg.YoutubeProfile = youtubeProfile[0]
	}
	return strategyArgsConfig(dir, cfg)
}

func strategyArgsConfig(dir string, cfg Config) []string {
	repeats := 11
	split := "multidisorder:pos=1,midsld"
	proxyExcludes := "--ipset-exclude-ip=2.26.225.138,31.76.251.3"
	switch normalizeMode(cfg.Mode) {
	case "compatible":
		repeats = 6
		split = "multisplit:pos=1,midsld"
	case "strong":
		repeats = 15
	}
	p := func(name string) string { return filepath.Join(dir, name) }
	exclude := []string{}
	if len(cfg.ExcludeDomains) > 0 {
		exclude = append(exclude, "--hostlist-exclude="+p("selected-exclude.txt"))
	}
	args := []string{
		"--lua-init=@" + p("zapret-lib.lua"),
		"--lua-init=@" + p("zapret-antidpi.lua"),
		"--lua-init=fake_default_tls = tls_mod(fake_default_tls,'rnd,rndsni')",
		"--blob=quic_google:" + p("quic_initial_www_google_com.bin"),
	}
	if hasService(cfg.Services, "youtube") {
		profile := normalizeYoutubeProfile(cfg.YoutubeProfile)
		youtubeFake := fmt.Sprintf("fake:blob=fake_default_tls:tcp_md5:repeats=%d:tls_mod=rnd,dupsid,sni=www.google.com", repeats)
		youtubeSplit := split
		quicDesync := fmt.Sprintf("fake:blob=quic_google:repeats=%d", repeats)
		switch profile {
		case "split":
			youtubeFake = "fake:blob=fake_default_tls:tcp_md5:repeats=6:tls_mod=rnd,dupsid,sni=www.google.com"
			youtubeSplit = "multisplit:pos=1,midsld"
		case "alternative", "tcp":
			youtubeFake = "fake:blob=fake_default_tls:tcp_md5:tcp_seq=-10000:repeats=6"
			youtubeSplit = "multidisorder:pos=midsld"
			if profile == "tcp" {
				// Only YouTube QUIC is dropped; browsers retry the same host over TCP.
				quicDesync = "drop"
			}
		}
		args = append(args,
			"--filter-tcp=443", "--filter-l7=tls", proxyExcludes, "--hostlist="+p("selected-youtube.txt"))
		args = append(args, exclude...)
		args = append(args, "--out-range=-d10",
			"--payload=tls_client_hello",
			"--lua-desync="+youtubeFake,
			"--lua-desync="+youtubeSplit,
			"--new",

			"--filter-udp=443", "--filter-l7=quic", proxyExcludes, "--hostlist="+p("selected-youtube.txt"))
		args = append(args, exclude...)
		args = append(args,
			"--payload=quic_initial",
			"--lua-desync="+quicDesync,
			"--new")
	}
	if hasService(cfg.Services, "discord") {
		fake, splitMode, quic := profileStrategy(cfg.Profiles["discord"], repeats, split)
		args = append(args,
			// Discord falls back to these HTTPS ports for media and gateway
			// connections.  Limiting the profile to 443 leaves the desktop client
			// waiting forever on networks where that fallback is used.
			"--filter-tcp=443,2053,2083,2087,2096,8443", "--filter-l7=tls", proxyExcludes, "--hostlist="+p("selected-discord.txt"))
		args = append(args, exclude...)
		args = append(args, "--out-range=-d10",
			"--payload=tls_client_hello",
			"--lua-desync="+fake,
			"--lua-desync="+splitMode,
			"--new",

			"--filter-udp=443", "--filter-l7=quic", proxyExcludes, "--hostlist="+p("selected-discord.txt"))
		args = append(args, exclude...)
		args = append(args,
			"--payload=quic_initial",
			"--lua-desync="+quic,
			"--new",

			// Voice and the initial Discord UDP discovery message do not carry a
			// hostname, so they need their own narrow port/protocol profile.
			"--filter-udp=19294-19344,50000-50100", "--filter-l7=stun,discord", proxyExcludes,
			"--payload=stun,discord_ip_discovery",
			"--lua-desync=fake:blob=0x00000000000000000000000000000000:repeats=6",
			"--new")
	}
	if hasService(cfg.Services, "telegram") {
		args = append(args,
			"--filter-tcp=443", "--filter-l7=tls", proxyExcludes, "--hostlist="+p("selected-telegram.txt"))
		args = append(args, exclude...)
		args = append(args, "--out-range=-d10",
			"--payload=tls_client_hello",
			fmt.Sprintf("--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=%d", repeats),
			"--lua-desync="+split,
			"--new")
	}
	for _, id := range normalizeServices(cfg.Services) {
		if id == "youtube" || id == "discord" || id == "telegram" {
			continue
		}
		profile := cfg.Profiles[id]
		if profile == "" {
			profile = cfg.Profiles["generic"]
		}
		args = appendHostProfile(args, p("selected-"+id+".txt"), profile, repeats, split, proxyExcludes, exclude)
	}
	if len(cfg.CustomDomains) > 0 {
		args = appendHostProfile(args, p("selected-custom.txt"), cfg.Profiles["custom"], repeats, split, proxyExcludes, exclude)
	}
	if cfg.AutoHostlist {
		fake, splitMode, _ := profileStrategy(cfg.Profiles["generic"], repeats, split)
		args = append(args, "--filter-tcp=443", "--filter-l7=tls", proxyExcludes,
			"--hostlist-auto="+p("selected-auto.txt"))
		args = append(args, exclude...)
		args = append(args, "--out-range=-d10", "--payload=tls_client_hello",
			"--lua-desync="+fake, "--lua-desync="+splitMode, "--new")
	}
	if cfg.GameMode && hasGameService(cfg.Services) {
		args = append(args, "--filter-udp=3074,3478-3480,3659", "--filter-l7=stun", proxyExcludes,
			"--payload=stun", "--lua-desync=fake:blob=0x00000000000000000000000000000000:repeats=3", "--new")
	}
	return args
}

func appendHostProfile(args []string, list, profile string, repeats int, split, proxyExcludes string, exclude []string) []string {
	fake, splitMode, quic := profileStrategy(profile, repeats, split)
	args = append(args, "--filter-tcp=443", "--filter-l7=tls", proxyExcludes, "--hostlist="+list)
	args = append(args, exclude...)
	args = append(args, "--out-range=-d10", "--payload=tls_client_hello", "--lua-desync="+fake, "--lua-desync="+splitMode, "--new",
		"--filter-udp=443", "--filter-l7=quic", proxyExcludes, "--hostlist="+list)
	args = append(args, exclude...)
	return append(args, "--payload=quic_initial", "--lua-desync="+quic, "--new")
}

func profileStrategy(profile string, repeats int, split string) (string, string, string) {
	fake := fmt.Sprintf("fake:blob=fake_default_tls:tcp_md5:repeats=%d", repeats)
	quic := fmt.Sprintf("fake:blob=quic_google:repeats=%d", repeats)
	switch profile {
	case "split":
		return "fake:blob=fake_default_tls:tcp_md5:repeats=6", "multisplit:pos=1,midsld", quic
	case "alternative":
		return "fake:blob=fake_default_tls:tcp_md5:tcp_seq=-10000:repeats=6", "multidisorder:pos=midsld", quic
	case "tcp":
		return "fake:blob=fake_default_tls:tcp_md5:tcp_seq=-10000:repeats=6", "multidisorder:pos=midsld", "drop"
	default:
		return fake, split, quic
	}
}

func hasGameService(services []string) bool {
	for _, id := range []string{"steam", "roblox", "cod", "fortnite", "ea", "ubisoft"} {
		if hasService(services, id) {
			return true
		}
	}
	return false
}

func hasGenericServices(services []string) bool {
	for _, id := range normalizeServices(services) {
		if id != "youtube" && id != "discord" && id != "telegram" {
			return true
		}
	}
	return false
}
