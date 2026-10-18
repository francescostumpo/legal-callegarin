package clientinfo

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type Identity struct {
	Address   netip.Addr
	Scheme    string
	Forwarded bool
}

func Resolve(request *http.Request, trustedHops int) (Identity, error) {
	if request == nil {
		return Identity{}, errors.New("request is required")
	}
	remote, err := parseRemoteAddress(request.RemoteAddr)
	if err != nil {
		return Identity{}, err
	}
	identity := Identity{Address: remote, Scheme: requestScheme(request)}
	if trustedHops <= 0 {
		return identity, nil
	}

	parts := strings.Split(request.Header.Get("X-Forwarded-For"), ",")
	if len(parts) < trustedHops || len(parts) == 1 && strings.TrimSpace(parts[0]) == "" {
		return identity, nil
	}
	addresses := make([]netip.Addr, len(parts))
	for index, part := range parts {
		address, parseErr := parseIPLiteral(strings.TrimSpace(part))
		if parseErr != nil {
			return identity, nil
		}
		addresses[index] = address
	}
	clientIndex := len(addresses) - trustedHops
	if clientIndex < 0 {
		return identity, nil
	}
	identity.Address = addresses[clientIndex]
	identity.Forwarded = true

	protocols := strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")
	if len(protocols) >= trustedHops {
		protocolIndex := len(protocols) - trustedHops
		protocol := strings.ToLower(strings.TrimSpace(protocols[protocolIndex]))
		if protocol == "http" || protocol == "https" {
			identity.Scheme = protocol
		}
	}
	return identity, nil
}

func requestScheme(request *http.Request) string {
	if request.TLS != nil {
		return "https"
	}
	if request.URL != nil && (request.URL.Scheme == "http" || request.URL.Scheme == "https") {
		return request.URL.Scheme
	}
	return "http"
}

func parseRemoteAddress(raw string) (netip.Addr, error) {
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	return parseIPLiteral(strings.Trim(raw, "[]"))
}

func parseIPLiteral(raw string) (netip.Addr, error) {
	if raw == "" {
		return netip.Addr{}, errors.New("client address is invalid")
	}
	address, err := netip.ParseAddr(strings.Trim(raw, "[]"))
	if err != nil {
		return netip.Addr{}, errors.New("client address is invalid")
	}
	return address.Unmap(), nil
}
