package webfetch

import (
	"net"
	"testing"
)

// Verify that common private IPv4 ranges are blocked.
func TestIsPrivateOrRestrictedIP_IPv4Private(t *testing.T) {
	blocked := []struct {
		name string
		ip   string
	}{
		{"loopback", "127.0.0.1"},
		{"loopback high", "127.255.255.255"},
		{"10.x private", "10.0.0.1"},
		{"10.x private high", "10.255.255.255"},
		{"172.16.x private", "172.16.0.1"},
		{"172.31.x private", "172.31.255.255"},
		{"192.168.x private", "192.168.1.1"},
		{"link-local", "169.254.1.1"},
		{"AWS metadata endpoint", "169.254.169.254"},
		{"carrier-grade NAT low", "100.64.0.1"},
		{"carrier-grade NAT high", "100.127.255.255"},
		{"zero network", "0.0.0.0"},
		{"zero network", "0.1.2.3"},
	}

	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			if !isPrivateOrRestrictedIP(ip) {
				t.Errorf("expected %s to be blocked", tc.ip)
			}
		})
	}
}

// Verify that public IPv4 addresses are allowed.
func TestIsPrivateOrRestrictedIP_IPv4Public(t *testing.T) {
	allowed := []struct {
		name string
		ip   string
	}{
		{"Google DNS", "8.8.8.8"},
		{"Cloudflare DNS", "1.1.1.1"},
		{"random public", "203.0.113.1"},
		{"172.15.x (just below private)", "172.15.255.255"},
		{"172.32.x (just above private)", "172.32.0.1"},
		{"100.63.x (just below CGNAT)", "100.63.255.255"},
		{"100.128.x (just above CGNAT)", "100.128.0.1"},
	}

	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			if isPrivateOrRestrictedIP(ip) {
				t.Errorf("expected %s to be allowed", tc.ip)
			}
		})
	}
}

// Verify that IPv6 private/restricted addresses are blocked.
func TestIsPrivateOrRestrictedIP_IPv6(t *testing.T) {
	blocked := []struct {
		name string
		ip   string
	}{
		{"loopback", "::1"},
		{"unique-local", "fc00::1"},
		{"unique-local fd", "fd12:3456:789a::1"},
		{"link-local", "fe80::1"},
		{"6to4 with private embed (10.x)", "2002:0a00:0001::1"},
		{"Teredo with private client (inverted 10.0.0.1 = 245.255.255.254)", "2001:0000:0000:0000:0000:0000:f5ff:fffe"},
	}

	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			if !isPrivateOrRestrictedIP(ip) {
				t.Errorf("expected %s to be blocked", tc.ip)
			}
		})
	}
}

// Verify that public IPv6 addresses are allowed.
func TestIsPrivateOrRestrictedIP_IPv6Public(t *testing.T) {
	allowed := []struct {
		name string
		ip   string
	}{
		{"Google DNS", "2001:4860:4860::8888"},
		{"Cloudflare DNS", "2606:4700:4700::1111"},
	}

	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			if isPrivateOrRestrictedIP(ip) {
				t.Errorf("expected %s to be allowed", tc.ip)
			}
		})
	}
}

// Verify that nil IP is treated as restricted.
func TestIsPrivateOrRestrictedIP_Nil(t *testing.T) {
	if !isPrivateOrRestrictedIP(nil) {
		t.Error("nil IP should be treated as restricted")
	}
}

// Verify that isObviousPrivateHost catches common private hostnames.
func TestIsObviousPrivateHost(t *testing.T) {
	blocked := []struct {
		name string
		host string
	}{
		{"localhost", "localhost"},
		{"subdomain of localhost", "foo.localhost"},
		{"empty string", ""},
		{"literal loopback", "127.0.0.1"},
		{"literal private", "10.0.0.1"},
		{"AWS metadata", "169.254.169.254"},
		{"trailing dot localhost", "localhost."},
	}

	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			if !isObviousPrivateHost(tc.host) {
				t.Errorf("expected %q to be blocked", tc.host)
			}
		})
	}
}

// Verify that public hostnames pass the pre-flight check.
func TestIsObviousPrivateHost_Public(t *testing.T) {
	allowed := []struct {
		name string
		host string
	}{
		{"normal domain", "example.com"},
		{"subdomain", "api.example.com"},
		{"public IP", "8.8.8.8"},
	}

	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			if isObviousPrivateHost(tc.host) {
				t.Errorf("expected %q to be allowed", tc.host)
			}
		})
	}
}

// Verify that IPv4-mapped IPv6 loopback is blocked.
func TestIsPrivateOrRestrictedIP_IPv4MappedIPv6(t *testing.T) {
	ip := net.ParseIP("::ffff:127.0.0.1")
	if ip == nil {
		t.Fatal("failed to parse ::ffff:127.0.0.1")
	}
	if !isPrivateOrRestrictedIP(ip) {
		t.Error("IPv4-mapped IPv6 loopback should be blocked")
	}
}
