package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckEndpointReportsHTTPResponseAndFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	client := &http.Client{Timeout: time.Second}
	result := checkEndpoint(client, server.URL)
	if !result.Reachable || result.HTTPCode != http.StatusForbidden {
		t.Fatalf("HTTP response should be reported, got %#v", result)
	}
	server.Close()
	result = checkEndpoint(client, server.URL)
	if result.Reachable || result.Error == "" {
		t.Fatalf("connection failure should be reported, got %#v", result)
	}
}

func TestCheckEndpointIdentifiesTLSStall(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			// Accept TCP but never complete the TLS handshake.
			time.Sleep(time.Second)
		}
	}()
	result := checkEndpoint(&http.Client{Timeout: 100 * time.Millisecond}, "https://"+listener.Addr().String())
	if result.Reachable || !strings.HasPrefix(result.Error, "TLS: тайм-аут") {
		t.Fatalf("TLS timeout should identify the failed phase, got %#v", result)
	}
}
