package utils

import (
	"net"
	"strings"
)

// ParseCIDRs parses a list of CIDR strings (bare IPs are accepted as /32 or
// /128), silently skipping malformed entries.
func ParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil {
				if ip.To4() != nil {
					c += "/32"
				} else {
					c += "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// IPAllowed reports whether ip falls within any of nets. An empty nets slice
// means "no restriction" and always returns true.
func IPAllowed(nets []*net.IPNet, ip net.IP) bool {
	if len(nets) == 0 {
		return true
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// HostAllowed resolves host (an IP literal or hostname) and reports whether it
// is permitted by nets. For hostnames, every resolved address must be allowed
// (fail closed on a split-horizon or rebinding trick). Empty nets = allowed.
func HostAllowed(nets []*net.IPNet, host string) bool {
	if len(nets) == 0 {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return IPAllowed(nets, ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !IPAllowed(nets, ip) {
			return false
		}
	}
	return true
}
