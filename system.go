package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SystemReport struct {
	Time                 string                   `json:"time"`
	Version              string                   `json:"version"`
	InstalledVersion     string                   `json:"installed_version,omitempty"`
	Platform             string                   `json:"platform"`
	Installed            bool                     `json:"installed"`
	Active               bool                     `json:"active"`
	EngineProcesses      int                      `json:"engine_processes"`
	Services             []string                 `json:"services"`
	Profiles             map[string]string        `json:"profiles"`
	YoutubeProfile       string                   `json:"youtube_profile"`
	AutoHostlist         bool                     `json:"auto_hostlist"`
	TelegramReady        bool                     `json:"telegram_ready"`
	TelegramMediaErrors  int                      `json:"telegram_media_errors_recent"`
	YouTubeWarpConnected bool                     `json:"youtube_warp_connected"`
	DNSEnabled           bool                     `json:"dns_enabled"`
	DNSActive            bool                     `json:"dns_active"`
	Conflicts            []string                 `json:"conflicts"`
	VPNInterfaces        []string                 `json:"vpn_interfaces"`
	Endpoints            map[string]EndpointCheck `json:"endpoints"`
}

func systemDiagnostics() SystemReport {
	status := platformStatus()
	report := SystemReport{
		Time: time.Now().Format(time.RFC3339), Version: appVersion, InstalledVersion: status.InstalledVersion,
		Platform: status.Platform, Installed: status.Installed, Active: status.Active, EngineProcesses: status.EngineProcesses,
		Services: status.Services, Profiles: status.Profiles,
		YoutubeProfile: status.YoutubeProfile, AutoHostlist: status.AutoHostlist,
		TelegramReady: status.TelegramReady, Conflicts: platformOtherFilters(),
		TelegramMediaErrors:  recentTelegramMediaErrors(),
		YouTubeWarpConnected: youtubeWarpConnected(),
		DNSEnabled:           status.DNSEnabled, DNSActive: status.DNSActive,
		Endpoints: connectivityCheck(),
	}
	if report.YouTubeWarpConnected {
		report.Endpoints["video_server_via_warp"] = warpMediaProbe()
	}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		name := strings.ToLower(iface.Name)
		for _, marker := range []string{"wireguard", "wintun", "openvpn", "amnezia", "tun", "tap", "clash"} {
			if strings.Contains(name, marker) {
				// Only the interface type is needed for support diagnostics.
				// Interface names can contain user-selected private information.
				report.VPNInterfaces = append(report.VPNInterfaces, marker)
				break
			}
		}
	}
	return report
}

func recentTelegramMediaErrors() int {
	path := filepath.Join(filepath.Dir(platformConfigPath()), "telegram.log")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	if len(data) > 128<<10 {
		data = data[len(data)-(128<<10):]
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "CF proxy failed") || strings.Contains(line, "request failed") || strings.Contains(line, "no fallback available") {
			count++
		}
	}
	return count
}
