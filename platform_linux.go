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
	"strings"
	"syscall"
	"time"
)

const linuxInstallDir = "/opt/privetzapret"
const linuxUnitPath = "/etc/systemd/system/privetzapret.service"

//go:embed payload/linux/*
var linuxPayload embed.FS

func platformHandleSpecialMode() bool {
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

func linuxVersionPath() string { return filepath.Join(linuxInstallDir, "version.txt") }

func platformStatus() Status {
	_, statErr := os.Stat(linuxUnitPath)
	active := exec.Command("systemctl", "is-active", "--quiet", "privetzapret.service").Run() == nil
	installed := statErr == nil
	return Status{Installed: installed, Active: active, Mode: loadConfig(linuxConfigPath()).Mode, Platform: "Linux · " + runtime.GOARCH, NeedsUpdate: installed && installedVersion(linuxVersionPath()) != appVersion}
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
Description=PrivetZapret for YouTube and Discord
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
	args := append([]string{"--qnum=300"}, strategyArgs(linuxInstallDir, loadConfig(linuxConfigPath()).Mode)...)
	cmd := exec.Command(filepath.Join(linuxInstallDir, "nfqws2"), args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return fmt.Errorf("движок завершился: %w", err)
	case <-time.After(350 * time.Millisecond):
	}
	if err := applyNftTable(); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	defer deleteNftTable()

	sig := make(chan os.Signal, 2)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-done:
		return err
	case <-sig:
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
		}
		return nil
	}
}

func applyNftTable() error {
	script := `table inet privetzapret {
  chain postrouting {
    type filter hook postrouting priority 101; policy accept;
    meta mark and 0x40000000 == 0 meta l4proto tcp tcp dport 443 ct original packets 1-12 queue num 300 bypass
    meta mark and 0x40000000 == 0 meta l4proto udp udp dport { 443, 19294-19344, 50000-50099 } ct original packets 1-12 queue num 300 bypass
  }
}
`
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
