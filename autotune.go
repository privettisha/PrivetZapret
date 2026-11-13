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
	applied := false
	seen := make(map[string]bool)
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
			applied = true
			time.Sleep(250 * time.Millisecond)
		}
		// A fresh transport is essential: an idle HTTPS connection established
		// under the previous profile would bypass the new DPI strategy and make
		// the comparison meaningless.
		client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
			Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			TLSHandshakeTimeout: 2 * time.Second, DisableKeepAlives: true,
		}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}}
		urls := []string{"https://" + def.Domains[0] + "/"}
		if service == "youtube" {
			// A reachable homepage and thumbnail say nothing about playback.
			// Require a response from a googlevideo host before accepting a
			// YouTube profile. This is still a connectivity probe, not a promise
			// that every regional video CDN will play.
			urls = []string{youtubeMediaProbeURL, "https://www.youtube.com/generate_204"}
		}
		if service == "soundcloud" {
			urls = []string{soundcloudMediaProbeURL, "https://soundcloud.com/"}
		}
		if service == "discord" {
			urls = []string{"https://discord.com/api/v10/gateway"}
		}
		score := 0
		mediaReady := service != "youtube" && service != "soundcloud"
		for _, url := range urls {
			check := checkEndpoint(client, url)
			if usableProbe(service, url, check) {
				score++
				if service == "youtube" && url == youtubeMediaProbeURL || service == "soundcloud" && url == soundcloudMediaProbeURL {
					mediaReady = true
				}
			}
		}
		if !mediaReady {
			continue
		}
		if (service == "youtube" || service == "soundcloud") && score != len(urls) {
			// A working CDN alone is not enough if the service's own site fails.
			continue
		}
		if score > bestScore {
			best, bestScore = profile, score
		}
	}
	if bestScore <= 0 {
		if applied {
			if err := platformApplyConfig(original); err != nil {
				return current, errors.New("не удалось проверить соединение и восстановить прежний профиль: " + err.Error())
			}
		}
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
			if restoreErr := platformApplyConfig(original); restoreErr != nil {
				return "", errors.New("не удалось применить профиль и восстановить прежний: " + restoreErr.Error())
			}
			return "", err
		}
	}
	return best, nil
}

func usableProbe(service, url string, check EndpointCheck) bool {
	if !check.Reachable {
		return false
	}
	if url == youtubeMediaProbeURL {
		return check.HTTPCode >= 200 && check.HTTPCode < 400
	}
	// Many service/CDN roots reject an unauthenticated probe with 4xx. The
	// response still proves that TCP/TLS reached that host; it says nothing
	// about login, playback, or regional availability.
	return check.HTTPCode >= 200 && check.HTTPCode < 500
}
