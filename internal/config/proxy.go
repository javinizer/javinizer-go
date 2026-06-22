package config

import "strings"

// ResolveScraperUserAgent resolves the effective User-Agent for a scraper.
// Priority: scraper config override → scraper module default → Chrome UA fallback.
func ResolveScraperUserAgent(userAgent string) string {
	if ua := strings.TrimSpace(userAgent); ua != "" {
		return ua
	}
	return DefaultScraperUserAgent
}
