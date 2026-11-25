//go:build windows

package main

import (
	"context"
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
var windowsClassicFiles = []string{"winws.exe", "WinDivert.dll", "WinDivert64.sys", "cygwin1.dll"}

// Keep engine assets in a versioned directory. An already loaded WinDivert
// driver may legitimately lock an older .sys file after winws has exited.
// A new release must never depend on overwriting that file in place.
const windowsEngineRevision = "engine-zapret1-72.9-zapret2-1.0.7-tgws-1.12.0"

func windowsInstallDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, appName)
}

func windowsConfigPath() string { return filepath.Join(windowsInstallDir(), "config.json") }

func windowsEngineDir() string  { return filepath.Join(windowsInstallDir(), windowsEngineRevision) }
func windowsClassicDir() string { return filepath.Join(windowsEngineDir(), "classic") }

func windowsVersionPath() string      { return filepath.Join(windowsInstallDir(), "version.txt") }
func platformInstalledBinary() string { return filepath.Join(windowsInstallDir(), appName+".exe") }

func platformHandleSpecialMode() bool {
	if len(os.Args) > 1 && os.Args[1] == "--silent-install" {
		time.Sleep(2 * time.Second)
		if err := platformInstall(loadConfig(windowsConfigPath()).Mode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return true
	}
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
	serviceRunning := active
	engineProcesses, _ := windowsEngines(false)
	if engineProcesses > 0 {
		active = true
	}
	cfg := loadConfig(windowsConfigPath())
	version := installedVersion(windowsVersionPath())
	status := statusSettings(Status{Installed: installed, Active: active, ServiceRunning: serviceRunning, EngineProcesses: engineProcesses, Mode: cfg.Mode, YoutubeProfile: cfg.YoutubeProfile, Platform: "Windows · " + runtime.GOARCH, InstalledVersion: version, NeedsUpdate: installed && version != appVersion, TelegramReady: active && hasService(cfg.Services, "telegram") && telegramProxyReady(), Services: cfg.Services, Catalog: serviceCatalog}, cfg)
	status.DNSSupported = true
	status.DNSActive = active && cfg.DNSEnabled && windowsDNSApplied()
	_, err = os.Stat(rollbackPath())
	status.RollbackAvailable = err == nil
	return status
}

func platformInstall(mode string) error {
	status := platformStatus()
	previousVersion := status.InstalledVersion
	if status.Installed || status.Active {
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
	if err := os.MkdirAll(windowsClassicDir(), 0755); err != nil {
		return err
	}
	for _, name := range windowsClassicFiles {
		source := "payload/windows/" + name
		if name == "winws.exe" || name == "WinDivert.dll" {
			source = "payload/windows/classic/" + name
		}
		b, err := windowsPayload.ReadFile(source)
		if err != nil {
			return err
		}
		if err := writeFileIfChanged(filepath.Join(windowsClassicDir(), name), b, 0644); err != nil {
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
	if previousVersion != "" && previousVersion != appVersion {
		cfg := loadConfig(windowsConfigPath())
		cfg.Mode, cfg.YoutubeProfile = "balanced", "standard"
		if err := saveConfigValue(windowsConfigPath(), cfg); err != nil {
			return err
		}
	} else if err := saveConfig(windowsConfigPath(), mode); err != nil {
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
			StartType:   mgr.StartManual,
		}, "--service")
		if err != nil {
			return fmt.Errorf("создание службы: %w", err)
		}
	}
	defer s.Close()
	if err := setWindowsServiceStartup(s, mgr.StartManual); err != nil {
		return fmt.Errorf("настройка ручного запуска службы: %w", err)
	}
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 2 * time.Second}}, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("запуск службы: %w", err)
	}
	if err := waitWindowsState(s, svc.Running, 8*time.Second); err != nil {
		return err
	}
	return verifyWindowsStarted()
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
	if err := setWindowsServiceStartup(s, mgr.StartManual); err != nil {
		return fmt.Errorf("включение службы: %w", err)
	}
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	if err := waitWindowsState(s, svc.Running, 8*time.Second); err != nil {
		return err
	}
	return verifyWindowsStarted()
}

func verifyWindowsStarted() error {
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
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	var stopErr error
	s, err := m.OpenService(serviceName)
	if err == nil {
		defer s.Close()
		q, queryErr := s.Query()
		if queryErr != nil {
			stopErr = queryErr
		} else if q.State != svc.Stopped {
			if _, controlErr := s.Control(svc.Stop); controlErr != nil && !errors.Is(controlErr, windows.ERROR_SERVICE_NOT_ACTIVE) {
				stopErr = controlErr
			} else {
				stopErr = waitWindowsState(s, svc.Stopped, 8*time.Second)
			}
		}
		stopErr = errors.Join(stopErr, setWindowsServiceStartup(s, mgr.StartDisabled))
	} else if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		stopErr = err
	}
	// A crashed service can leave winws, winws2 or the Telegram proxy running on its
	// own. Only terminate executables inside this application's install dir.
	engineErr := stopWindowsEngines()
	dnsErr := restoreWindowsDNS()
	return errors.Join(stopErr, engineErr, dnsErr)
}

