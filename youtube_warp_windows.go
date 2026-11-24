//go:build windows

package main

import "errors"

func youtubeWarpSupported() bool    { return false }
func youtubeWarpOwned() bool        { return false }
func youtubeWarpConnected() bool    { return false }
func warpMediaProbe() EndpointCheck { return EndpointCheck{Error: "недоступно на Windows"} }
func startYouTubeWarp() error {
	return errors.New("выборочный WARP-маршрут пока доступен в версии для Arch Linux")
}
func stopYouTubeWarp() error { return nil }
