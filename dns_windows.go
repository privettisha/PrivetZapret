//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// SetInterfaceDnsSettings calling convention follows the MIT-licensed
// WireGuard for Windows winipcfg implementation (WireGuard LLC, 2019-2026).
type dnsInterfaceSettings struct {
	Version             uint32
	_                   [4]byte
	Flags               uint64
	Domain              *uint16
	NameServer          *uint16
	SearchList          *uint16
	RegistrationEnabled uint32
	RegisterAdapterName uint32
	EnableLLMNR         uint32
	QueryAdapterName    uint32
	ProfileNameServer   *uint16
}

var setDNSProc = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("SetInterfaceDnsSettings")

type windowsDNSState struct {
	GUID        string   `json:"guid"`
	IPv6Enabled bool     `json:"ipv6_enabled"`
	Static4     string   `json:"static4"`
	Static6     string   `json:"static6"`
	Original    []string `json:"original"`
}

type windowsDNSAdapter struct {
	GUID        string
	IfIndex     uint32
	IfType      uint32
	OperStatus  uint32
	IPv6Enabled bool
	DNS4        []string
	DNS6        []string
}

func windowsDNSStatePath() string { return filepath.Join(windowsInstallDir(), "dns-state.json") }

func platformProtectConfig(path string) error {
	cmd := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", "*S-1-5-18:F", "*S-1-5-32-544:F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("не удалось защитить DNS и ключи: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func windowsDNSApplied() bool {
	state, err := readWindowsDNSState()
	if err != nil {
		return false
	}
	items, err := windowsDNSAdapters()
	if err != nil {
		return false
	}
	for _, item := range items {
		if strings.EqualFold(strings.Trim(item.GUID, "{}"), strings.Trim(state.GUID, "{}")) {
			return len(item.DNS4) == 1 && item.DNS4[0] == "127.0.0.1" &&
				(!state.IPv6Enabled || len(item.DNS6) == 1 && item.DNS6[0] == "::1")
		}
	}
	return false
}

func windowsDNSAdapters() ([]windowsDNSAdapter, error) {
	size := uint32(15 * 1024)
	for attempt := 0; attempt < 4; attempt++ {
		buffer := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var result []windowsDNSAdapter
		for current := first; current != nil; current = current.Next {
			item := windowsDNSAdapter{
				GUID: windows.BytePtrToString(current.AdapterName), IfIndex: current.IfIndex,
				IfType: current.IfType, OperStatus: current.OperStatus,
				IPv6Enabled: current.Flags&0x100 != 0 && current.Ipv6IfIndex != 0,
			}
			for server := current.FirstDnsServerAddress; server != nil; server = server.Next {
				ip := server.Address.IP()
				if ip == nil {
					continue
				}
				if ip.To4() != nil {
					item.DNS4 = append(item.DNS4, ip.String())
				} else {
					item.DNS6 = append(item.DNS6, ip.String())
				}
			}
			result = append(result, item)
		}
		runtime.KeepAlive(buffer)
		return result, nil
	}
	return nil, errors.New("Windows вернула слишком большой список сетевых адаптеров")
}

func windowsDefaultDNSAdapter() (windowsDNSAdapter, error) {
	var index uint32
	if err := windows.GetBestInterfaceEx(&windows.SockaddrInet4{Addr: [4]byte{1, 1, 1, 1}}, &index); err != nil {
		return windowsDNSAdapter{}, errors.New("нет активного подключения к интернету")
	}
	items, err := windowsDNSAdapters()
	if err != nil {
		return windowsDNSAdapter{}, err
	}
	for _, item := range items {
		if item.IfIndex != index {
			continue
		}
		if item.OperStatus != windows.IfOperStatusUp || (item.IfType != windows.IF_TYPE_ETHERNET_CSMACD && item.IfType != windows.IF_TYPE_IEEE80211) {
			return windowsDNSAdapter{}, errors.New("основное подключение должно быть активным Wi-Fi или Ethernet без VPN")
		}
		return item, nil
	}
	return windowsDNSAdapter{}, errors.New("не найден основной сетевой адаптер")
}

func windowsStaticDNS(guid string, ipv6 bool) (string, error) {
	service := "Tcpip"
	if ipv6 {
		service = "Tcpip6"
	}
	path := `SYSTEM\CurrentControlSet\Services\` + service + `\Parameters\Interfaces\{` + strings.Trim(guid, "{}") + `}`
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue("NameServer")
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	var servers []string
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' }) {
		ip := net.ParseIP(part)
		if ip == nil || (ipv6 && ip.To4() != nil) || (!ipv6 && ip.To4() == nil) {
			return "", errors.New("статические настройки DNS повреждены")
		}
		servers = append(servers, ip.String())
	}
	return strings.Join(servers, ","), nil
}

func windowsDNSOriginal() (windowsDNSState, error) {
	item, err := windowsDefaultDNSAdapter()
	if err != nil {
		return windowsDNSState{}, err
	}
	for _, server := range append(append([]string{}, item.DNS4...), item.DNS6...) {
		if ip := net.ParseIP(server); ip != nil && ip.IsLoopback() {
			return windowsDNSState{}, errors.New("на основном подключении уже настроен локальный DNS; сначала отключите другое DNS-приложение")
		}
	}
	static4, err := windowsStaticDNS(item.GUID, false)
	if err != nil {
		return windowsDNSState{}, err
	}
	static6, err := windowsStaticDNS(item.GUID, true)
	if err != nil {
		return windowsDNSState{}, err
	}
	return windowsDNSState{GUID: item.GUID, IPv6Enabled: item.IPv6Enabled, Static4: static4, Static6: static6, Original: append(item.DNS4, item.DNS6...)}, nil
}

func windowsSetDNS(guid string, ipv6 bool, servers string) error {
	parsed, err := windows.GUIDFromString("{" + strings.Trim(guid, "{}") + "}")
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(servers)
	if err != nil {
		return err
	}
	if err := setDNSProc.Find(); err != nil {
		return windowsSetDNSFallback(guid, ipv6, servers)
	}
	settings := &dnsInterfaceSettings{Version: 1, Flags: 0x0002, NameServer: name}
	if ipv6 {
		settings.Flags |= 0x0001
	}
	var code uintptr
	switch runtime.GOARCH {
	case "amd64":
		code, _, _ = setDNSProc.Call(uintptr(unsafe.Pointer(&parsed)), uintptr(unsafe.Pointer(settings)))
	case "arm64":
		words := *(*[2]uintptr)(unsafe.Pointer(&parsed))
		code, _, _ = setDNSProc.Call(words[0], words[1], uintptr(unsafe.Pointer(settings)))
	default:
		return errors.New("неподдерживаемая архитектура Windows")
	}
	runtime.KeepAlive(parsed)
	runtime.KeepAlive(name)
	runtime.KeepAlive(settings)
	if code != 0 {
		if err := windowsSetDNSFallback(guid, ipv6, servers); err != nil {
			return fmt.Errorf("Windows DNS API: %v; netsh: %w", syscall.Errno(code), err)
		}
	}
	return nil
}

func windowsSetDNSFallback(guid string, ipv6 bool, servers string) error {
	items, err := windowsDNSAdapters()
	if err != nil {
		return err
	}
	var index uint32
	for _, item := range items {
		if strings.EqualFold(strings.Trim(item.GUID, "{}"), strings.Trim(guid, "{}")) {
			index = item.IfIndex
			break
		}
	}
	if index == 0 {
		return errors.New("сетевой адаптер не найден")
	}
	protocol := "ipv4"
	if ipv6 {
		protocol = "ipv6"
	}
	name := "name=" + strconv.FormatUint(uint64(index), 10)
	run := func(args ...string) error {
		cmd := exec.Command("netsh.exe", args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
		}
		return nil
	}
	if servers == "" {
		return run("interface", protocol, "set", "dnsservers", name, "source=dhcp")
	}
	values := strings.Split(servers, ",")
	if err := run("interface", protocol, "set", "dnsservers", name, "source=static", "address="+values[0], "validate=no"); err != nil {
		return err
	}
	for i, value := range values[1:] {
		if err := run("interface", protocol, "add", "dnsservers", name, "address="+value, "index="+strconv.Itoa(i+2), "validate=no"); err != nil {
			return err
		}
	}
	return nil
}

func readWindowsDNSState() (windowsDNSState, error) {
	data, err := os.ReadFile(windowsDNSStatePath())
	if err != nil {
		return windowsDNSState{}, err
	}
	var state windowsDNSState
	if err := json.Unmarshal(data, &state); err != nil || state.GUID == "" {
		return windowsDNSState{}, errors.New("сохранённые настройки DNS повреждены")
	}
	return state, nil
}

func saveWindowsDNSState(state windowsDNSState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := windowsDNSStatePath()
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	if err := platformProtectConfig(path + ".tmp"); err != nil {
		_ = os.Remove(path + ".tmp")
		return err
	}
	return os.Rename(path+".tmp", path)
}

func restoreWindowsDNS() error {
	state, err := readWindowsDNSState()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	items, err := windowsDNSAdapters()
	if err != nil {
		return err
	}
	var current windowsDNSAdapter
	found := false
	for _, item := range items {
		if strings.EqualFold(strings.Trim(item.GUID, "{}"), strings.Trim(state.GUID, "{}")) {
			current = item
			found = true
			break
		}
	}
	if !found {
		return errors.New("адаптер с прежними настройками DNS не найден")
	}
	static4, err := windowsStaticDNS(state.GUID, false)
	if err != nil {
		return err
	}
	restore4 := static4 == "127.0.0.1" || dnsListContains(current.DNS4, "127.0.0.1")
	if restore4 {
		if err := windowsSetDNS(state.GUID, false, state.Static4); err != nil {
			return fmt.Errorf("восстановление IPv4 DNS: %w", err)
		}
	}
	restore6 := false
	if state.IPv6Enabled {
		static6, err := windowsStaticDNS(state.GUID, true)
		if err != nil {
			return err
		}
		restore6 = static6 == "::1" || dnsListContains(current.DNS6, "::1")
		if restore6 {
			if err := windowsSetDNS(state.GUID, true, state.Static6); err != nil {
				return fmt.Errorf("восстановление IPv6 DNS: %w", err)
			}
		}
	}
	if restore4 || restore6 {
		if err := waitWindowsDNS(state.GUID, state.Static4, state.Static6, restore4, restore6); err != nil {
			return fmt.Errorf("не удалось проверить восстановленный DNS: %w", err)
		}
	}
	return os.Remove(windowsDNSStatePath())
}

func waitWindowsDNS(guid, expected4, expected6 string, check4, check6 bool) error {
	for attempt := 0; attempt < 20; attempt++ {
		staticOK := true
		if check4 {
			actual4, err := windowsStaticDNS(guid, false)
			staticOK = err == nil && actual4 == expected4
		}
		if check6 {
			actual6, err := windowsStaticDNS(guid, true)
			staticOK = staticOK && err == nil && actual6 == expected6
		}
		if staticOK {
			items, err := windowsDNSAdapters()
			if err == nil {
				for _, item := range items {
					if !strings.EqualFold(strings.Trim(item.GUID, "{}"), strings.Trim(guid, "{}")) {
						continue
					}
					live4 := !check4 || (expected4 == "127.0.0.1" && len(item.DNS4) == 1 && item.DNS4[0] == expected4) || (expected4 != "127.0.0.1" && !dnsListContains(item.DNS4, "127.0.0.1"))
					live6 := !check6 || (expected6 == "::1" && len(item.DNS6) == 1 && item.DNS6[0] == expected6) || (expected6 != "::1" && !dnsListContains(item.DNS6, "::1"))
					if live4 && live6 {
						return nil
					}
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("Windows не подтвердила настройки DNS адаптера")
}

func dnsListContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func applyWindowsDNS() ([]string, error) {
	state, err := readWindowsDNSState()
	if errors.Is(err, os.ErrNotExist) {
		state, err = windowsDNSOriginal()
		if err != nil {
			return nil, err
		}
		if err := saveWindowsDNSState(state); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		current, err := windowsDefaultDNSAdapter()
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(strings.Trim(current.GUID, "{}"), strings.Trim(state.GUID, "{}")) ||
			!dnsListContains(current.DNS4, "127.0.0.1") ||
			(state.IPv6Enabled && !dnsListContains(current.DNS6, "::1")) {
			return nil, errors.New("сетевое подключение или DNS изменились; перезапустите PrivetZapret")
		}
	}
	if err := windowsSetDNS(state.GUID, false, "127.0.0.1"); err != nil {
		_ = restoreWindowsDNS()
		return nil, err
	}
	if state.IPv6Enabled {
		if err := windowsSetDNS(state.GUID, true, "::1"); err != nil {
			_ = restoreWindowsDNS()
			return nil, err
		}
	}
	if err := waitWindowsDNS(state.GUID, "127.0.0.1", "::1", true, state.IPv6Enabled); err != nil {
		_ = restoreWindowsDNS()
		return nil, err
	}
	return state.Original, nil
}
