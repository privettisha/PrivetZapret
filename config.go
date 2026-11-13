package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	appName     = "PrivetZapret"
	serviceName = "PrivetZapret"
	appVersion  = "0.6.3"
	tgProxyPort = 14430
)

type Config struct {
	Mode           string            `json:"mode"`
	YoutubeProfile string            `json:"youtube_profile"`
	TelegramSecret string            `json:"telegram_secret"`
	Services       []string          `json:"services"`
	CustomDomains  []string          `json:"custom_domains,omitempty"`
	ExcludeDomains []string          `json:"exclude_domains,omitempty"`
	AutoHostlist   bool              `json:"auto_hostlist,omitempty"`
	GameMode       bool              `json:"game_mode,omitempty"`
	AutoUpdate     bool              `json:"auto_update,omitempty"`
	Profiles       map[string]string `json:"profiles,omitempty"`
}

type Status struct {
	Installed         bool                `json:"installed"`
	Active            bool                `json:"active"`
	Mode              string              `json:"mode"`
	YoutubeProfile    string              `json:"youtube_profile"`
	Platform          string              `json:"platform"`
	Message           string              `json:"message,omitempty"`
	NeedsUpdate       bool                `json:"needs_update"`
	TelegramReady     bool                `json:"telegram_ready"`
	Services          []string            `json:"services"`
	Catalog           []ServiceDefinition `json:"catalog"`
	CustomDomains     []string            `json:"custom_domains"`
	ExcludeDomains    []string            `json:"exclude_domains"`
	AutoHostlist      bool                `json:"auto_hostlist"`
	GameMode          bool                `json:"game_mode"`
	AutoUpdate        bool                `json:"auto_update"`
	Profiles          map[string]string   `json:"profiles"`
	RollbackAvailable bool                `json:"rollback_available"`
}

func installedVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func normalizeMode(mode string) string {
	switch mode {
	case "compatible", "balanced", "strong":
		return mode
	default:
		return "balanced"
	}
}

func normalizeYoutubeProfile(profile string) string {
	switch profile {
	case "standard", "split", "alternative", "tcp":
		return profile
	default:
		return "standard"
	}
}

func loadConfig(path string) Config {
	cfg := Config{Mode: "balanced", YoutubeProfile: "standard"}
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg.Mode = normalizeMode(cfg.Mode)
	cfg.YoutubeProfile = normalizeYoutubeProfile(cfg.YoutubeProfile)
	cfg.TelegramSecret = normalizeTelegramSecret(cfg.TelegramSecret)
	cfg.CustomDomains = normalizeDomains(cfg.CustomDomains)
	cfg.ExcludeDomains = normalizeDomains(cfg.ExcludeDomains)
	cfg.Profiles = normalizeProfiles(cfg.Profiles)
	if cfg.Services == nil {
		cfg.Services = defaultServices()
	} else {
		cfg.Services = normalizeServices(cfg.Services)
	}
	return cfg
}

func saveConfig(path, mode string) error {
	cfg := loadConfig(path)
	cfg.Mode = normalizeMode(mode)
	return saveConfigValue(path, cfg)
}

func saveServices(path string, services []string) error {
	cfg := loadConfig(path)
	cfg.Services = normalizeServices(services)
	if len(cfg.Services) == 0 {
		return fmt.Errorf("выберите хотя бы один сервис")
	}
	return saveConfigValue(path, cfg)
}

func saveYoutubeProfile(path, profile string) error {
	if normalizeYoutubeProfile(profile) != profile {
		return fmt.Errorf("неизвестный профиль YouTube")
	}
	cfg := loadConfig(path)
	cfg.YoutubeProfile = profile
	return saveConfigValue(path, cfg)
}

func saveConfigValue(path string, cfg Config) error {
	cfg.CustomDomains = normalizeDomains(cfg.CustomDomains)
	cfg.ExcludeDomains = normalizeDomains(cfg.ExcludeDomains)
	cfg.Profiles = normalizeProfiles(cfg.Profiles)
	if cfg.TelegramSecret == "" {
		secret := make([]byte, 16)
		if _, err := rand.Read(secret); err != nil {
			return fmt.Errorf("telegram secret: %w", err)
		}
		cfg.TelegramSecret = fmt.Sprintf("%x", secret)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

func normalizeDomains(values []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, raw := range values {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
		if len(domain) < 4 || len(domain) > 253 || strings.Contains(domain, "..") {
			continue
		}
		valid := true
		for _, label := range strings.Split(domain, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				valid = false
				break
			}
			for _, ch := range label {
				if ch != '-' && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') {
					valid = false
				}
			}
		}
		if valid && strings.Contains(domain, ".") && !seen[domain] {
			out = append(out, domain)
			seen[domain] = true
		}
	}
	return out
}

func normalizeProfiles(profiles map[string]string) map[string]string {
	out := make(map[string]string)
	for id, profile := range profiles {
		if id != "custom" && id != "discord" && id != "generic" {
			if _, ok := serviceByID(id); !ok || id == "youtube" || id == "telegram" {
				continue
			}
		}
		switch profile {
		case "standard", "split", "alternative", "tcp":
			out[id] = profile
		}
	}
	return out
}

func statusSettings(status Status, cfg Config) Status {
	status.CustomDomains = cfg.CustomDomains
	status.ExcludeDomains = cfg.ExcludeDomains
	status.AutoHostlist = cfg.AutoHostlist
	status.GameMode = cfg.GameMode
	status.AutoUpdate = cfg.AutoUpdate
	status.Profiles = cfg.Profiles
	return status
}

func normalizeTelegramSecret(secret string) string {
	secret = strings.ToLower(strings.TrimSpace(secret))
	if len(secret) != 32 {
		return ""
	}
	for _, ch := range secret {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return ""
		}
	}
	return secret
}

func telegramProxyURL(secret string) string {
	return fmt.Sprintf("tg://proxy?server=127.0.0.1&port=%d&secret=dd%s", tgProxyPort, secret)
}

func telegramProxyReady() bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", tgProxyPort), 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
