package main

import (
	"net"
	"strings"
	"time"
)

type SystemReport struct {
	Time          string                   `json:"time"`
	Version       string                   `json:"version"`
	Platform      string                   `json:"platform"`
	Installed     bool                     `json:"installed"`
	Active        bool                     `json:"active"`
	TelegramReady bool                     `json:"telegram_ready"`
	Conflicts     []string                 `json:"conflicts"`
	VPNInterfaces []string                 `json:"vpn_interfaces"`
	Endpoints     map[string]EndpointCheck `json:"endpoints"`
}

func systemDiagnostics() SystemReport {
	status := platformStatus()
	report := SystemReport{
		Time: time.Now().Format(time.RFC3339), Version: appVersion,
		Platform: status.Platform, Installed: status.Installed, Active: status.Active,
		TelegramReady: status.TelegramReady, Conflicts: platformOtherFilters(),
		Endpoints: connectivityCheck(),
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
