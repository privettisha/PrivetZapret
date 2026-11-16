package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var testDNSQuery = []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}

func testDNSAnswer() []byte {
	answer := append([]byte(nil), testDNSQuery...)
	answer[2] = 0x81
	answer[3] = 0x80
	answer[6] = 0
	answer[7] = 1
	answer = append(answer, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 30, 0, 4, 203, 0, 113, 7)
	return answer
}

func TestDoHValidation(t *testing.T) {
	for _, value := range []string{"http://dns.example.com/dns-query", "https://localhost/dns-query", "https://user@dns.example.com/dns-query", "https://dns.example.com/dns-query#fragment", " https://dns.example.com/dns-query"} {
		if _, err := validateDoHURL(value); err == nil {
			t.Errorf("accepted invalid DoH URL %q", value)
		}
	}
	if _, err := validateDoHURL("https://dns.example.com/dns-query/device/private-token?key=secret"); err != nil {
		t.Fatal(err)
	}
}

func TestDoHAndFallback(t *testing.T) {
	answer := testDNSAnswer()
	var available atomic.Bool
	available.Store(true)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != dnsMessageType {
			t.Errorf("unexpected DoH request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", dnsMessageType)
		_, _ = w.Write(answer)
	}))
	defer server.Close()

	fallback, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	go func() {
		buffer := make([]byte, 4096)
		for {
			_, addr, err := fallback.ReadFrom(buffer)
			if err != nil {
				return
			}
			_, _ = fallback.WriteTo(answer, addr)
		}
	}()
	proxy := &dnsProxy{url: server.URL, client: server.Client(), fallback: []string{fallback.LocalAddr().String()}}
	if got := proxy.resolve(testDNSQuery); !bytes.Equal(got, answer) {
		t.Fatalf("DoH answer = %x", got)
	}
	available.Store(false)
	if got := proxy.resolve(testDNSQuery); !bytes.Equal(got, answer) {
		t.Fatalf("fallback answer = %x", got)
	}
	bad := append([]byte(nil), answer...)
	bad[0]++
	if validDNSMessage(testDNSQuery, bad) {
		t.Fatal("accepted response with different DNS transaction ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := proxy.ordinary(ctx, testDNSQuery); err != nil {
		t.Fatal(err)
	}
}
