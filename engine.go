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

func strategyArgs(dir, mode string, services []string) []string {
	repeats := 11
	split := "multidisorder:pos=1,midsld"
	switch normalizeMode(mode) {
	case "compatible":
		repeats = 6
		split = "multisplit:pos=1,midsld"
	case "strong":
		repeats = 15
	}
	p := func(name string) string { return filepath.Join(dir, name) }
	args := []string{
		"--lua-init=@" + p("zapret-lib.lua"),
		"--lua-init=@" + p("zapret-antidpi.lua"),
		"--lua-init=fake_default_tls = tls_mod(fake_default_tls,'rnd,rndsni')",
		"--blob=quic_google:" + p("quic_initial_www_google_com.bin"),
	}
	if hasService(services, "youtube") {
		args = append(args,
			"--filter-tcp=443", "--filter-l7=tls", "--hostlist="+p("selected-youtube.txt"), "--out-range=-d10",
			"--payload=tls_client_hello",
			fmt.Sprintf("--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=%d:tls_mod=rnd,dupsid,sni=www.google.com", repeats),
			"--lua-desync="+split,
			"--new",

			"--filter-udp=443", "--filter-l7=quic", "--hostlist="+p("selected-youtube.txt"),
			"--payload=quic_initial",
			fmt.Sprintf("--lua-desync=fake:blob=quic_google:repeats=%d", repeats),
			"--new")
	}
	if hasService(services, "discord") {
		args = append(args,
			// Discord falls back to these HTTPS ports for media and gateway
			// connections.  Limiting the profile to 443 leaves the desktop client
			// waiting forever on networks where that fallback is used.
			"--filter-tcp=443,2053,2083,2087,2096,8443", "--filter-l7=tls", "--hostlist="+p("selected-discord.txt"), "--out-range=-d10",
			"--payload=tls_client_hello",
			fmt.Sprintf("--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=%d", repeats),
			"--lua-desync="+split,
			"--new",

			"--filter-udp=443", "--filter-l7=quic", "--hostlist="+p("selected-discord.txt"),
			"--payload=quic_initial",
			fmt.Sprintf("--lua-desync=fake:blob=quic_google:repeats=%d", repeats),
			"--new",

			// Voice and the initial Discord UDP discovery message do not carry a
			// hostname, so they need their own narrow port/protocol profile.
			"--filter-udp=19294-19344,50000-50100", "--filter-l7=stun,discord",
			"--payload=stun,discord_ip_discovery",
			"--lua-desync=fake:blob=0x00000000000000000000000000000000:repeats=6",
			"--new")
	}
	if hasService(services, "telegram") {
		args = append(args,
			"--filter-tcp=443", "--filter-l7=tls", "--hostlist="+p("selected-telegram.txt"), "--out-range=-d10",
			"--payload=tls_client_hello",
			fmt.Sprintf("--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=%d", repeats),
			"--lua-desync="+split,
			"--new")
	}
	if hasGenericServices(services) {
		args = append(args,
			"--filter-tcp=443", "--filter-l7=tls", "--hostlist="+p("selected-generic.txt"), "--out-range=-d10",
			"--payload=tls_client_hello",
			fmt.Sprintf("--lua-desync=fake:blob=fake_default_tls:tcp_md5:repeats=%d", repeats),
			"--lua-desync="+split,
			"--new",
			"--filter-udp=443", "--filter-l7=quic", "--hostlist="+p("selected-generic.txt"),
			"--payload=quic_initial",
			fmt.Sprintf("--lua-desync=fake:blob=quic_google:repeats=%d", repeats),
			"--new")
	}
	return args
}

func hasGenericServices(services []string) bool {
	for _, id := range normalizeServices(services) {
		if id != "youtube" && id != "discord" && id != "telegram" {
			return true
		}
	}
	return false
}
