package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed ui/*
var uiFiles embed.FS

func main() {
	if platformHandleSpecialMode() {
		return
	}
	if !platformIsAdministrator() {
		platformRelaunchElevated()
		return
	}

	tokenBytes := make([]byte, 24)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	mux := http.NewServeMux()
	static, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	mux.HandleFunc("GET /api/status", api(token, func(w http.ResponseWriter, r *http.Request) error {
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/install", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if err := platformInstall("balanced"); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/start", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if err := platformStart(); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/stop", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if err := platformStop(); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/remove", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if err := platformRemove(); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/services", api(token, func(w http.ResponseWriter, r *http.Request) error {
		var request struct {
			Services []string `json:"services"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return fmt.Errorf("не удалось прочитать список сервисов")
		}
		if len(normalizeServices(request.Services)) == 0 {
			return fmt.Errorf("выберите хотя бы один сервис")
		}
		if err := platformSetServices(request.Services); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/youtube/profile", api(token, func(w http.ResponseWriter, r *http.Request) error {
		var request struct {
			Profile string `json:"profile"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return fmt.Errorf("не удалось прочитать профиль")
		}
		if !platformStatus().Installed {
			return fmt.Errorf("сначала установите PrivetZapret")
		}
		if err := platformSetYoutubeProfile(request.Profile); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/settings", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if !platformStatus().Installed {
			return fmt.Errorf("сначала установите PrivetZapret")
		}
		var request struct {
			CustomDomains  []string          `json:"custom_domains"`
			ExcludeDomains []string          `json:"exclude_domains"`
			AutoHostlist   bool              `json:"auto_hostlist"`
			GameMode       bool              `json:"game_mode"`
			AutoUpdate     bool              `json:"auto_update"`
			Profiles       map[string]string `json:"profiles"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return fmt.Errorf("не удалось прочитать настройки")
		}
		if len(request.CustomDomains) > 100 || len(request.ExcludeDomains) > 100 {
			return fmt.Errorf("слишком много доменов")
		}
		if len(normalizeDomains(request.CustomDomains)) != len(request.CustomDomains) || len(normalizeDomains(request.ExcludeDomains)) != len(request.ExcludeDomains) {
			return fmt.Errorf("проверьте домены: нужен адрес без https:// и путей")
		}
		for id, profile := range request.Profiles {
			_, known := serviceByID(id)
			if (id != "custom" && id != "generic" && !known) || id == "youtube" || id == "telegram" || normalizeYoutubeProfile(profile) != profile {
				return fmt.Errorf("неизвестный профиль")
			}
		}
		cfg := loadConfig(platformConfigPath())
		cfg.CustomDomains, cfg.ExcludeDomains = request.CustomDomains, request.ExcludeDomains
		cfg.AutoHostlist, cfg.GameMode, cfg.AutoUpdate = request.AutoHostlist, request.GameMode, request.AutoUpdate
		cfg.Profiles = request.Profiles
		if err := platformApplyConfig(cfg); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("POST /api/repair", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if err := platformRepair(); err != nil {
			return err
		}
		return writeJSON(w, platformStatus())
	}))
	mux.HandleFunc("GET /api/update/check", api(token, func(w http.ResponseWriter, r *http.Request) error {
		info, err := checkUpdate(r.Context())
		if err != nil {
			return err
		}
		return writeJSON(w, info)
	}))
	mux.HandleFunc("POST /api/update/apply", api(token, func(w http.ResponseWriter, r *http.Request) error {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
		defer cancel()
		path, err := stageUpdate(ctx)
		if err != nil {
			return err
		}
		if err := launchInstaller(path); err != nil {
			return err
		}
		time.AfterFunc(1500*time.Millisecond, func() { os.Exit(0) })
		return writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/update/rollback", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if err := rollbackUpdate(); err != nil {
			return err
		}
		time.AfterFunc(1500*time.Millisecond, func() { os.Exit(0) })
		return writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/strategies/auto", api(token, func(w http.ResponseWriter, r *http.Request) error {
		var request struct {
			Service string `json:"service"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&request); err != nil {
			return fmt.Errorf("не удалось прочитать сервис")
		}
		profile, err := autoTune(request.Service)
		if err != nil {
			return err
		}
		return writeJSON(w, map[string]any{"profile": profile, "status": platformStatus()})
	}))
	mux.HandleFunc("GET /api/diagnostics", api(token, func(w http.ResponseWriter, r *http.Request) error {
		return writeJSON(w, connectivityCheck())
	}))
	mux.HandleFunc("GET /api/system", api(token, func(w http.ResponseWriter, r *http.Request) error {
		return writeJSON(w, systemDiagnostics())
	}))
	mux.HandleFunc("POST /api/telegram/connect", api(token, func(w http.ResponseWriter, r *http.Request) error {
		if err := platformConnectTelegram(); err != nil {
			return err
		}
		return writeJSON(w, map[string]bool{"ok": true})
	}))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	url := fmt.Sprintf("http://%s/?token=%s", listener.Addr(), token)
	go func() {
		time.Sleep(250 * time.Millisecond)
		platformOpenBrowser(url)
	}()
	fmt.Println("PrivetZapret:", url)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	if err := runTray(url, server); err != nil {
		log.Printf("tray: %v", err)
	}
	if err := <-serveResult; err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func api(token string, fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("X-Privet-Token") != token && r.URL.Query().Get("token") != token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := fn(w, r); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": userError(err)})
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(v)
}

func userError(err error) string {
	s := strings.TrimSpace(err.Error())
	if s == "" {
		return "Не удалось выполнить действие"
	}
	return s
}

type EndpointCheck struct {
	Reachable bool   `json:"reachable"`
	HTTPCode  int    `json:"http_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

func connectivityCheck() map[string]EndpointCheck {
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second},
	}
	checks := map[string]EndpointCheck{}
	targets := map[string]string{
		"youtube": "https://www.youtube.com/generate_204",
		"preview": "https://i.ytimg.com/vi/jfKfPfyJRdk/default.jpg",
	}
	results := make(chan struct {
		name  string
		check EndpointCheck
	}, len(targets))
	for name, url := range targets {
		go func(name, url string) {
			results <- struct {
				name  string
				check EndpointCheck
			}{name, checkEndpoint(client, url)}
		}(name, url)
	}
	for range targets {
		result := <-results
		checks[result.name] = result.check
	}
	return checks
}

func checkEndpoint(client *http.Client, url string) EndpointCheck {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return EndpointCheck{Error: "Некорректный адрес"}
	}
	req.Header.Set("User-Agent", "PrivetZapret/"+appVersion)
	resp, err := client.Do(req)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return EndpointCheck{Error: "Тайм-аут"}
		}
		return EndpointCheck{Error: "Нет соединения"}
	}
	_ = resp.Body.Close()
	return EndpointCheck{Reachable: true, HTTPCode: resp.StatusCode}
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(os.Stderr)
}
