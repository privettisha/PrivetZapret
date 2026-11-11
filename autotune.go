package main

import (
	"errors"
	"net"
	"net/http"
	"time"
)

func autoTune(service string) (string, error) {
	status := platformStatus()
	if !status.Active {
		return "", errors.New("сначала запустите PrivetZapret")
	}
	if !hasService(status.Services, service) {
		return "", errors.New("сервис не выбран")
	}
	def, ok := serviceByID(service)
	if !ok {
		return "", errors.New("неизвестный сервис")
	}
	original := loadConfig(platformConfigPath())
	current := original.Profiles[service]
	if current == "" {
		current = original.Profiles["generic"]
	}
	if service == "youtube" {
		current = original.YoutubeProfile
	}
	if service == "discord" {
		current = original.Profiles["discord"]
	}
	if current == "" {
		current = "standard"
	}
	choices := []string{current, "standard", "split", "alternative", "tcp"}
	best, bestScore := current, -1
	activeProfile := current
	seen := make(map[string]bool)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		TLSHandshakeTimeout: 2 * time.Second,
	}}
	for _, profile := range choices {
		if seen[profile] {
			continue
		}
		seen[profile] = true
		cfg := original
		cfg.Profiles = make(map[string]string)
		for key, value := range original.Profiles {
			cfg.Profiles[key] = value
		}
		switch service {
		case "youtube":
			cfg.YoutubeProfile = profile
		case "discord":
			cfg.Profiles["discord"] = profile
		default:
			cfg.Profiles[service] = profile
		}
		if profile != activeProfile {
			if err := platformApplyConfig(cfg); err != nil {
				continue
			}
			activeProfile = profile
			time.Sleep(250 * time.Millisecond)
		}
		urls := []string{"https://" + def.Domains[0] + "/"}
		if service == "youtube" {
			urls = []string{"https://www.youtube.com/generate_204", "https://i.ytimg.com/vi/jfKfPfyJRdk/default.jpg"}
		}
		if service == "discord" {
			urls = []string{"https://discord.com/api/v10/gateway"}
		}
		score := 0
		for _, url := range urls {
			if checkEndpoint(client, url).Reachable {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = profile, score
		}
	}
	if bestScore <= 0 {
		_ = platformApplyConfig(original)
		return current, errors.New("не удалось проверить соединение; сохранён прежний профиль")
	}
	cfg := original
	cfg.Profiles = make(map[string]string)
	for key, value := range original.Profiles {
		cfg.Profiles[key] = value
	}
	switch service {
	case "youtube":
		cfg.YoutubeProfile = best
	case "discord":
		cfg.Profiles["discord"] = best
	default:
		cfg.Profiles[service] = best
	}
	if best != activeProfile {
		if err := platformApplyConfig(cfg); err != nil {
			return "", err
		}
	}
	return best, nil
}
