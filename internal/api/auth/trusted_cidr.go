package auth

import (
	"net"
	"os"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
)

var defaultTrustedCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
}

func parseCIDRList(raw string) []*net.IPNet {
	if raw == "" {
		return nil
	}
	var cidrs []*net.IPNet
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			logging.Warnf("Ignoring invalid CIDR in JAVINIZER_SETUP_TRUSTED_CIDRS: %s", s)
			continue
		}
		cidrs = append(cidrs, n)
	}
	return cidrs
}

// computeTrustedCIDRs returns the merged list of default and env-configured
// CIDRs. The envLookup function is used to read JAVINIZER_SETUP_TRUSTED_CIDRS;
// when nil, os.Getenv is used.
func computeTrustedCIDRs(envLookup func(key string) string) []*net.IPNet {
	if envLookup == nil {
		envLookup = os.Getenv
	}
	var cidrs []*net.IPNet
	for _, s := range defaultTrustedCIDRs {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			continue
		}
		cidrs = append(cidrs, n)
	}
	if extra := envLookup("JAVINIZER_SETUP_TRUSTED_CIDRS"); extra != "" {
		cidrs = append(cidrs, parseCIDRList(extra)...)
	}
	return cidrs
}

// isTrustedClient checks whether ipStr belongs to a trusted CIDR.
// The envLookup function is used to resolve environment-configured CIDRs;
// when nil, os.Getenv is used.
func isTrustedClient(ipStr string, envLookup func(key string) string) bool {
	ipStr = strings.TrimPrefix(strings.TrimSuffix(ipStr, "]"), "[")
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range computeTrustedCIDRs(envLookup) {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
