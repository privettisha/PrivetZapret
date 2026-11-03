//go:build windows

package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

//go:embed payload/windows/*
var windowsPayload embed.FS

var windowsFiles = []string{"winws2.exe", "WinDivert.dll", "WinDivert64.sys", "cygwin1.dll", "PrivetTgWs.exe"}

// Keep engine assets in a versioned directory. An already loaded WinDivert
// driver may legitimately lock an older .sys file after winws2 has exited.
// A new release must never depend on overwriting that file in place.
const windowsEngineRevision = "engine-zapret2-1.0.6-r3"

func windowsInstallDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, appName)
}

func windowsConfigPath() string { return filepath.Join(windowsInstallDir(), "config.json") }

func windowsEngineDir() string { return filepath.Join(windowsInstallDir(), windowsEngineRevision) }

func windowsVersionPath() string { return filepath.Join(windowsInstallDir(), "version.txt") }

func platformHandleSpecialMode() bool {
	if len(os.Args) > 1 && os.Args[1] == "--service" {
		if err := svc.Run(serviceName, &zapretService{}); err != nil {
			fmt.Fprintln(os.Stderr, "PrivetZapret service:", err)
		}
		return true
	}
	return false
}

func platformIsAdministrator() bool {
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("IsUserAnAdmin")
	r, _, _ := proc.Call()
	return r != 0
}

func platformRelaunchElevated() {
	path, err := os.Executable()
	if err != nil {
		return
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	exe, _ := windows.UTF16PtrFromString(path)
	params, _ := windows.UTF16PtrFromString(strings.Join(os.Args[1:], " "))
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")
	_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(exe)), uintptr(unsafe.Pointer(params)), 0, 1)
}

func platformStatus() Status {
	installed, active := false, false
	m, err := mgr.Connect()
	if err == nil {
		defer m.Disconnect()
		if s, e := m.OpenService(serviceName); e == nil {
			installed = true
			if q, e := s.Query(); e == nil {
				active = q.State == svc.Running || q.State == svc.StartPending
			}
			s.Close()
		}
	}
	cfg := loadConfig(windowsConfigPath())
	return Status{Installed: installed, Active: active, Mode: cfg.Mode, Platform: "Windows · " + runtime.GOARCH, NeedsUpdate: installed && installedVersion(windowsVersionPath()) != appVersion, TelegramReady: active && hasService(cfg.Services, "telegram") && telegramProxyReady(), Services: cfg.Services, Catalog: serviceCatalog}
}

func platformInstall(mode string) error {
	if platformStatus().Installed {
		if err := platformStop(); err != nil {
			return fmt.Errorf("не удалось остановить предыдущую версию: %w", err)
		}
	}
	dir := windowsInstallDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	engineDir := windowsEngineDir()
	if err := os.MkdirAll(engineDir, 0755); err != nil {
		return err
	}
	if err := extractCommon(engineDir); err != nil {
		return err
	}
	for _, name := range windowsFiles {
		b, err := windowsPayload.ReadFile("payload/windows/" + name)
		if err != nil {
			return err
		}
		if err := writeFileIfChanged(filepath.Join(engineDir, name), b, 0644); err != nil {
			return fmt.Errorf("подготовка %s: %w", name, err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, appName+".exe")
	if !strings.EqualFold(filepath.Clean(self), dst) {
		b, err := os.ReadFile(self)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0755); err != nil {
			return err
		}
	}
	if err := saveConfig(windowsConfigPath(), mode); err != nil {
		return err
	}
	if err := writeFileIfChanged(windowsVersionPath(), []byte(appVersion+"\n"), 0644); err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		s, err = m.CreateService(serviceName, dst, mgr.Config{
			DisplayName: "PrivetZapret",
			Description: "Selected service connectivity",
			StartType:   mgr.StartAutomatic,
		}, "--service")
		if err != nil {
			return fmt.Errorf("создание службы: %w", err)
		}
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 2 * time.Second}}, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("запуск службы: %w", err)
	}
	return waitWindowsState(s, svc.Running, 8*time.Second)
}

func platformStart() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return errors.New("сначала установите PrivetZapret")
	}
	defer s.Close()
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	return waitWindowsState(s, svc.Running, 8*time.Second)
}

func platformStop() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return nil
	}
	defer s.Close()
	q, err := s.Query()
	if err == nil && q.State == svc.Stopped {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return err
	}
	return waitWindowsState(s, svc.Stopped, 8*time.Second)
}

