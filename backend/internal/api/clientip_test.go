package api

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

// FR-A11, NFR-3: the per-IP limits and audit IPs see the real client behind the ingress. X-Forwarded-For counts only
// when the TCP peer is a trusted proxy, and the walk from the right stops at the first untrusted address.
func TestClientIP_FRA11(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")}
	tests := []struct {
		name, peer string
		xff        []string // one entry per header line
		trusted    []netip.Prefix
		want       string
	}{
		{"no trusted proxies ignores header", "10.1.1.1:5000", []string{"203.0.113.9"}, nil, "10.1.1.1"},
		{"untrusted peer with spoofed header", "198.51.100.7:5000", []string{"203.0.113.9"}, trusted, "198.51.100.7"},
		{"trusted peer, one client", "10.1.1.1:5000", []string{"203.0.113.9"}, trusted, "203.0.113.9"},
		{"trusted peer, no header", "10.1.1.1:5000", nil, trusted, "10.1.1.1"},
		{"spoofed left entry is skipped", "10.1.1.1:5000", []string{"1.2.3.4, 203.0.113.9"}, trusted, "203.0.113.9"},
		{"several trusted hops", "10.1.1.1:5000", []string{"203.0.113.9, 10.2.2.2", "10.3.3.3"}, trusted, "203.0.113.9"},
		{"all hops trusted", "10.1.1.1:5000", []string{"10.2.2.2, 10.3.3.3"}, trusted, "10.1.1.1"},
		{"malformed entry", "10.1.1.1:5000", []string{"203.0.113.9, not-an-ip"}, trusted, "10.1.1.1"},
		{"empty entry", "10.1.1.1:5000", []string{"203.0.113.9, "}, trusted, "10.1.1.1"},
		{"IPv6 peer and client", "[fd00::1]:5000", []string{"2001:db8::5, fd00::2"}, trusted, "2001:db8::5"},
		{"IPv4-mapped trusted peer", "[::ffff:10.1.1.1]:5000", []string{"203.0.113.9"}, trusted, "203.0.113.9"},
		{"IPv6 untrusted peer", "[2001:db8::9]:5000", []string{"203.0.113.9"}, trusted, "2001:db8::9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.peer
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			s := &Server{TrustedProxies: tt.trusted}
			if got := s.clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
