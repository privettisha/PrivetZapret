package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ServiceDefinition struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Domains     []string `json:"-"`
	Default     bool     `json:"default"`
}

// The catalogue intentionally contains services whose HTTPS connections can
// be handled locally. Services that require a foreign account or a different
// country IP are not included: selecting a switch must have a useful effect.
var serviceCatalog = []ServiceDefinition{
	{ID: "youtube", Name: "YouTube", Category: "Основные", Description: "Видео, превью и трансляции", Default: true, Domains: []string{
		"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtubei.googleapis.com",
		"youtube.googleapis.com", "youtube-nocookie.com", "ggpht.com", "withyoutube.com",
	}},
	{ID: "discord", Name: "Discord", Category: "Основные", Description: "Чаты, медиа, звонки и голос", Default: true, Domains: []string{
		"discord.com", "discord.gg", "discord.co", "discord.gift", "discordapp.com",
		"discordapp.net", "discord.media", "discordcdn.com",
	}},
	{ID: "telegram", Name: "Telegram Desktop", Category: "Основные", Description: "Встроенное локальное подключение", Default: true, Domains: []string{
		"telegram.org", "t.me", "telegram.me", "telegra.ph", "web.telegram.org",
	}},
	{ID: "whatsapp", Name: "WhatsApp", Category: "Мессенджеры", Description: "Сайт и медиа", Domains: []string{
		"whatsapp.com", "whatsapp.net", "wa.me", "whatsapp-cdn.net",
	}},
	{ID: "signal", Name: "Signal", Category: "Мессенджеры", Description: "Сайт и вложения", Domains: []string{
		"signal.org", "signal.art", "signal.group", "signal.link", "signal.me", "signal.tube",
	}},
	{ID: "steam", Name: "Steam Community", Category: "Игры", Description: "Сообщество и мастерская", Domains: []string{
		"steamcommunity.com", "steamusercontent.com", "steamstatic.com", "steampowered.com", "steamcontent.com",
	}},
	{ID: "roblox", Name: "Roblox", Category: "Игры", Description: "Сайт и запуск", Domains: []string{
		"roblox.com", "rbxcdn.com",
	}},
	{ID: "cod", Name: "Call of Duty / Warzone", Category: "Игры", Description: "Сайт и вход", Domains: []string{
		"callofduty.com", "activision.com", "demonware.net",
	}},
	{ID: "fortnite", Name: "Fortnite", Category: "Игры", Description: "Сайт и вход", Domains: []string{
		"fortnite.com", "epicgames.com", "epicgames.dev", "unrealengine.com",
	}},
	{ID: "ea", Name: "EA", Category: "Игры", Description: "Сайт и вход", Domains: []string{
		"ea.com", "electronicarts.com", "origin.com",
	}},
	{ID: "ubisoft", Name: "Ubisoft Connect", Category: "Игры", Description: "Сайт и вход", Domains: []string{
		"ubisoft.com", "ubi.com", "ubiservices.com",
	}},
	{ID: "github", Name: "GitHub", Category: "Инструменты", Description: "Сайт, код и релизы", Domains: []string{
		"github.com", "githubusercontent.com", "githubassets.com", "github.io",
	}},
	{ID: "spotify", Name: "Spotify", Category: "Медиа и сообщества", Description: "Сайт и медиа", Domains: []string{
		"spotify.com", "scdn.co", "spotifycdn.com", "spotifycdn.net",
	}},
	{ID: "instagram", Name: "Instagram", Category: "Социальные сети", Description: "Лента, Reels и медиа", Domains: []string{
		"instagram.com", "cdninstagram.com", "ig.me", "fbcdn.net",
	}},
	{ID: "facebook", Name: "Facebook", Category: "Социальные сети", Description: "Сайт и медиаконтент", Domains: []string{
		"facebook.com", "facebook.net", "fb.com", "fbcdn.net", "fbsbx.com",
	}},
	{ID: "threads", Name: "Threads", Category: "Социальные сети", Description: "Сайт и вложения", Domains: []string{
		"threads.net", "threads.com", "cdninstagram.com", "fbcdn.net",
	}},
	{ID: "x", Name: "X / Twitter", Category: "Социальные сети", Description: "Лента, изображения и видео", Domains: []string{
		"x.com", "twitter.com", "t.co", "twimg.com", "twittercdn.com", "pscp.tv",
	}},
	{ID: "linkedin", Name: "LinkedIn", Category: "Социальные сети", Description: "Сайт и публикации", Domains: []string{
		"linkedin.com", "licdn.com", "lnkd.in",
	}},
	{ID: "reddit", Name: "Reddit", Category: "Социальные сети", Description: "Лента, изображения и видео", Domains: []string{
		"reddit.com", "redd.it", "redditmedia.com", "redditstatic.com", "redditspace.com",
	}},
	{ID: "pinterest", Name: "Pinterest", Category: "Социальные сети", Description: "Сайт, изображения и видео", Domains: []string{
		"pinterest.com", "pinimg.com", "pinterest.ru",
	}},
	{ID: "twitch", Name: "Twitch", Category: "Видео и эфиры", Description: "Трансляции, клипы и чат", Domains: []string{
		"twitch.tv", "ttvnw.net", "jtvnw.net", "twitchcdn.net", "ext-twitch.tv",
	}},
	{ID: "tiktok", Name: "TikTok", Category: "Видео и эфиры", Description: "Сайт и видеопотоки", Domains: []string{
		"tiktok.com", "tiktokv.com", "tiktokcdn.com", "tiktokcdn-eu.com", "byteoversea.com",
		"ibytedtos.com", "ibyteimg.com", "muscdn.com", "musical.ly",
	}},
	{ID: "vimeo", Name: "Vimeo", Category: "Видео и эфиры", Description: "Видео и встроенный плеер", Domains: []string{
		"vimeo.com", "vimeocdn.com",
	}},
	{ID: "dailymotion", Name: "Dailymotion", Category: "Видео и эфиры", Description: "Видео и встроенный плеер", Domains: []string{
		"dailymotion.com", "dai.ly", "dmcdn.net",
	}},
	{ID: "kick", Name: "Kick", Category: "Видео и эфиры", Description: "Трансляции и чат", Domains: []string{
		"kick.com", "kickcdn.com", "kickassets.com",
	}},
	{ID: "soundcloud", Name: "SoundCloud", Category: "Медиа и сообщества", Description: "Музыка и обложки", Domains: []string{
		"soundcloud.com", "sndcdn.com",
	}},
	{ID: "patreon", Name: "Patreon", Category: "Медиа и сообщества", Description: "Публикации и вложения", Domains: []string{
		"patreon.com", "patreonusercontent.com",
	}},
	{ID: "medium", Name: "Medium", Category: "Медиа и сообщества", Description: "Статьи и изображения", Domains: []string{
		"medium.com", "miro.medium.com",
	}},
}

