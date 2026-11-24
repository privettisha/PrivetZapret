package main

import (
	"fmt"
	"path/filepath"
)

// This profile follows the nfqws command that restored YouTube playback on
// the user's Arch machine. The binary and fake QUIC payload are bundled so the
// desktop app does not depend on an external zapret installation.
func provenLinuxStrategyArgs(dir string, cfg Config) []string {
	tcpRepeats, quicRepeats := 4, 6
	switch normalizeMode(cfg.Mode) {
	case "compatible":
		tcpRepeats, quicRepeats = 3, 4
	case "strong":
		tcpRepeats, quicRepeats = 6, 8
	}
	all := filepath.Join(dir, "selected-all.txt")
	fakeQUIC := filepath.Join(dir, "quic_initial_www_google_com.bin")
	exclude := []string{}
	if len(cfg.ExcludeDomains) > 0 {
		exclude = append(exclude, "--hostlist-exclude="+filepath.Join(dir, "selected-exclude.txt"))
	}
	args := []string{
		"--dpi-desync-fwmark=0x40000000",
		"--filter-udp=443", "--hostlist=" + all,
	}
	args = append(args, exclude...)
	args = append(args,
		"--dpi-desync=fake",
		fmt.Sprintf("--dpi-desync-repeats=%d", quicRepeats),
		"--dpi-desync-fake-quic="+fakeQUIC,
		"--new",
	)
	if hasService(cfg.Services, "discord") {
		args = append(args,
			"--filter-udp=19294-19344,50000-50100", "--filter-l7=discord,stun",
			"--dpi-desync=fake", "--dpi-desync-repeats=6",
			"--dpi-desync-fake-discord="+fakeQUIC,
			"--dpi-desync-fake-stun="+fakeQUIC,
			"--new",
			"--filter-tcp=2053,2083,2087,2096,8443", "--hostlist-domains=discord.media",
			"--dpi-desync=hostfakesplit", fmt.Sprintf("--dpi-desync-repeats=%d", tcpRepeats),
			"--dpi-desync-fooling=ts", "--dpi-desync-hostfakesplit-mod=host=ozon.ru",
			"--new",
		)
	}
	args = append(args,
		"--filter-tcp=443", "--hostlist="+all,
	)
	args = append(args, exclude...)
	args = append(args,
		"--ip-id=zero",
		"--dpi-desync=hostfakesplit",
		fmt.Sprintf("--dpi-desync-repeats=%d", tcpRepeats),
		"--dpi-desync-fooling=ts",
		"--dpi-desync-hostfakesplit-mod=host=www.google.com",
	)
	return args
}