func platformSetMode(mode string) error {
	wasActive := platformStatus().Active
	if wasActive {
		_ = platformStop()
	}
	if err := saveConfig(windowsConfigPath(), mode); err != nil {
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
	if err := saveServices(windowsConfigPath(), services); err != nil {
		return err
	}
	if wasActive {
		return platformStart()
	}
	return nil
}

func platformRemove() error {
	_ = platformStop()
	m, err := mgr.Connect()
	if err == nil {
		if s, e := m.OpenService(serviceName); e == nil {
			_ = s.Delete()
			s.Close()
		}
		m.Disconnect()
	}
	time.Sleep(250 * time.Millisecond)
	return os.RemoveAll(windowsInstallDir())
}

func waitWindowsState(s *mgr.Service, wanted svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		q, err := s.Query()
		if err == nil && q.State == wanted {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("служба не ответила вовремя")
}

type zapretService struct{}

func (z *zapretService) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	dir := windowsInstallDir()
	engineDir := windowsEngineDir()
	cfg := loadConfig(windowsConfigPath())
	if len(cfg.Services) == 0 {
		return false, 4
	}
	if err := writeSelectedHostlists(engineDir, cfg.Services); err != nil {
		return false, 4
	}
	args := []string{
		"--wf-tcp-out=443,2053,2083,2087,2096,8443",
		"--wf-udp-out=443,19294-19344,50000-50100",
		"--wf-raw-part=@" + filepath.Join(engineDir, "windivert_part.discord_media.txt"),
		"--wf-raw-part=@" + filepath.Join(engineDir, "windivert_part.quic_initial_ietf.txt"),
	}
	args = append(args, strategyArgs(engineDir, cfg.Mode, cfg.Services)...)
	zapCmd := exec.Command(filepath.Join(engineDir, "winws2.exe"), args...)
	zapCmd.Dir = engineDir
	zapCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var tgCmd *exec.Cmd
	var tgDone chan error
	if hasService(cfg.Services, "telegram") {
		tgCmd = exec.Command(filepath.Join(engineDir, "PrivetTgWs.exe"),
			"--host", "127.0.0.1", "--port", fmt.Sprint(tgProxyPort),
			"--log-file", filepath.Join(dir, "telegram.log"),
			"--log-max-mb", "3", "--log-backups", "1")
		tgCmd.Dir = engineDir
		tgCmd.Env = append(os.Environ(), "PRIVET_TG_SECRET="+cfg.TelegramSecret)
		tgCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	}
	logFile, _ := os.OpenFile(filepath.Join(dir, "service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logFile != nil {
		defer logFile.Close()
		zapCmd.Stdout, zapCmd.Stderr = logFile, logFile
		if tgCmd != nil {
			tgCmd.Stdout, tgCmd.Stderr = logFile, logFile
		}
	} else {
		zapCmd.Stdout, zapCmd.Stderr = os.Stdout, os.Stderr
		if tgCmd != nil {
			tgCmd.Stdout, tgCmd.Stderr = os.Stdout, os.Stderr
		}
	}
	if err := zapCmd.Start(); err != nil {
		return false, 1
	}
	zapDone := make(chan error, 1)
	go func() { zapDone <- zapCmd.Wait() }()
	if tgCmd != nil {
		if err := tgCmd.Start(); err != nil {
			_ = zapCmd.Process.Kill()
			<-zapDone
			return false, 3
		}
		tgDone = make(chan error, 1)
		go func() { tgDone <- tgCmd.Wait() }()
	}
	select {
	case <-zapDone:
		if tgCmd != nil {
			_ = tgCmd.Process.Kill()
			<-tgDone
		}
		changes <- svc.Status{State: svc.Stopped}
		return false, 2
	case <-tgDone:
		_ = zapCmd.Process.Kill()
		<-zapDone
		changes <- svc.Status{State: svc.Stopped}
		return false, 3
	case <-time.After(700 * time.Millisecond):
	}
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case <-zapDone:
			if tgCmd != nil {
				_ = tgCmd.Process.Kill()
				<-tgDone
			}
			changes <- svc.Status{State: svc.Stopped}
			return false, 2
		case <-tgDone:
			_ = zapCmd.Process.Kill()
			<-zapDone
			changes <- svc.Status{State: svc.Stopped}
			return false, 3
		case c := <-requests:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				_ = zapCmd.Process.Kill()
				if tgCmd != nil {
					_ = tgCmd.Process.Kill()
				}
				for zapDone != nil || tgDone != nil {
					select {
					case <-zapDone:
						zapDone = nil
					case <-tgDone:
						tgDone = nil
					case <-time.After(3 * time.Second):
					}
				}
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}

func platformOpenBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func platformConnectTelegram() error {
	cfg := loadConfig(windowsConfigPath())
	if cfg.TelegramSecret == "" || !telegramProxyReady() {
		return errors.New("сначала запустите PrivetZapret")
	}
	platformOpenBrowser(telegramProxyURL(cfg.TelegramSecret))
	return nil
}
