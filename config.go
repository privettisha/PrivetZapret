package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	appName     = "PrivetZapret"
	serviceName = "PrivetZapret"
	appVersion  = "0.2.0"
)

type Config struct {
	Mode string `json:"mode"`
}

type Status struct {
	Installed   bool   `json:"installed"`
	Active      bool   `json:"active"`
	Mode        string `json:"mode"`
	Platform    string `json:"platform"`
	Message     string `json:"message,omitempty"`
	NeedsUpdate bool   `json:"needs_update"`
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
	return cfg
}

func saveConfig(path, mode string) error {
	b, err := json.MarshalIndent(Config{Mode: normalizeMode(mode)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
