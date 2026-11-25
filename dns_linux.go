//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const linuxResolvPath = "/etc/resolv.conf"
const linuxDNSMarker = "# Managed by PrivetZapret; restored when stopped\nnameserver 127.0.0.1\n"

type linuxDNSState struct {
	Link     string   `json:"link,omitempty"`
	Contents []byte   `json:"contents"`
	Mode     uint32   `json:"mode"`
	UID      int      `json:"uid"`
	GID      int      `json:"gid"`
	Original []string `json:"original"`
}

func linuxDNSStatePath() string { return filepath.Join(linuxInstallDir, "dns-state.json") }

func dnsNameservers(contents []byte) []string {
	var values []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		ip := net.ParseIP(fields[1])
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		values = append(values, ip.String())
	}
	return values
}

func snapshotLinuxDNS(path string) (linuxDNSState, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return linuxDNSState{}, err
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return linuxDNSState{}, errors.New("/etc/resolv.conf должен быть файлом или символьной ссылкой")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return linuxDNSState{}, err
	}
	state := linuxDNSState{Contents: contents, Mode: uint32(info.Mode().Perm())}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		state.UID, state.GID = int(stat.Uid), int(stat.Gid)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		state.Link, err = os.Readlink(path)
		if err != nil {
			return linuxDNSState{}, err
		}
	}
	state.Original = dnsNameservers(contents)
	if len(state.Original) == 0 {
		// systemd-resolved commonly uses 127.0.0.53 in resolv.conf.
		// Ask it for the real per-link servers before changing the file.
		output, err := exec.Command("resolvectl", "dns").Output()
		if err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				_, servers, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				for _, value := range strings.Fields(servers) {
					state.Original = append(state.Original, dnsNameservers([]byte("nameserver "+strings.SplitN(value, "#", 2)[0]))...)
				}
			}
		}
	}
	if len(state.Original) == 0 {
		return linuxDNSState{}, errors.New("не удалось определить исходные DNS-серверы; системный DNS не изменён")
	}
	return state, nil
}

func writeLinuxDNSState(state linuxDNSState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	path := linuxDNSStatePath()
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func readLinuxDNSState() (linuxDNSState, error) {
	data, err := os.ReadFile(linuxDNSStatePath())
	if err != nil {
		return linuxDNSState{}, err
	}
	var state linuxDNSState
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if len(state.Original) == 0 || len(state.Contents) > 1<<20 || strings.ContainsRune(state.Link, 0) {
		return state, errors.New("резервная копия DNS повреждена")
	}
	return state, nil
}

func replaceLinuxResolv(path string, contents []byte, link string, mode os.FileMode, uid, gid int) error {
	if info, err := os.Lstat(path); err == nil && info.IsDir() {
		return errors.New("/etc/resolv.conf стал каталогом")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".privetzapret-resolv-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(name)
	if link != "" {
		if err := os.Remove(name); err != nil {
			return err
		}
		if err := os.Symlink(link, name); err != nil {
			return err
		}
		if err := os.Lchown(name, uid, gid); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(name, contents, mode); err != nil {
			return err
		}
		if err := os.Chown(name, uid, gid); err != nil {
			return err
		}
		if err := os.Chmod(name, mode); err != nil {
			return err
		}
	}
	return os.Rename(name, path)
}

func linuxDNSApplied() bool {
	data, err := os.ReadFile(linuxResolvPath)
	return err == nil && string(data) == linuxDNSMarker
}

func applyLinuxDNS(state linuxDNSState) error {
	if _, err := os.Stat(linuxDNSStatePath()); err == nil && !linuxDNSApplied() {
		return errors.New("DNS изменён после сохранения резервной копии; /etc/resolv.conf не перезаписан")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writeLinuxDNSState(state); err != nil {
		return err
	}
	if linuxDNSApplied() {
		return nil
	}
	if err := replaceLinuxResolv(linuxResolvPath, []byte(linuxDNSMarker), "", 0644, 0, 0); err != nil {
		_ = os.Remove(linuxDNSStatePath())
		return err
	}
	if !linuxDNSApplied() {
		return errors.New("системный DNS не применился")
	}
	return nil
}

func restoreLinuxDNS() error {
	state, err := readLinuxDNSState()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !linuxDNSApplied() {
		return errors.New("/etc/resolv.conf изменён другой программой; резервная копия сохранена")
	}
	if err := replaceLinuxResolv(linuxResolvPath, state.Contents, state.Link, os.FileMode(state.Mode), state.UID, state.GID); err != nil {
		return fmt.Errorf("восстановление DNS: %w", err)
	}
	return os.Remove(linuxDNSStatePath())
}
