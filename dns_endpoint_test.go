package main

import "testing"

func TestValidateDNSEndpoint(t *testing.T) {
	for _, address := range []string{"https://dns.example.com/dns-query?token=private", "tls://dns.example.com", "tls://1.1.1.1:853"} {
		if _, err := validateDNSEndpoint(address); err != nil {
			t.Errorf("rejected %q: %v", address, err)
		}
	}
	for _, address := range []string{"http://dns.example.com", "tls://localhost", "tls://dns.example.com/path", "tls://user@dns.example.com", "tls://dns.example.com:65536", "tls://dns.example.com?token=x"} {
		if _, err := validateDNSEndpoint(address); err == nil {
			t.Errorf("accepted %q", address)
		}
	}
}
