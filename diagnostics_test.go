package main

import (
	"net/http"
	"net/http/httptest"
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
