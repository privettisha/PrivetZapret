package main

import (
	"net"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

type EndpointCheck struct {
	Reachable bool   `json:"reachable"`
	HTTPCode  int    `json:"http_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

// A direct Googlevideo CDN host catches TCP connection failures hidden by the
// always-reachable redirector/report_mapping endpoint. It is a sample host,
// not a test of the exact CDN assigned to every video/account.
const youtubeMediaProbeURL = "https://rr2---sn-ug5onuxaxjvh-v8cz.googlevideo.com/"

// This sample is selected when YouTube itself is routed through WARP. The
// service can assign a different CDN to every network and video.
const youtubeWarpMediaProbeURL = "https://rr3---sn-ojnpo5-ci.googlevideo.com/"
const youtubeMappingProbeURL = "https://redirector.googlevideo.com/report_mapping"
const soundcloudMediaProbeURL = "https://cf-media.sndcdn.com/"

func connectivityCheck() map[string]EndpointCheck {
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, TLSHandshakeTimeout: 3 * time.Second},
	}
	targets := map[string]string{
		"youtube":                 "https://www.youtube.com/generate_204",
		"preview":                 "https://i.ytimg.com/vi/jfKfPfyJRdk/default.jpg",
		"video_server":            youtubeMediaProbeURL,
		"video_mapping":           youtubeMappingProbeURL,
		"soundcloud_media_server": soundcloudMediaProbeURL,
	}
	checks := make(map[string]EndpointCheck, len(targets))
	results := make(chan struct {
		name  string
		check EndpointCheck
	}, len(targets))
	for name, address := range targets {
		go func(name, address string) {
			results <- struct {
				name  string
				check EndpointCheck
			}{name, checkEndpoint(client, address)}
		}(name, address)
	}
	for range targets {
		result := <-results
		checks[result.name] = result.check
	}
	return checks
}

func checkEndpoint(client *http.Client, address string) EndpointCheck {
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return EndpointCheck{Error: "Некорректный адрес"}
	}
	// A timeout before TCP connects calls for a different fix than a timeout
	// during TLS or while waiting for an HTTP response. Keep the report useful
	// without exposing a user's address, DNS answer or request details.
	var stage atomic.Value
	stage.Store("соединение")
	trace := &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { stage.Store("DNS") },
		ConnectStart:      func(_, _ string) { stage.Store("TCP") },
		TLSHandshakeStart: func() { stage.Store("TLS") },
		WroteRequest:      func(httptrace.WroteRequestInfo) { stage.Store("HTTP") },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	req.Header.Set("User-Agent", "PrivetZapret/"+appVersion)
	resp, err := client.Do(req)
	if err != nil {
		phase := stage.Load().(string)
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return EndpointCheck{Error: phase + ": тайм-аут"}
		}
		return EndpointCheck{Error: phase + ": ошибка соединения"}
	}
	_ = resp.Body.Close()
	return EndpointCheck{Reachable: true, HTTPCode: resp.StatusCode}
}
