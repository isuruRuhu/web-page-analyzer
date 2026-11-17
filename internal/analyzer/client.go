package analyzer

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// reserved lists address ranges that are not on the public internet and are
// not already covered by the netip.Addr checks in isPublic.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // "this" network
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved, includes broadcast
	netip.MustParsePrefix("64:ff9b::/96"),  // NAT64, maps to IPv4 addresses
}

// NewClient returns the HTTP client for an Analyzer. Unless allowPrivate is
// set, it refuses to connect to addresses that are not on the public
// internet, so that users cannot make the server request its own network.
func NewClient(timeout time.Duration, allowPrivate bool) *http.Client {
	if allowPrivate {
		return &http.Client{Timeout: timeout}
	}
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   publicOnly,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	// With a proxy the dialer would only ever see the proxy's address.
	transport.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: transport}
}

// publicOnly is a net.Dialer Control function. It runs after the host name
// is resolved and before each connection is made, so it also covers
// redirects, the links on a page and names that resolve to a private address.
func publicOnly(network, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, address)
	}
	if !isPublic(addrPort.Addr()) {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, addrPort.Addr())
	}
	return nil
}

// isPublic reports whether ip is an address on the public internet.
func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
