package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
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

func connectivityCheck() map[string]any {
	client := &http.Client{Timeout: 8 * time.Second}
	check := func(url string) bool {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("User-Agent", "PrivetZapret/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode < 500
	}
	return map[string]any{
		"youtube":  check("https://www.youtube.com/generate_204"),
		"discord":  check("https://discord.com/api/v10/gateway"),
		"telegram": telegramProxyReady(),
	}
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(os.Stderr)
}
