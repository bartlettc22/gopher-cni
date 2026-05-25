package utils

import (
	"fmt"
	"net"
	"strings"
)

// ParseIPOrCIDRList parses a comma-separated list of IP addresses and CIDR
// blocks. Plain IP addresses are returned as /32 (IPv4) or /128 (IPv6) host
// routes. Returns an error if any token is neither a valid IP nor a valid CIDR.
func ParseIPOrCIDRList(raw string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			_, cidr, err := net.ParseCIDR(s)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", s, err)
			}
			nets = append(nets, cidr)
		} else {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address %q", s)
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets, nil
}
