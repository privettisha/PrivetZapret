package main

import "testing"

func TestMediaProbeDistinguishesSiteFromPlaybackHost(t *testing.T) {
	if usableProbe("youtube", youtubeMediaProbeURL, EndpointCheck{Reachable: true, HTTPCode: 403}) {
		t.Fatal("blocked Google video response must not select a profile")
	}
	if !usableProbe("youtube", youtubeMediaProbeURL, EndpointCheck{Reachable: true, HTTPCode: 200}) {
		t.Fatal("healthy Google video response must count")
	}
	if !usableProbe("soundcloud", soundcloudMediaProbeURL, EndpointCheck{Reachable: true, HTTPCode: 403}) {
		t.Fatal("expected CDN root response must count as reachable")
	}
	if usableProbe("soundcloud", soundcloudMediaProbeURL, EndpointCheck{Reachable: false, HTTPCode: 403}) {
		t.Fatal("a failed connection must not count as reachable")
	}
	if usableProbe("youtube", "https://www.youtube.com/generate_204", EndpointCheck{Reachable: true, HTTPCode: 500}) {
		t.Fatal("server errors must not count as working")
	}
}
