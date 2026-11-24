//go:build linux

package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	userpkg "os/user"
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
		if err := platformInstall(loadConfig(linuxConfigPath()).Mode); err != nil {
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
	if pkexec, err := exec.LookPath("pkexec"); err == nil && os.Getenv("DISPLAY") != "" {
		args := []string{"/usr/bin/env"}
		for _, key := range []string{"DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
			if value := os.Getenv(key); value != "" {
				args = append(args, key+"="+value)
			}
		}
		if current, err := userpkg.Current(); err == nil {
			args = append(args, "SUDO_USER="+current.Username, "SUDO_UID="+current.Uid)
		}
		cmd := exec.Command(pkexec, append(args, append([]string{path}, os.Args[1:]...)...)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "PrivetZapret: не удалось получить права через polkit:", err)
		}
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
	serviceRunning := exec.Command("systemctl", "is-active", "--quiet", "privetzapret.service").Run() == nil
	pids, _ := linuxEnginePIDs()
	active := serviceRunning || len(pids) > 0
	installed := statErr == nil
	cfg := loadConfig(linuxConfigPath())
	version := installedVersion(linuxVersionPath())
	status := statusSettings(Status{Installed: installed, Active: active, ServiceRunning: serviceRunning, EngineProcesses: len(pids), Mode: cfg.Mode, YoutubeProfile: cfg.YoutubeProfile, Platform: "Linux · " + runtime.GOARCH, InstalledVersion: version, NeedsUpdate: installed && version != appVersion, TelegramReady: active && hasService(cfg.Services, "telegram") && telegramProxyReady(), Services: cfg.Services, Catalog: serviceCatalog}, cfg)
	status.DNSSupported = true
	status.DNSActive = active && cfg.DNSEnabled && linuxDNSApplied()
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
KillMode=control-group

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile(linuxUnitPath, []byte(unit), 0644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemd: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "start", "privetzapret.service").CombinedOutput(); err != nil {
		return fmt.Errorf("запуск: %s", strings.TrimSpace(string(out)))
	}
	return verifyLinuxStarted()
}

func platformStart() error {
	out, err := exec.Command("systemctl", "start", "privetzapret.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("запуск: %s", strings.TrimSpace(string(out)))
	}
	return verifyLinuxStarted()
}

func verifyLinuxStarted() error {
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		status := platformStatus()
		if status.ServiceRunning && status.EngineProcesses > 0 && (!hasService(status.Services, "telegram") || status.TelegramReady) && (!status.DNSEnabled || status.DNSActive) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("движок не запустился; откройте «Проверка» и журнал службы")
}

func platformStop() error {
	out, err := exec.Command("systemctl", "disable", "--now", "privetzapret.service").CombinedOutput()
	// The nft queue and engine may survive a crashed or manually killed daemon.
	// Clean up only PrivetZapret's own resources even if systemd reports failure.
	orphanErr := stopLinuxEngines()
	_ = deleteNftTable()
	dnsErr := restoreLinuxDNS()
	warpErr := stopYouTubeWarp()
	if err != nil {
		return errors.Join(fmt.Errorf("остановка: %s", strings.TrimSpace(string(out))), orphanErr, dnsErr, warpErr)
	}
	return errors.Join(orphanErr, dnsErr, warpErr)
}

