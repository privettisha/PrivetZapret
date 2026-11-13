package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releasePage = "https://github.com/privettisha/PrivetZapret/releases/latest"
const releaseTagPrefix = "https://github.com/privettisha/PrivetZapret/releases/tag/"
const releaseDownloadPrefix = "https://github.com/privettisha/PrivetZapret/releases/download/"

var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

type ReleaseInfo struct {
	TagName string
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
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releasePage, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	req.Header.Set("User-Agent", "PrivetZapret/"+appVersion)
	client := releaseClient()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently && resp.StatusCode != http.StatusTemporaryRedirect && resp.StatusCode != http.StatusPermanentRedirect {
		return ReleaseInfo{}, fmt.Errorf("GitHub: HTTP %d", resp.StatusCode)
	}
	return releaseFromLocation(resp.Header.Get("Location"))
}

func releaseFromLocation(location string) (ReleaseInfo, error) {
	tag := strings.TrimPrefix(location, releaseTagPrefix)
	if tag == location || !releaseTagPattern.MatchString(tag) {
		return ReleaseInfo{}, errors.New("GitHub: некорректный адрес релиза")
	}
	return ReleaseInfo{TagName: tag}, nil
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
	if !releaseTagPattern.MatchString(release.TagName) {
		return ""
	}
	return releaseDownloadPrefix + release.TagName + "/" + name
}

func downloadAsset(ctx context.Context, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, releaseDownloadPrefix) {
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
	if runtime.GOOS == "linux" {
		// The updater must not share the PrivetZapret service cgroup: stopping
		// that service during installation would kill its own child process.
		unit := fmt.Sprintf("privetzapret-upgrade-%d", time.Now().UnixNano())
		cmd := exec.Command("systemd-run", "--quiet", "--collect", "--service-type=exec", "--unit="+unit, path, "--silent-install")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("запуск обновления: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
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
