// Package webfetch SSRF (Server-Side Request Forgery) prevention.
//
// Prompt injection can trick the agent into fetching internal URLs like
// 169.254.169.254 (EC2 metadata endpoint). On EC2 with IMDSv2 (now the
// default), metadata requires a PUT token so a simple GET won't leak
// credentials. Older instances on IMDSv1 (HttpTokens=optional) are
// still vulnerable. Lambda does not expose 169.254.169.254 at all;
// its metadata uses a separate authenticated endpoint.
// We block all private IPs regardless as defense in depth, especially
// for the future EC2 workspace environment.
// https://docs.aws.amazon.com/lambda/latest/dg/configuration-metadata-endpoint.html
// This package blocks all private, loopback, and link-local addresses
// at both pre-flight and connect time.
//
// Defense is two-layered:
//  1. Pre-flight: isObviousPrivateHost blocks literal private hostnames/IPs
//     before any DNS lookup.
//  2. Connect-time: safeDialContext re-validates resolved IPs at dial time
//     to catch DNS rebinding attacks where a hostname resolves to a public
//     IP initially but a private IP at connect time.
//
// Reference: picoclaw/pkg/tools/integration/web.go (lines 1877-2116)
package webfetch

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	maxRedirects  = 5
	dialTimeout   = 15 * time.Second
	dialKeepAlive = 30 * time.Second
)

// newSafeHTTPClient returns an http.Client with SSRF protections:
//   - Custom dialer that validates resolved IPs at connect time
//   - Redirect checker that blocks redirects to private hosts
//   - Configurable timeout
func newSafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialKeepAlive,
	}

	transport := &http.Transport{
		DialContext: safeDialContext(dialer),
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			// Block redirects to private/local hosts.
			if isObviousPrivateHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect target is a private or local network host")
			}
			return nil
		},
	}
}

// safeDialContext wraps a dialer to validate resolved IPs at connect time.
// This prevents DNS rebinding attacks where a hostname resolves to a public
// IP during pre-flight checks but a private IP when the actual connection
// is made.
func safeDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", address, err)
		}

		// If host is already an IP literal, validate directly.
		if ip := net.ParseIP(host); ip != nil {
			if isPrivateOrRestrictedIP(ip) {
				return nil, fmt.Errorf("blocked private or local target: %s", host)
			}
			return dialer.DialContext(ctx, network, address)
		}

		// Resolve hostname and validate each IP.
		ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", host, err)
		}

		attempted := 0
		var lastErr error
		for _, ipAddr := range ipAddrs {
			if isPrivateOrRestrictedIP(ipAddr.IP) {
				continue // skip private IPs silently
			}
			attempted++
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}

		if attempted == 0 {
			return nil, fmt.Errorf("all resolved addresses for %s are private or restricted", host)
		}
		return nil, fmt.Errorf("failed to connect to %s: %w", host, lastErr)
	}
}

// isObviousPrivateHost is a lightweight pre-flight check that catches
// obviously private hostnames without performing DNS resolution.
// The real SSRF guard is safeDialContext which checks IPs at connect time.
func isObviousPrivateHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimSuffix(h, ".")

	if h == "" {
		return true
	}

	// localhost and *.localhost
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}

	// Literal IP address check.
	if ip := net.ParseIP(h); ip != nil {
		return isPrivateOrRestrictedIP(ip)
	}

	return false
}

// isPrivateOrRestrictedIP returns true for IPs that should never be fetched:
//
// IPv4:
//   - 10.0.0.0/8 RFC 1918 private
//   - 172.16.0.0/12 RFC 1918 private
//   - 192.168.0.0/16 RFC 1918 private
//   - 127.0.0.0/8 loopback
//   - 0.0.0.0/8 "this" network
//   - 169.254.0.0/16 link-local (includes AWS metadata at 169.254.169.254)
//   - 100.64.0.0/10 carrier-grade NAT (RFC 6598)
//
// IPv6:
//   - ::1 loopback
//   - fe80::/10 link-local
//   - fc00::/7 unique-local addresses
//   - 2002::/16 6to4 (checks embedded IPv4)
//   - 2001:0000::/32 Teredo (checks XOR-inverted client IPv4)
//
// https://www.postgresql.org/docs/16/errcodes-appendix.html (referenced for completeness)
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
func isPrivateOrRestrictedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}

	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 127.0.0.0/8
		if ip4[0] == 127 {
			return true
		}
		// 0.0.0.0/8
		if ip4[0] == 0 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 169.254.0.0/16 (link-local, AWS metadata endpoint)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 100.64.0.0/10 (carrier-grade NAT, RFC 6598)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		return false
	}

	// IPv6 checks.
	if len(ip) == net.IPv6len {
		// fc00::/7 unique-local addresses
		if (ip[0] & 0xfe) == 0xfc {
			return true
		}
		// 2002::/16 (6to4): embedded IPv4 is at bytes [2:6].
		if ip[0] == 0x20 && ip[1] == 0x02 {
			embedded := net.IPv4(ip[2], ip[3], ip[4], ip[5])
			return isPrivateOrRestrictedIP(embedded)
		}
		// 2001:0000::/32 (Teredo): client IPv4 at bytes [12:16], XOR-inverted.
		if ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x00 && ip[3] == 0x00 {
			client := net.IPv4(ip[12]^0xff, ip[13]^0xff, ip[14]^0xff, ip[15]^0xff)
			return isPrivateOrRestrictedIP(client)
		}
	}

	return false
}