func setWindowsServiceStartup(s *mgr.Service, startType uint32) error {
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if cfg.StartType == startType {
		return nil
	}
	cfg.StartType = startType
	return s.UpdateConfig(cfg)
}

func platformSetDNS(rawURL string, enabled bool) error {
	cfg := loadConfig(windowsConfigPath())
	if rawURL == "" && !enabled {
		cfg.DNSURL = ""
	} else if rawURL != "" {
		url, err := validateDNSEndpoint(rawURL)
		if err != nil {
			return err
		}
		cfg.DNSURL = url
	}
	if !enabled && !platformStatus().Active {
		if err := restoreWindowsDNS(); err != nil {
			return err
		}
	}
	if enabled {
		if cfg.DNSURL == "" {
			return errors.New("вставьте адрес DoH или DoT")
		}
		status := platformStatus()
		if status.NeedsUpdate {
			return errors.New("сначала обновите PrivetZapret до версии с системным DoH")
		}
		if !status.Active {
			return errors.New("сначала запустите PrivetZapret")
		}
		state, err := readWindowsDNSState()
		if errors.Is(err, os.ErrNotExist) {
			state, err = windowsDNSOriginal()
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

func platformSetMode(mode string) error {
	wasActive := platformStatus().Active
	if wasActive {
		if err := platformStop(); err != nil {
			return err
		}
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

func platformSetYoutubeProfile(profile string) error {
	if normalizeYoutubeProfile(profile) != profile {
		return errors.New("неизвестный профиль YouTube")
	}
	previous := loadConfig(windowsConfigPath()).YoutubeProfile
	wasActive := platformStatus().Active
	if wasActive {
		if err := platformStop(); err != nil {
			return err
		}
	}
	if err := saveYoutubeProfile(windowsConfigPath(), profile); err != nil {
		if wasActive {
			_ = platformStart()
		}
		return err
	}
	if wasActive {
		if err := platformStart(); err != nil {
			_ = saveYoutubeProfile(windowsConfigPath(), previous)
			_ = platformStart()
			return err
		}
	}
	return nil
}

func platformConfigPath() string { return windowsConfigPath() }

func platformApplyConfig(cfg Config) error {
	previous := loadConfig(windowsConfigPath())
	wasActive := platformStatus().Active
	if wasActive {
		if err := platformStop(); err != nil {
			return err
		}
	}
	if err := saveConfigValue(windowsConfigPath(), cfg); err != nil {
		if wasActive {
			_ = platformStart()
		}
		return err
	}
	if wasActive {
		if err := platformStart(); err != nil {
			_ = saveConfigValue(windowsConfigPath(), previous)
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
	_ = exec.Command("ipconfig", "/flushdns").Run()
	return nil
}

func platformOtherFilters() []string {
	out, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil
	}
	text := strings.ToLower(string(out))
	var found []string
	for _, name := range []string{"goodbyedpi.exe", "nfqws.exe", "nfqws2.exe", "tpws.exe", "winws.exe"} {
		if strings.Contains(text, "\""+name+"\"") {
			found = append(found, name)
		}
	}
	return found
}

func platformRemove() error {
	if err := platformStop(); err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serviceName); err == nil {
		deleteErr := s.Delete()
		s.Close()
		if deleteErr != nil {
			return fmt.Errorf("удаление службы: %w", deleteErr)
		}
	} else if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("поиск службы: %w", err)
	}
	time.Sleep(250 * time.Millisecond)
	return os.RemoveAll(windowsInstallDir())
}

func stopWindowsEngines() error {
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := windowsEngines(true); err != nil {
			return err
		}
		time.Sleep(150 * time.Millisecond)
		remaining, err := windowsEngines(false)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return nil
		}
	}
	return errors.New("процессы движка остались после остановки")
}

func windowsEngines(terminate bool) (int, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return 0, err
	}
	installDir := strings.ToLower(filepath.Clean(windowsInstallDir())) + `\`
	found := 0
	for {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		if name == "winws.exe" || name == "winws2.exe" || name == "privettgws.exe" {
			access := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION)
			if terminate {
				access |= windows.PROCESS_TERMINATE | windows.SYNCHRONIZE
			}
			process, openErr := windows.OpenProcess(access, false, entry.ProcessID)
			if openErr == nil {
				path := make([]uint16, 32768)
				size := uint32(len(path))
				if queryErr := windows.QueryFullProcessImageName(process, 0, &path[0], &size); queryErr == nil &&
					strings.HasPrefix(strings.ToLower(filepath.Clean(windows.UTF16ToString(path[:size]))), installDir) {
					found++
					if terminate {
						if killErr := windows.TerminateProcess(process, 0); killErr != nil {
							windows.CloseHandle(process)
							return found, fmt.Errorf("остановка %s: %w", name, killErr)
						}
						_, _ = windows.WaitForSingleObject(process, 3000)
					}
				}
				windows.CloseHandle(process)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return found, nil
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
	defer func() { _ = stopWindowsEngines() }()
	dir := windowsInstallDir()
	engineDir := windowsEngineDir()
	cfg := loadConfig(windowsConfigPath())
	defer func() { _ = restoreWindowsDNS() }()
	if !cfg.DNSEnabled {
		if err := restoreWindowsDNS(); err != nil {
			return false, 5
		}
	}
	if len(cfg.Services) == 0 {
		return false, 4
	}
	if err := writeSelectedHostlistsConfig(engineDir, cfg); err != nil {
		return false, 4
	}
	args := []string{
		"--wf-tcp-out=443,2053,2083,2087,2096,8443",
		"--wf-udp-out=443,19294-19344,50000-50100",
		"--wf-raw-part=@" + filepath.Join(engineDir, "windivert_part.discord_media.txt"),
		"--wf-raw-part=@" + filepath.Join(engineDir, "windivert_part.quic_initial_ietf.txt"),
	}
	if cfg.GameMode && hasGameService(cfg.Services) {
		args[1] = "--wf-udp-out=443,19294-19344,50000-50100,3074,3478-3480,3659"
	}
	if cfg.AutoHostlist {
		args = append(args, "--wf-raw-part=@"+filepath.Join(engineDir, "windivert_part.tls_reply_in.txt"))
		args = append(args, "--wf-raw-part=@"+filepath.Join(engineDir, "windivert_part.quic_initial_in.txt"))
	}
	enginePath, engineWorkDir := filepath.Join(engineDir, "winws2.exe"), engineDir
	if hasService(cfg.Services, "youtube") && normalizeYoutubeProfile(cfg.YoutubeProfile) == "standard" {
		args = provenWindowsStrategyArgs(engineDir, cfg)
		enginePath, engineWorkDir = filepath.Join(windowsClassicDir(), "winws.exe"), windowsClassicDir()
	} else {
		args = append(args, strategyArgsConfig(engineDir, cfg)...)
	}
	zapCmd := exec.Command(enginePath, args...)
	zapCmd.Dir = engineWorkDir
	zapCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var tgCmd *exec.Cmd
	var tgDone chan error
	if hasService(cfg.Services, "telegram") {
		tgCmd = exec.Command(filepath.Join(engineDir, "PrivetTgWs.exe"), telegramProxyArgsConfig(dir, cfg.TelegramRoute)...)
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
	var dnsRuntime *dnsProxy
	stopDNS := func() {
		if dnsRuntime != nil {
			_ = restoreWindowsDNS()
			dnsRuntime.Close()
			dnsRuntime = nil
		}
	}
	defer stopDNS()
	if cfg.DNSEnabled {
		state, err := readWindowsDNSState()
		if errors.Is(err, os.ErrNotExist) {
			state, err = windowsDNSOriginal()
		}
		if err != nil {
			_ = zapCmd.Process.Kill()
			if tgCmd != nil {
				_ = tgCmd.Process.Kill()
			}
			return false, 5
		}
		proxy, err := newDNSProxy(cfg.DNSURL, state.Original)
		if err == nil {
			err = proxy.listen(state.IPv6Enabled)
		}
		if err == nil {
			_, err = applyWindowsDNS()
		}
		if err != nil {
			if proxy != nil {
				proxy.Close()
			}
			_ = zapCmd.Process.Kill()
			if tgCmd != nil {
				_ = tgCmd.Process.Kill()
			}
			return false, 5
		}
		dnsRuntime = proxy
	}
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	go autoUpdateLoop()
	leaseTicker := time.NewTicker(3 * time.Second)
	defer leaseTicker.Stop()
	for {
		select {
		case <-zapDone:
			if tgCmd != nil {
				_ = tgCmd.Process.Kill()
				<-tgDone
			}
			stopDNS()
			changes <- svc.Status{State: svc.Stopped}
			return false, 2
		case <-tgDone:
			_ = zapCmd.Process.Kill()
			<-zapDone
			stopDNS()
			changes <- svc.Status{State: svc.Stopped}
			return false, 3
		case <-leaseTicker.C:
			if !controlLeaseAlive() {
				changes <- svc.Status{State: svc.StopPending}
				_ = zapCmd.Process.Kill()
				if tgCmd != nil {
					_ = tgCmd.Process.Kill()
				}
				stopDNS()
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
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
				stopDNS()
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		}
	}
}

func platformOpenBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
