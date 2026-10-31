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
	appVersion  = "0.3.1"
	tgProxyPort = 14430
)

type Config struct {
	Mode           string `json:"mode"`
	TelegramSecret string `json:"telegram_secret"`
}

type Status struct {
	Installed     bool   `json:"installed"`
	Active        bool   `json:"active"`
	Mode          string `json:"mode"`
	Platform      string `json:"platform"`
	Message       string `json:"message,omitempty"`
	NeedsUpdate   bool   `json:"needs_update"`
	TelegramReady bool   `json:"telegram_ready"`
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

func loadConfig(path string) Config {
	cfg := Config{Mode: "balanced"}
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg.Mode = normalizeMode(cfg.Mode)
	cfg.TelegramSecret = normalizeTelegramSecret(cfg.TelegramSecret)
	return cfg
}

func saveConfig(path, mode string) error {
	cfg := loadConfig(path)
	cfg.Mode = normalizeMode(mode)
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