func linuxEnginePIDs() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := 0
		if _, err := fmt.Sscanf(entry.Name(), "%d", &pid); err != nil || pid <= 1 {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil {
			continue
		}
		exe = strings.TrimSuffix(exe, " (deleted)")
		if exe != filepath.Join(linuxInstallDir, "nfqws2") && exe != filepath.Join(linuxInstallDir, "PrivetTgWs") {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func stopLinuxEngines() error {
	pids, err := linuxEnginePIDs()
	if err != nil {
		return err
	}
	var stopErr error
	for _, pid := range pids {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			stopErr = errors.Join(stopErr, fmt.Errorf("остановка движка %d: %w", pid, err))
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		remaining, err := linuxEnginePIDs()
		if err != nil {
			return errors.Join(stopErr, err)
		}
		if len(remaining) == 0 {
			return stopErr
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.Join(stopErr, errors.New("движки не завершились после остановки"))
}

func platformSetMode(mode string) error {
	wasActive := platformStatus().Active
	if wasActive {
		if err := platformStop(); err != nil {
			return err
		}
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

func platformProtectConfig(_ string) error { return nil }

func platformSetDNS(rawURL string, enabled bool) error {
	cfg := loadConfig(linuxConfigPath())
	if rawURL == "" && !enabled {
		cfg.DNSURL = ""
	} else if rawURL != "" {
		endpoint, err := validateDNSEndpoint(rawURL)
		if err != nil {
			return err
		}
		cfg.DNSURL = endpoint
	}
	if enabled {
		if cfg.DNSURL == "" {
			return errors.New("вставьте адрес DoH или DoT")
		}
		if !platformStatus().Active {
			return errors.New("сначала запустите PrivetZapret")
		}
		state, err := readLinuxDNSState()
		if errors.Is(err, os.ErrNotExist) {
			state, err = snapshotLinuxDNS(linuxResolvPath)
		}
		if err != nil {
			return err
		}
		proxy, err := newDNSProxy(cfg.DNSURL, state.Original)
		if err != nil {
			return err
		}
		defer proxy.Close()
		query := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := proxy.secure(ctx, query); err != nil {
			return fmt.Errorf("DNS-сервер не ответил: %w", err)
		}
	}
	cfg.DNSEnabled = enabled
	return platformApplyConfig(cfg)
}

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
	if err := platformStop(); err != nil {
		return err
	}
	if err := os.Remove(linuxUnitPath); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("обновление служб: %s", strings.TrimSpace(string(out)))
	}
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
		tgCmd = exec.Command(filepath.Join(linuxInstallDir, "PrivetTgWs"), telegramProxyArgsConfig(linuxInstallDir, cfg.TelegramRoute)...)
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

	leaseTicker := time.NewTicker(3 * time.Second)
	defer leaseTicker.Stop()
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
		_ = stopLinuxEngines()
		_ = deleteNftTable()
		_ = stopYouTubeWarp()
	}()
	if cfg.DNSEnabled {
		state, err := readLinuxDNSState()
		if errors.Is(err, os.ErrNotExist) {
			state, err = snapshotLinuxDNS(linuxResolvPath)
		}
		if err != nil {
			return err
		}
		proxy, err := newDNSProxy(cfg.DNSURL, state.Original)
		if err != nil {
			return err
		}
		if err := proxy.listen(false); err != nil {
			proxy.Close()
			return err
		}
		defer func() {
			_ = restoreLinuxDNS()
			proxy.Close()
		}()
		if err := applyLinuxDNS(state); err != nil {
			return err
		}
	}

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	for {
		select {
		case err := <-done:
			return err
		case err := <-tgDone:
			return fmt.Errorf("telegram proxy завершился: %w", err)
		case <-leaseTicker.C:
			if !controlLeaseAlive() {
				return nil
			}
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
    meta mark and 0x40000000 == 0 meta l4proto udp udp sport 443 ct reply packets 1-12 queue num 300 bypass
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
	if user == "" && os.Getenv("PKEXEC_UID") != "" {
		if account, err := userpkg.LookupId(os.Getenv("PKEXEC_UID")); err == nil {
			user = account.Username
		}
	}
	if user != "" && user != "root" {
		cmd := exec.Command("sudo", "-u", user, "xdg-open", url)
		cmd.Env = os.Environ()
		if account, err := userpkg.Lookup(user); err == nil {
			cmd.Env = append(cmd.Env, "HOME="+account.HomeDir, "USER="+user, "LOGNAME="+user)
		}
		_ = cmd.Start()
		return
	}
	_ = exec.Command("xdg-open", url).Start()
}
