package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gogpu/systray"
)

func trayIcon() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	blue, white := color.RGBA{49, 87, 213, 255}, color.RGBA{255, 255, 255, 255}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			dx, dy := x-16, y-16
			if dx*dx+dy*dy <= 14*14 {
				img.Set(x, y, blue)
			}
		}
	}
	for _, bar := range []struct{ x, y0, y1 int }{{10, 12, 20}, {15, 8, 24}, {20, 11, 21}} {
		for x := bar.x; x < bar.x+3; x++ {
			for y := bar.y0; y < bar.y1; y++ {
				img.Set(x, y, white)
			}
		}
	}
	var data bytes.Buffer
	_ = png.Encode(&data, img)
	return data.Bytes()
}

func runTray(url string, server *http.Server) error {
	if runtime.GOOS == "linux" {
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return nil
		}
		if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			uid := os.Getenv("SUDO_UID")
			if uid != "" {
				bus := filepath.Join("/run/user", uid, "bus")
				if _, err := os.Stat(bus); err == nil {
					_ = os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+bus)
				}
			}
		}
		if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
			return nil
		}
	}
	tray := systray.New()
	menu := systray.NewMenu()
	menu.Add("Открыть PrivetZapret", func() { platformOpenBrowser(url) })
	stateItem := menu.Add("Запустить", func() {
		if platformStatus().Active {
			_ = platformStop()
		} else {
			_ = platformStart()
		}
	})
	menu.AddSeparator()
	serviceItems := make(map[string]*systray.MenuItem)
	for _, service := range []struct{ id, name string }{{"youtube", "YouTube"}, {"discord", "Discord"}, {"telegram", "Telegram"}} {
		id := service.id
		serviceItems[id] = menu.AddCheckbox(service.name, hasService(platformStatus().Services, id), func() {
			cfg := loadConfig(platformConfigPath())
			var next []string
			for _, selected := range cfg.Services {
				if selected != id {
					next = append(next, selected)
				}
			}
			if len(next) == len(cfg.Services) {
				next = append(next, id)
			}
			if len(next) == 0 {
				return
			}
			_ = platformSetServices(next)
		})
	}
	menu.AddSeparator()
	menu.Add("Выход", func() {
		tray.Remove()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	tray.SetIcon(trayIcon()).SetTooltip("PrivetZapret").SetMenu(menu)
	tray.OnClick(func() { platformOpenBrowser(url) })
	tray.Show()
	stopRefresh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopRefresh:
				return
			case <-ticker.C:
				status := platformStatus()
				if status.Active {
					stateItem.SetLabel("Остановить")
				} else {
					stateItem.SetLabel("Запустить")
				}
				for id, item := range serviceItems {
					item.SetChecked(hasService(status.Services, id))
				}
			}
		}
	}()
	err := tray.Run()
	close(stopRefresh)
	return err
}