func serviceByID(id string) (ServiceDefinition, bool) {
	for _, service := range serviceCatalog {
		if service.ID == id {
			return service, true
		}
	}
	return ServiceDefinition{}, false
}

func defaultServices() []string {
	var selected []string
	for _, service := range serviceCatalog {
		if service.Default {
			selected = append(selected, service.ID)
		}
	}
	return selected
}

func normalizeServices(services []string) []string {
	seen := make(map[string]bool)
	for _, id := range services {
		id = strings.ToLower(strings.TrimSpace(id))
		if _, ok := serviceByID(id); ok {
			seen[id] = true
		}
	}
	selected := make([]string, 0, len(seen))
	for _, service := range serviceCatalog {
		if seen[service.ID] {
			selected = append(selected, service.ID)
		}
	}
	return selected
}

func hasService(services []string, id string) bool {
	for _, selected := range services {
		if selected == id {
			return true
		}
	}
	return false
}

func writeSelectedHostlists(dir string, services []string) error {
	return writeSelectedHostlistsConfig(dir, Config{Services: services})
}

func writeSelectedHostlistsConfig(dir string, cfg Config) error {
	selected := normalizeServices(cfg.Services)
	groups := map[string][]string{"youtube": {}, "discord": {}, "telegram": {}, "generic": {}}
	for _, id := range selected {
		service, _ := serviceByID(id)
		groups[id] = append(groups[id], service.Domains...)
		if id != "youtube" && id != "discord" && id != "telegram" {
			groups["generic"] = append(groups["generic"], service.Domains...)
		}
	}
	for group, domains := range groups {
		unique := make(map[string]bool)
		for _, domain := range domains {
			unique[strings.ToLower(strings.TrimSpace(domain))] = true
		}
		domains = domains[:0]
		for domain := range unique {
			if domain != "" {
				domains = append(domains, domain)
			}
		}
		sort.Strings(domains)
		content := []byte(strings.Join(domains, "\n"))
		if len(content) != 0 {
			content = append(content, '\n')
		}
		if err := writeFileIfChanged(filepath.Join(dir, "selected-"+group+".txt"), content, 0644); err != nil {
			return fmt.Errorf("список %s: %w", group, err)
		}
	}
	for name, domains := range map[string][]string{"custom": cfg.CustomDomains, "exclude": cfg.ExcludeDomains} {
		content := []byte(strings.Join(normalizeDomains(domains), "\n"))
		if len(content) > 0 {
			content = append(content, '\n')
		}
		if err := writeFileIfChanged(filepath.Join(dir, "selected-"+name+".txt"), content, 0644); err != nil {
			return err
		}
	}
	if cfg.AutoHostlist {
		path := filepath.Join(dir, "selected-auto.txt")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, nil, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func selectedServiceNames(services []string) string {
	var names []string
	for _, id := range normalizeServices(services) {
		service, _ := serviceByID(id)
		names = append(names, service.Name)
	}
	return strings.Join(names, ", ")
}

func ensureServiceDirectory(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0755)
}
