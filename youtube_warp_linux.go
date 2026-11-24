//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	userpkg "os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const youtubeWarpPort = "40000"

const youtubeWarpPAC = `function FindProxyForURL(url, host) {
  host = host.toLowerCase();
  if (host === "googlevideo.com" || dnsDomainIs(host, ".googlevideo.com") ||
      host === "youtube.com" || dnsDomainIs(host, ".youtube.com") ||
      host === "youtube-nocookie.com" || dnsDomainIs(host, ".youtube-nocookie.com") ||
      host === "youtu.be" || host === "youtubei.googleapis.com" ||
      host === "youtube.googleapis.com") {
    return "SOCKS5 127.0.0.1:40000";
  }
  return "DIRECT";
}
`

type youtubeWarpState struct {
	PreviousMode string `json:"previous_mode"`
}

func youtubeWarpStatePath() string { return filepath.Join(linuxInstallDir, "youtube-warp-state.json") }
func youtubeWarpPACPath() string   { return filepath.Join(linuxInstallDir, "youtube-warp.pac") }

func youtubeWarpSupported() bool {
	_, err := exec.LookPath("warp-cli")
	return err == nil
}

func youtubeWarpOwned() bool {
	_, err := os.Stat(youtubeWarpStatePath())
	return err == nil
}

func youtubeWarpConnected() bool {
	if !youtubeWarpSupported() {
		return false
	}
	mode, status, err := warpSnapshot()
	return err == nil && mode == "proxy" && status == "Connected"
}

func runWarpCLI(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "warp-cli", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if len(message) > 300 {
			message = message[:300]
		}
		return nil, fmt.Errorf("warp-cli %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return out, nil
}

func warpSnapshot() (mode, status string, err error) {
	settings, err := runWarpCLI("-j", "settings")
	if err != nil {
		return "", "", err
	}
	var s struct {
		Settings struct {
			Mode string `json:"operation_mode"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return "", "", err
	}
	state, err := runWarpCLI("-j", "status")
	if err != nil {
		return "", "", err
	}
	var q struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(state, &q); err != nil {
		return "", "", err
	}
	return s.Settings.Mode, q.Status, nil
}

func startYouTubeWarp() error {
	if !youtubeWarpSupported() {
		return errors.New("установите Cloudflare WARP для выборочного видеомаршрута")
	}
	if youtubeWarpOwned() {
		if err := stopYouTubeWarp(); err != nil {
			return err
		}
	}
	mode, status, err := warpSnapshot()
	if err != nil {
		return err
	}
	if status == "Connected" {
		if mode != "proxy" {
			return errors.New("WARP уже подключён в другом режиме; отключите его перед запуском видеобраузера")
		}
	} else {
		if status != "Disconnected" {
			return fmt.Errorf("WARP пока не готов: %s", status)
		}
		state, err := json.Marshal(youtubeWarpState{PreviousMode: mode})
		if err != nil {
			return err
		}
		if err := os.WriteFile(youtubeWarpStatePath(), state, 0600); err != nil {
			return err
		}
		if _, err := runWarpCLI("mode", "proxy"); err != nil {
			_ = stopYouTubeWarp()
			return err
		}
		if _, err := runWarpCLI("connect"); err != nil {
			_ = stopYouTubeWarp()
			return err
		}
		ready := false
		for i := 0; i < 20; i++ {
			_, state, checkErr := warpSnapshot()
			if checkErr == nil && state == "Connected" {
				ready = true
				break
			}
			time.Sleep(time.Second)
		}
		if !ready {
			_ = stopYouTubeWarp()
			return errors.New("WARP не подключился за 20 секунд")
		}
	}
	if err := checkYouTubeWarp(); err != nil {
		if youtubeWarpOwned() {
			_ = stopYouTubeWarp()
		}
		return err
	}
	if err := os.WriteFile(youtubeWarpPACPath(), []byte(youtubeWarpPAC), 0644); err != nil {
		if youtubeWarpOwned() {
			_ = stopYouTubeWarp()
		}
		return err
	}
	if err := launchYouTubeWarpBrowser(); err != nil {
		if youtubeWarpOwned() {
			_ = stopYouTubeWarp()
		}
		return err
	}
	return nil
}

func checkYouTubeWarp() error {
	check := warpMediaProbe()
	if !usableProbe("youtube", youtubeWarpMediaProbeURL, check) {
		return fmt.Errorf("через WARP видеосервер недоступен: %s (HTTP %d)", check.Error, check.HTTPCode)
	}
	return nil
}

func warpMediaProbe() EndpointCheck {
	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:"+youtubeWarpPort, nil, proxy.Direct)
	if err != nil {
		return EndpointCheck{Error: "локальный прокси WARP недоступен"}
	}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 4 * time.Second}
	if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
		transport.DialContext = contextDialer.DialContext
	} else {
		transport.DialContext = func(_ context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}
	}
	defer transport.CloseIdleConnections()
	return checkEndpoint(&http.Client{Timeout: 7 * time.Second, Transport: transport}, youtubeWarpMediaProbeURL)
}

func stopYouTubeWarp() error {
	data, err := os.ReadFile(youtubeWarpStatePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state youtubeWarpState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	mode, _, err := warpSnapshot()
	if err != nil {
		return err
	}
	if mode == "proxy" {
		if _, err := runWarpCLI("disconnect"); err != nil {
			return err
		}
		if state.PreviousMode != "" && state.PreviousMode != "proxy" {
			if _, err := runWarpCLI("mode", state.PreviousMode); err != nil {
				return err
			}
		}
	}
	return os.Remove(youtubeWarpStatePath())
}

func launchYouTubeWarpBrowser() error {
	username := os.Getenv("SUDO_USER")
	if username == "" && os.Getenv("PKEXEC_UID") != "" {
		if account, err := userpkg.LookupId(os.Getenv("PKEXEC_UID")); err == nil {
			username = account.Username
		}
	}
	if username == "" || username == "root" {
		return errors.New("не удалось определить пользователя браузера")
	}
	account, err := userpkg.Lookup(username)
	if err != nil {
		return err
	}
	var browser string
	for _, candidate := range []string{"brave", "google-chrome", "chromium", "microsoft-edge"} {
		if browser, err = exec.LookPath(candidate); err == nil {
			break
		}
	}
	if browser == "" {
		return errors.New("для видеобраузера нужен Brave, Chrome, Chromium или Edge")
	}
	profileDir := filepath.Join(account.HomeDir, ".config", "PrivetZapret-YouTube")
	args := []string{"-u", username, "env", "HOME=" + account.HomeDir, "USER=" + username, "LOGNAME=" + username}
	for _, key := range []string{"DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if value := os.Getenv(key); value != "" {
			args = append(args, key+"="+value)
		}
	}
	args = append(args, browser,
		"--user-data-dir="+profileDir,
		"--proxy-pac-url=file://"+youtubeWarpPACPath(),
		"--disable-quic", "--no-first-run", "--new-window", "https://www.youtube.com/")
	cmd := exec.Command("sudo", args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return nil
	}
}
