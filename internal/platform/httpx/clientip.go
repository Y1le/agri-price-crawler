package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// ParseTrustedProxies parses and normalizes exact IP addresses and CIDR ranges.
func ParseTrustedProxies(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errors.New("trusted proxy entry is empty")
		}

		if address, err := netip.ParseAddr(value); err == nil {
			if address.Zone() != "" {
				return nil, fmt.Errorf("trusted proxy %q contains an IPv6 zone", value)
			}
			address = address.Unmap()
			prefixes = append(prefixes, netip.PrefixFrom(address, address.BitLen()))
			continue
		}

		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy %q: %w", value, err)
		}
		prefix, err = normalizePrefix(prefix)
		if err != nil {
			return nil, fmt.Errorf("normalize trusted proxy %q: %w", value, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

// ClientIP returns a parsed client address without trusting attacker-controlled
// forwarding data unless the immediate peer is explicitly trusted.
func ClientIP(r *http.Request, trusted []netip.Prefix) (netip.Addr, error) {
	peer, err := parseRemoteAddress(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}
	if !addressTrusted(peer, trusted) {
		return peer, nil
	}

	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		return peer, nil
	}
	forwarded, err := parseForwardedFor(values)
	if err != nil {
		return netip.Addr{}, err
	}

	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate := forwarded[index]
		if !addressTrusted(candidate, trusted) {
			return candidate, nil
		}
	}
	return forwarded[0], nil
}

func parseRemoteAddress(remote string) (netip.Addr, error) {
	host, port, err := net.SplitHostPort(remote)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse remote address: %w", err)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number > 65535 {
		return netip.Addr{}, errors.New("parse remote address: invalid port")
	}
	address, err := netip.ParseAddr(host)
	if err != nil || address.Zone() != "" {
		return netip.Addr{}, errors.New("parse remote address: invalid IP")
	}
	return address.Unmap(), nil
}

func parseForwardedFor(values []string) ([]netip.Addr, error) {
	addresses := make([]netip.Addr, 0, len(values))
	for _, line := range values {
		for member := range strings.SplitSeq(line, ",") {
			member = strings.TrimSpace(member)
			if member == "" {
				return nil, errors.New("parse X-Forwarded-For: empty entry")
			}
			address, err := netip.ParseAddr(member)
			if err != nil || address.Zone() != "" {
				return nil, fmt.Errorf("parse X-Forwarded-For entry %q: invalid IP", member)
			}
			addresses = append(addresses, address.Unmap())
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("parse X-Forwarded-For: no addresses")
	}
	return addresses, nil
}

func addressTrusted(address netip.Addr, trusted []netip.Prefix) bool {
	address = address.Unmap()
	for _, prefix := range trusted {
		normalized, err := normalizePrefix(prefix)
		if err == nil && normalized.Contains(address) {
			return true
		}
	}
	return false
}

func normalizePrefix(prefix netip.Prefix) (netip.Prefix, error) {
	if !prefix.IsValid() || prefix.Addr().Zone() != "" {
		return netip.Prefix{}, errors.New("invalid prefix")
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return netip.Prefix{}, errors.New("IPv4-mapped prefix length must be at least 96")
		}
		address := prefix.Addr().Unmap()
		return netip.PrefixFrom(address, prefix.Bits()-96).Masked(), nil
	}
	return prefix.Masked(), nil
}
