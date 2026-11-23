package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func main() {
	if platformHandleSpecialMode() {
		return
	}
	if !platformIsAdministrator() {
		platformRelaunchElevated()
		return
	}
	runDesktop()
}

func runDesktop() {
	a := app.NewWithID("ru.privetdns.PrivetZapret")
	w := a.NewWindow("PrivetZapret")
	w.Resize(fyne.NewSize(780, 650))
	status := platformStatus()
	var leaseMu sync.Mutex
	var stopLease func()
	ensureLease := func() error {
		leaseMu.Lock()
		defer leaseMu.Unlock()
		if stopLease != nil {
			return nil
		}
		stop, err := startControlLease()
		if err == nil {
			stopLease = stop
		}
		return err
	}
	clearLease := func() {
		leaseMu.Lock()
		defer leaseMu.Unlock()
		if stopLease != nil {
			stopLease()
			stopLease = nil
		}
	}
	if status.Installed || status.Active {
		if err := ensureLease(); err != nil {
			dialog.ShowError(err, w)
		}
	}

	state := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	detail := widget.NewLabel("")
	detail.Wrapping = fyne.TextWrapWord
	primary := widget.NewButton("", nil)
	primary.Importance = widget.HighImportance
	remove := widget.NewButton("Удалить программу и остановить процессы", nil)
	refresh := widget.NewButton("Обновить состояние", nil)
	mode := widget.NewSelect([]string{"Совместимый", "Сбалансированный", "Сильный"}, nil)
	modeIDs := map[string]string{"Совместимый": "compatible", "Сбалансированный": "balanced", "Сильный": "strong"}
	modeNames := map[string]string{"compatible": "Совместимый", "balanced": "Сбалансированный", "strong": "Сильный"}
	profile := widget.NewSelect([]string{"Стандарт", "Разделение", "Альтернативный", "Без QUIC"}, nil)
	profileIDs := map[string]string{"Стандарт": "standard", "Разделение": "split", "Альтернативный": "alternative", "Без QUIC": "tcp"}
	profileNames := map[string]string{"standard": "Стандарт", "split": "Разделение", "alternative": "Альтернативный", "tcp": "Без QUIC"}
	applyMode := widget.NewButton("Применить режим", nil)
	applyProfile := widget.NewButton("Применить профиль YouTube", nil)
	autoYouTube := widget.NewButton("Подобрать профиль YouTube", nil)
	serviceChecks := make(map[string]*widget.Check)
	serviceRows := make([]fyne.CanvasObject, 0, len(serviceCatalog))
	for _, service := range serviceCatalog {
		service := service
		check := widget.NewCheck(service.Name, nil)
		serviceChecks[service.ID] = check
		serviceRows = append(serviceRows, check)
	}
	applyServices := widget.NewButton("Применить выбранные сервисы", nil)
	applyServices.Importance = widget.HighImportance
	customDomains := widget.NewMultiLineEntry()
	customDomains.SetPlaceHolder("example.com — один домен в строке")
	customDomains.SetMinRowsVisible(3)
	excludeDomains := widget.NewMultiLineEntry()
	excludeDomains.SetPlaceHolder("example.org — один домен в строке")
	excludeDomains.SetMinRowsVisible(3)
	autoHostlist := widget.NewCheck("Автоматически находить заблокированные домены", nil)
	gameMode := widget.NewCheck("Игровые UDP правила", nil)
	autoUpdate := widget.NewCheck("Автоматические обновления", nil)
	discordProfile := widget.NewSelect([]string{"Стандарт", "Разделение", "Альтернативный", "Без QUIC"}, nil)
	genericProfile := widget.NewSelect([]string{"Стандарт", "Разделение", "Альтернативный", "Без QUIC"}, nil)
	saveSettings := widget.NewButton("Сохранить настройки", nil)
	autoDiscord := widget.NewButton("Подобрать профиль Discord", nil)
	dnsInput := widget.NewEntry()
	dnsInput.SetPlaceHolder("https://dns.example/dns-query или tls://dns.example")
	dnsEnable := widget.NewCheck("Использовать как системный DNS", nil)
	dnsState := widget.NewLabel("")
	dnsState.Wrapping = fyne.TextWrapWord
	applyDNS := widget.NewButton("Применить DNS", nil)
	telegramState := widget.NewLabel("")
	telegramState.Wrapping = fyne.TextWrapWord
	telegramRoute := widget.NewSelect([]string{"Медиа (DC4 + резерв)", "Прямой (DC2 + DC4)", "Только резерв"}, nil)
	telegramRouteIDs := map[string]string{"Медиа (DC4 + резерв)": "media", "Прямой (DC2 + DC4)": "direct", "Только резерв": "cloudflare"}
	telegramRouteNames := map[string]string{"media": "Медиа (DC4 + резерв)", "direct": "Прямой (DC2 + DC4)", "cloudflare": "Только резерв"}
	applyTelegramRoute := widget.NewButton("Применить маршрут Telegram", nil)
	telegramOpen := widget.NewButton("Подключить в Telegram", nil)
	telegramCopy := widget.NewButton("Скопировать параметры", nil)
	report := widget.NewMultiLineEntry()
	report.SetMinRowsVisible(16)
	report.Disable()
	checkConnection := widget.NewButton("Проверить соединение", nil)
	repair := widget.NewButton("Восстановить службу", nil)
	checkUpdates := widget.NewButton("Проверить обновления", nil)
	installUpdate := widget.NewButton("Установить обновление", nil)
	rollback := widget.NewButton("Откатить обновление", nil)
	installUpdate.Hide()

	busy := false
	updateView := func(s Status, loadFields bool) {
		status = s
		if s.Active {
			state.SetText("Включено")
			primary.SetText("Выключить")
			primary.Importance = widget.DangerImportance
		} else if s.Installed {
			state.SetText("Выключено")
			primary.SetText("Включить")
			primary.Importance = widget.HighImportance
		} else {
			state.SetText("Не установлено")
			primary.SetText("Установить и включить")
			primary.Importance = widget.HighImportance
		}
		primary.Refresh()
		detail.SetText(fmt.Sprintf("%s · версия %s · процессов движка: %d · выбрано сервисов: %d. Закрытие окна выключает службу.", s.Platform, appVersion, s.EngineProcesses, len(s.Services)))
		if s.TelegramReady {
			telegramState.SetText("Локальный прокси Telegram работает на 127.0.0.1. Для загрузки медиа Telegram должен быть подключён к нему.")
		} else if s.Active && hasService(s.Services, "telegram") {
			telegramState.SetText("Прокси Telegram не отвечает. Откройте проверку и восстановите службу.")
		} else {
			telegramState.SetText("Прокси Telegram выключен.")
		}
		if s.DNSActive {
			dnsState.SetText("Системный DNS подключён")
		} else if s.DNSEnabled {
			dnsState.SetText("DNS сохранён, но сейчас не применяется")
		} else {
			dnsState.SetText("Системный DNS не изменён")
		}
		if loadFields {
			mode.SetSelected(modeNames[s.Mode])
			profile.SetSelected(profileNames[s.YoutubeProfile])
			telegramRoute.SetSelected(telegramRouteNames[s.TelegramRoute])
			for id, check := range serviceChecks {
				check.SetChecked(hasService(s.Services, id))
			}
			dnsInput.SetText(s.DNSURL)
			dnsEnable.SetChecked(s.DNSEnabled)
			customDomains.SetText(strings.Join(s.CustomDomains, "\n"))
			excludeDomains.SetText(strings.Join(s.ExcludeDomains, "\n"))
			autoHostlist.SetChecked(s.AutoHostlist)
			gameMode.SetChecked(s.GameMode)
			autoUpdate.SetChecked(s.AutoUpdate)
			discordProfile.SetSelected(profileNames[s.Profiles["discord"]])
			genericProfile.SetSelected(profileNames[s.Profiles["generic"]])
			if discordProfile.Selected == "" {
				discordProfile.SetSelected("Стандарт")
			}
			if genericProfile.Selected == "" {
				genericProfile.SetSelected("Стандарт")
			}
		}
		if busy {
			primary.Disable()
		} else {
			primary.Enable()
		}
		if s.Installed && !busy {
			remove.Enable()
			applyMode.Enable()
			applyProfile.Enable()
			applyServices.Enable()
			applyDNS.Enable()
			applyTelegramRoute.Enable()
			repair.Enable()
			saveSettings.Enable()
			checkUpdates.Enable()
		} else {
			remove.Disable()
			applyMode.Disable()
			applyProfile.Disable()
			applyServices.Disable()
			applyDNS.Disable()
			applyTelegramRoute.Disable()
			repair.Disable()
			saveSettings.Disable()
			checkUpdates.Disable()
		}
		if s.TelegramReady && !busy {
			telegramOpen.Enable()
			telegramCopy.Enable()
		} else {
			telegramOpen.Disable()
			telegramCopy.Disable()
		}
		if s.Active && !busy && hasService(s.Services, "youtube") {
			autoYouTube.Enable()
		} else {
			autoYouTube.Disable()
		}
		if s.Active && !busy && hasService(s.Services, "discord") {
			autoDiscord.Enable()
		} else {
			autoDiscord.Disable()
		}
		if s.RollbackAvailable && !busy {
			rollback.Show()
			rollback.Enable()
		} else {
			rollback.Disable()
			if !s.RollbackAvailable {
				rollback.Hide()
			}
		}
		if !hasService(s.Services, "youtube") {
			applyProfile.Disable()
		}
		if !hasService(s.Services, "telegram") {
			applyTelegramRoute.Disable()
		}
		if s.DNSSupported {
			dnsInput.Enable()
			dnsEnable.Enable()
		} else {
			dnsInput.Disable()
			dnsEnable.Disable()
			applyDNS.Disable()
			dnsState.SetText("Системный DNS недоступен в этой конфигурации")
		}
	}
	act := func(work func() error) {
		if busy {
			return
		}
		busy = true
		updateView(status, false)
		go func() {
			err := work()
			latest := platformStatus()
			fyne.Do(func() {
				busy = false
				updateView(latest, err == nil)
				if err != nil {
					dialog.ShowError(err, w)
				}
			})
		}()
	}
	primary.OnTapped = func() {
		active, installed, selectedMode := status.Active, status.Installed, modeIDs[mode.Selected]
		act(func() error {
			if active {
				return platformStop()
			}
			if err := ensureLease(); err != nil {
				return err
			}
			if !installed {
				return platformInstall(selectedMode)
			}
			return platformStart()
		})
	}
	remove.OnTapped = func() {
		dialog.ShowConfirm("Удаление PrivetZapret", "Остановить службу, вернуть DNS и удалить файлы программы?", func(confirmed bool) {
			if confirmed {
				if busy {
					return
				}
				busy = true
				updateView(status, false)
				go func() {
					clearLease()
					err := platformRemove()
					fyne.Do(func() {
						if err == nil {
							a.Quit()
							return
						}
						busy = false
						updateView(platformStatus(), false)
						dialog.ShowError(err, w)
					})
				}()
			}
		}, w)
	}
	refresh.OnTapped = func() { updateView(platformStatus(), false) }
	applyMode.OnTapped = func() { selected := modeIDs[mode.Selected]; act(func() error { return platformSetMode(selected) }) }
	applyProfile.OnTapped = func() {
		selected := profileIDs[profile.Selected]
		act(func() error { return platformSetYoutubeProfile(selected) })
	}
	autoYouTube.OnTapped = func() { act(func() error { _, err := autoTune("youtube"); return err }) }
	applyServices.OnTapped = func() {
		var selected []string
		for _, service := range serviceCatalog {
			if serviceChecks[service.ID].Checked {
				selected = append(selected, service.ID)
			}
		}
		if len(selected) == 0 {
			dialog.ShowInformation("Сервисы", "Выберите хотя бы один сервис", w)
			return
		}
		act(func() error { return platformSetServices(selected) })
	}
	saveSettings.OnTapped = func() {
		custom, excluded := strings.Fields(customDomains.Text), strings.Fields(excludeDomains.Text)
		if len(custom) > 100 || len(excluded) > 100 || len(normalizeDomains(custom)) != len(custom) || len(normalizeDomains(excluded)) != len(excluded) {
			dialog.ShowInformation("Домены", "Укажите до 100 разных доменов без https:// и путей", w)
			return
		}
		cfg := loadConfig(platformConfigPath())
		cfg.CustomDomains, cfg.ExcludeDomains = custom, excluded
		cfg.AutoHostlist, cfg.GameMode, cfg.AutoUpdate = autoHostlist.Checked, gameMode.Checked, autoUpdate.Checked
		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]string)
		}
		cfg.Profiles["discord"] = profileIDs[discordProfile.Selected]
		cfg.Profiles["generic"] = profileIDs[genericProfile.Selected]
		act(func() error { return platformApplyConfig(cfg) })
	}
	autoDiscord.OnTapped = func() { act(func() error { _, err := autoTune("discord"); return err }) }
	applyDNS.OnTapped = func() {
		endpoint, enabled := strings.TrimSpace(dnsInput.Text), dnsEnable.Checked
		act(func() error { return platformSetDNS(endpoint, enabled) })
	}
	applyTelegramRoute.OnTapped = func() {
		selected := telegramRouteIDs[telegramRoute.Selected]
		act(func() error { return platformSetTelegramRoute(selected) })
	}
	telegramOpen.OnTapped = func() {
		cfg := loadConfig(platformConfigPath())
		platformOpenBrowser(telegramProxyURL(cfg.TelegramSecret))
	}
	telegramCopy.OnTapped = func() {
		cfg := loadConfig(platformConfigPath())
		w.Clipboard().SetContent(telegramProxyURL(cfg.TelegramSecret))
		dialog.ShowInformation("Telegram", "Ссылка подключения скопирована", w)
	}
	checkConnection.OnTapped = func() {
		act(func() error {
			data, err := json.MarshalIndent(systemDiagnostics(), "", "  ")
			if err == nil {
				fyne.Do(func() { report.SetText(string(data)) })
			}
			return err
		})
	}
	repair.OnTapped = func() { act(platformRepair) }
	checkUpdates.OnTapped = func() {
		act(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			info, err := checkUpdate(ctx)
			fyne.Do(func() {
				if err == nil && info.Available {
					installUpdate.Show()
					dialog.ShowInformation("Обновление", "Доступна версия "+info.Version, w)
				} else if err == nil {
					installUpdate.Hide()
					dialog.ShowInformation("Обновление", "Установлена актуальная версия", w)
				}
			})
			return err
		})
	}
	installUpdate.OnTapped = func() {
		act(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			path, err := stageUpdate(ctx)
			if err != nil {
				return err
			}
			return launchInstaller(path)
		})
	}
	rollback.OnTapped = func() { act(rollbackUpdate) }

	connectionTab := container.NewVScroll(container.NewVBox(
		widget.NewCard("Состояние", "Служба и процессы", container.NewVBox(state, detail, primary, refresh)),
		widget.NewCard("Режим соединения", "Режим влияет на число попыток и разделение пакетов", container.NewVBox(mode, applyMode)),
		widget.NewCard("YouTube", "Если страница открывается, а видео нет, попробуйте «Без QUIC» и проверьте видеосервер", container.NewVBox(profile, applyProfile, autoYouTube)),
		widget.NewCard("Telegram", "Подключите локальный прокси в Telegram Desktop", container.NewVBox(telegramState, telegramOpen, telegramCopy, telegramRoute, applyTelegramRoute)),
		remove,
	))
	servicesTab := container.NewBorder(nil, applyServices, nil, nil, container.NewVScroll(container.NewVBox(serviceRows...)))
	settingsTab := container.NewVScroll(container.NewVBox(
		widget.NewCard("Свои домены", "Укажите адреса, которые нужно обрабатывать", customDomains),
		widget.NewCard("Исключения", "Не обрабатывать эти адреса", excludeDomains),
		widget.NewCard("Профили", "Discord и остальные сервисы", container.NewVBox(widget.NewLabel("Discord"), discordProfile, autoDiscord, widget.NewLabel("Остальные сервисы"), genericProfile)),
		autoHostlist, gameMode, autoUpdate, saveSettings,
	))
	dnsTab := container.NewVScroll(container.NewVBox(widget.NewCard("Свой DNS", "DoH или DoT", container.NewVBox(
		widget.NewLabel("Вставьте HTTPS ссылку DoH или tls://имя-сервера для DoT. При сбое запросы пойдут через прежние DNS без шифрования."), dnsInput, dnsEnable, dnsState, applyDNS,
	))))
	checkTab := container.NewBorder(container.NewVBox(checkConnection, repair, checkUpdates, installUpdate, rollback), nil, nil, nil, report)
	tabs := container.NewAppTabs(
		container.NewTabItem("Управление", connectionTab),
		container.NewTabItem("Сервисы", servicesTab),
		container.NewTabItem("Настройки", settingsTab),
		container.NewTabItem("DNS", dnsTab),
		container.NewTabItem("Проверка", checkTab),
	)
	w.SetContent(tabs)
	updateView(status, true)
	refreshDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-refreshDone:
				return
			case <-ticker.C:
				latest := platformStatus()
				fyne.Do(func() { updateView(latest, false) })
			}
		}
	}()
	closing := false
	w.SetCloseIntercept(func() {
		if closing {
			return
		}
		closing = true
		needsStop := status.Installed || status.Active
		state.SetText("Выключение…")
		go func() {
			var err error
			if needsStop {
				err = platformStop()
			}
			fyne.Do(func() {
				if err != nil {
					closing = false
					updateView(platformStatus(), false)
					dialog.ShowError(err, w)
					return
				}
				clearLease()
				a.Quit()
			})
		}()
	})
	w.ShowAndRun()
	close(refreshDone)
	clearLease()
}
