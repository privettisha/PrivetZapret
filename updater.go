package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releaseAPI = "https://api.github.com/repos/privettisha/PrivetZapret/releases/latest"

type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type UpdateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

func rollbackPath() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return filepath.Join(filepath.Dir(platformInstalledBinary()), "rollback", "PrivetZapret"+ext)
}

func releaseClient() *http.Client { return &http.Client{Timeout: 3 * time.Minute} }

func fetchLatestRelease(ctx context.Context) (ReleaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseAPI, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PrivetZapret/"+appVersion)
	resp, err := releaseClient().Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("GitHub: HTTP %d", resp.StatusCode)
	}
	var release ReleaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return ReleaseInfo{}, err
	}
	return release, nil
}

func versionParts(version string) [3]int {
	var parts [3]int
	for i, value := range strings.Split(strings.TrimPrefix(version, "v"), ".") {
		if i >= len(parts) {
			break
		}
		parts[i], _ = strconv.Atoi(value)
	}
	return parts
}

func newerVersion(candidate, current string) bool {
	a, b := versionParts(candidate), versionParts(current)
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func checkUpdate(ctx context.Context) (UpdateInfo, error) {
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return UpdateInfo{}, err
	}
	return UpdateInfo{Available: newerVersion(release.TagName, appVersion), Version: release.TagName}, nil
}

func assetURL(release ReleaseInfo, name string) string {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset.URL
		}
	}
	return ""
}

func downloadAsset(ctx context.Context, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://github.com/privettisha/PrivetZapret/releases/download/") {
		return nil, errors.New("неизвестный адрес релиза")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PrivetZapret/"+appVersion)
	resp, err := releaseClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("загрузка: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit+1))
}

func releaseBinaryName() string {
	if runtime.GOOS == "windows" {
		return "PrivetZapret-Windows-x64.exe"
	}
	return "PrivetZapret-Arch-x64"
}

func stageUpdate(ctx context.Context) (string, error) {
	if !platformStatus().Installed {
		return "", errors.New("сначала установите PrivetZapret")
	}
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return "", err
	}
	if !newerVersion(release.TagName, appVersion) {
		return "", errors.New("установлена актуальная версия")
	}
	name := releaseBinaryName()
	checksums, err := downloadAsset(ctx, assetURL(release, "SHA256SUMS.txt"), 64<<10)
	if err != nil {
		return "", err
	}
	expected := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if len(expected) != 64 {
		return "", errors.New("нет контрольной суммы файла")
	}
	binary, err := downloadAsset(ctx, assetURL(release, name), 100<<20)
	if err != nil {
		return "", err
	}
	if len(binary) == 0 || len(binary) > 100<<20 {
		return "", errors.New("некорректный файл обновления")
	}
	sum := sha256.Sum256(binary)
	if hex.EncodeToString(sum[:]) != expected {
		return "", errors.New("контрольная сумма не совпала")
	}
	dir, err := os.MkdirTemp(filepath.Dir(platformInstalledBinary()), "update-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, binary, 0700); err != nil {
		return "", err
	}
	old, err := os.ReadFile(platformInstalledBinary())
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(rollbackPath()), 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(rollbackPath(), old, 0700); err != nil {
		return "", err
	}
	return path, nil
}

func launchInstaller(path string) error {
	cmd := exec.Command(path, "--silent-install")
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func rollbackUpdate() error {
	if _, err := os.Stat(rollbackPath()); err != nil {
		return errors.New("предыдущая версия не найдена")
	}
	return launchInstaller(rollbackPath())
}

func autoUpdateLoop() {
	if !loadConfig(platformConfigPath()).AutoUpdate {
		return
	}
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	for {
		<-timer.C
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		info, err := checkUpdate(ctx)
		if err == nil && info.Available {
			if path, err := stageUpdate(ctx); err == nil {
				if launchInstaller(path) == nil {
					cancel()
					return
				}
			}
		}
		cancel()
		timer.Reset(12 * time.Hour)
	}
}
