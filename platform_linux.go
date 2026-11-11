//go:build linux

package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

const linuxInstallDir = "/opt/privetzapret"
const linuxUnitPath = "/etc/systemd/system/privetzapret.service"

//go:embed payload/linux/*
var linuxPayload embed.FS

func platformHandleSpecialMode() bool {
	if len(os.Args) > 1 && os.Args[1] == "--silent-install" {
		time.Sleep(2 * time.Second)
		if err := platformInstall("balanced"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
	if len(os.Args) > 1 && os.Args[1] == "--daemon" {
		if err := runLinuxDaemon(); err != nil {
			fmt.Fprintln(os.Stderr, "PrivetZapret:", err)
			os.Exit(1)
		}
		return true
	}
	return false
}

func platformIsAdministrator() bool { return os.Geteuid() == 0 }

func platformRelaunchElevated() {
	path, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	cmd := exec.Command("sudo", append([]string{path}, os.Args[1:]...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	_ = cmd.Run()
}

func linuxConfigPath() string { return filepath.Join(linuxInstallDir, "config.json") }

func linuxVersionPath() string        { return filepath.Join(linuxInstallDir, "version.txt") }
func platformInstalledBinary() string { return filepath.Join(linuxInstallDir, appName) }

func platformStatus() Status {
	_, statErr := os.Stat(linuxUnitPath)
	active := exec.Command("systemctl", "is-active", "--quiet", "privetzapret.service").Run() == nil
	installed := statErr == nil
	cfg := loadConfig(linuxConfigPath())
	status := statusSettings(Status{Installed: installed, Active: active, Mode: cfg.Mode, YoutubeProfile: cfg.YoutubeProfile, Platform: "Linux · " + runtime.GOARCH, NeedsUpdate: installed && installedVersion(linuxVersionPath()) != appVersion, TelegramReady: active && hasService(cfg.Services, "telegram") && telegramProxyReady(), Services: cfg.Services, Catalog: serviceCatalog}, cfg)
	_, err := os.Stat(rollbackPath())
	status.RollbackAvailable = err == nil
	return status
}

func platformInstall(mode string) error {
	if platformStatus().Installed {
		if err := platformStop(); err != nil {
			return err
		}
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return errors.New("не найден nftables: установите пакет nftables")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("не найден systemd")
	}
	if err := os.MkdirAll(linuxInstallDir, 0755); err != nil {
		return err
	}
	if err := extractCommon(linuxInstallDir); err != nil {
		return err
	}
	b, err := linuxPayload.ReadFile("payload/linux/nfqws2")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(linuxInstallDir, "nfqws2"), b, 0755); err != nil {
		return err
	}
	tg, err := linuxPayload.ReadFile("payload/linux/PrivetTgWs")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(linuxInstallDir, "PrivetTgWs"), tg, 0755); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(linuxInstallDir, appName)
	if filepath.Clean(self) != dst {
		bin, err := os.ReadFile(self)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, bin, 0755); err != nil {
			return err
		}
	}
	if err := saveConfig(linuxConfigPath(), mode); err != nil {
		return err
	}
	if err := writeFileIfChanged(linuxVersionPath(), []byte(appVersion+"\n"), 0644); err != nil {
		return err
	}
	unit := `[Unit]
Description=PrivetZapret selected service connectivity
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/privetzapret/PrivetZapret --daemon
Restart=on-failure
RestartSec=2
TimeoutStopSec=8

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile(linuxUnitPath, []byte(unit), 0644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemd: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "enable", "--now", "privetzapret.service").CombinedOutput(); err != nil {
		return fmt.Errorf("запуск: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func platformStart() error {
	out, err := exec.Command("systemctl", "start", "privetzapret.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("запуск: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func platformStop() error {
	out, err := exec.Command("systemctl", "stop", "privetzapret.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("остановка: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func platformSetMode(mode string) error {
	wasActive := platformStatus().Active
	if wasActive {
		_ = platformStop()
	}
	if err := saveConfig(linuxConfigPath(), mode); err != nil {
		return err
	}
	if wasActive {
		return platformStart()
	}
	return nil
}

func platformSetServices(services []string) error {
	wasActive := platformStatus().Active
	if wasActive {
		if err := platformStop(); err != nil {
			return err
		}
	}
	if err := saveServices(linuxConfigPath(), services); err != nil {
		return err
	}
	if wasActive {
		return platformStart()
	}
	return nil
}

func platformSetYoutubeProfile(profile string) error {
	if normalizeYoutubeProfile(profile) != profile {
		return errors.New("неизвестный профиль YouTube")
	}
	previous := loadConfig(linuxConfigPath()).YoutubeProfile
	wasActive := platformStatus().Active
	if wasActive {
		if err := platformStop(); err != nil {
			return err
		}
	}
	if err := saveYoutubeProfile(linuxConfigPath(), profile); err != nil {
		if wasActive {
			_ = platformStart()
		}
		return err
	}
	if wasActive {
		if err := platformStart(); err != nil {
			_ = saveYoutubeProfile(linuxConfigPath(), previous)
			_ = platformStart()
			return err
		}
	}
	return nil
}

func platformConfigPath() string { return linuxConfigPath() }

func platformApplyConfig(cfg Config) error {
	previous := loadConfig(linuxConfigPath())
	wasActive := platformStatus().Active
	if wasActive {
		if err := platformStop(); err != nil {
			return err
		}
	}
	if err := saveConfigValue(linuxConfigPath(), cfg); err != nil {
		if wasActive {
			_ = platformStart()
		}
		return err
	}
	if wasActive {
		if err := platformStart(); err != nil {
			_ = saveConfigValue(linuxConfigPath(), previous)
			_ = platformStart()
			return err
		}
	}
	return nil
}

func platformRepair() error {
	if !platformStatus().Installed {
		return errors.New("сначала установите PrivetZapret")
	}
	if err := platformStop(); err != nil {
		return err
	}
	if err := platformStart(); err != nil {
		return err
	}
	if path, err := exec.LookPath("resolvectl"); err == nil {
		_ = exec.Command(path, "flush-caches").Run()
	}
	return nil
}

func platformOtherFilters() []string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil || strings.HasPrefix(path, linuxInstallDir+"/") {
			continue
		}
		name := strings.ToLower(filepath.Base(path))
		for _, match := range []string{"nfqws", "nfqws2", "tpws", "goodbyedpi", "winws", "winws2"} {
			if name == match {
				seen[name] = true
			}
		}
	}
	var names []string
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func platformRemove() error {
	_ = platformStop()
	_ = exec.Command("systemctl", "disable", "privetzapret.service").Run()
	_ = os.Remove(linuxUnitPath)
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = deleteNftTable()
	return os.RemoveAll(linuxInstallDir)
}

func runLinuxDaemon() error {
	if os.Geteuid() != 0 {
		return errors.New("службе нужны права root")
	}
	_ = deleteNftTable()
	cfg := loadConfig(linuxConfigPath())
	if len(cfg.Services) == 0 {
		return errors.New("не выбран ни один сервис")
	}
	if err := writeSelectedHostlistsConfig(linuxInstallDir, cfg); err != nil {
		return err
	}
	args := append([]string{"--qnum=300"}, strategyArgsConfig(linuxInstallDir, cfg)...)
	cmd := exec.Command(filepath.Join(linuxInstallDir, "nfqws2"), args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	if hasService(cfg.Services, "telegram") && cfg.TelegramSecret == "" {
		_ = cmd.Process.Kill()
		<-done
		return errors.New("не создан локальный ключ Telegram")
	}
	var tgCmd *exec.Cmd
	var tgDone chan error
	if hasService(cfg.Services, "telegram") {
		tgCmd = exec.Command(filepath.Join(linuxInstallDir, "PrivetTgWs"),
			"--host", "127.0.0.1", "--port", fmt.Sprint(tgProxyPort),
			"--log-file", filepath.Join(linuxInstallDir, "telegram.log"),
			"--log-max-mb", "3", "--log-backups", "1")
		tgCmd.Env = append(os.Environ(), "PRIVET_TG_SECRET="+cfg.TelegramSecret)
		tgCmd.Stdout, tgCmd.Stderr = os.Stdout, os.Stderr
		if err := tgCmd.Start(); err != nil {
			_ = cmd.Process.Kill()
			<-done
			return fmt.Errorf("telegram proxy: %w", err)
		}
		tgDone = make(chan error, 1)
		go func() { tgDone <- tgCmd.Wait() }()
	}

	select {
	case err := <-done:
		if tgCmd != nil {
			_ = tgCmd.Process.Kill()
			<-tgDone
		}
		return fmt.Errorf("движок завершился: %w", err)
	case err := <-tgDone:
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("telegram proxy завершился: %w", err)
	case <-time.After(350 * time.Millisecond):
	}
	if err := applyNftTable(cfg); err != nil {
		_ = cmd.Process.Kill()
		if tgCmd != nil {
			_ = tgCmd.Process.Kill()
		}
		<-done
		if tgDone != nil {
			<-tgDone
		}
		return err
	}
	go autoUpdateLoop()
	defer func() {
		_ = cmd.Process.Kill()
		if tgCmd != nil {
			_ = tgCmd.Process.Kill()
		}
		_ = deleteNftTable()
	}()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-done:
		return err
	case err := <-tgDone:
		return fmt.Errorf("telegram proxy завершился: %w", err)
	case <-sig:
		_ = cmd.Process.Signal(syscall.SIGTERM)
		if tgCmd != nil {
			_ = tgCmd.Process.Signal(syscall.SIGTERM)
		}
		deadline := time.After(3 * time.Second)
		for done != nil || tgDone != nil {
			select {
			case <-done:
				done = nil
			case <-tgDone:
				tgDone = nil
			case <-deadline:
				_ = cmd.Process.Kill()
				if tgCmd != nil {
					_ = tgCmd.Process.Kill()
				}
				return nil
			}
		}
		return nil
	}
}

func applyNftTable(cfg Config) error {
	udpPorts := "443, 19294-19344, 50000-50100"
	if cfg.GameMode && hasGameService(cfg.Services) {
		udpPorts += ", 3074, 3478-3480, 3659"
	}
	script := fmt.Sprintf(`table inet privetzapret {
  chain postrouting {
    type filter hook postrouting priority 101; policy accept;
    ip daddr { 2.26.225.138, 31.76.251.3 } accept
    meta mark and 0x40000000 == 0 meta l4proto tcp tcp dport { 443, 2053, 2083, 2087, 2096, 8443 } ct original packets 1-12 queue num 300 bypass
    meta mark and 0x40000000 == 0 meta l4proto udp udp dport { %s } ct original packets 1-12 queue num 300 bypass
  }
`, udpPorts)
	if cfg.AutoHostlist {
		script += `  chain input {
    type filter hook input priority -101; policy accept;
    ip saddr { 2.26.225.138, 31.76.251.3 } accept
    meta mark and 0x40000000 == 0 meta l4proto tcp tcp sport 443 ct reply packets 1-12 queue num 300 bypass
  }
`
	}
	script += "}\n"
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nftables: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func deleteNftTable() error {
	return exec.Command("nft", "delete", "table", "inet", "privetzapret").Run()
}

func platformOpenBrowser(url string) {
	user := os.Getenv("SUDO_USER")
	if user != "" && user != "root" {
		cmd := exec.Command("sudo", "-u", user, "xdg-open", url)
		cmd.Env = os.Environ()
		_ = cmd.Start()
		return
	}
	_ = exec.Command("xdg-open", url).Start()
}

func platformConnectTelegram() error {
	cfg := loadConfig(linuxConfigPath())
	if cfg.TelegramSecret == "" || !telegramProxyReady() {
		return errors.New("сначала запустите PrivetZapret")
	}
	platformOpenBrowser(telegramProxyURL(cfg.TelegramSecret))
	return nil
}
