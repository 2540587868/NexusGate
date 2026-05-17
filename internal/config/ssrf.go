package config

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
)

var privateNets []net.IPNet

func init() {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range privateRanges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		privateNets = append(privateNets, *ipNet)
	}
}

func IsPrivateAddress(address string) bool {
	host := address
	if strings.HasPrefix(host, "http://") {
		host = strings.TrimPrefix(host, "http://")
	} else if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	ip := net.ParseIP(host)
	if ip == nil {
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			return false
		}
		ip = ips[0]
	}

	for _, pn := range privateNets {
		if pn.Contains(ip) {
			return true
		}
	}
	return false
}

func ValidateNoSSRF(cfg *Config) error {
	allowPrivate := cfg.ConfigStore.AllowPrivateBackends
	for i, rc := range cfg.Routes {
		for j, b := range rc.Backend {
			if IsPrivateAddress(b.Address) {
				if allowPrivate {
					slog.Warn("backend address is private/internal",
						"address", b.Address,
						"route_index", i,
						"backend_index", j,
					)
				} else {
					return fmt.Errorf("routes[%d].backend[%d].address %q is a private/internal address (SSRF risk); set config.allow_private_backends=true to allow", i, j, b.Address)
				}
			}
		}
	}
	return nil
}
