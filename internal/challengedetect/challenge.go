// Package challengedetect provides utilities for detecting Cloudflare
// anti-bot challenge pages from HTTP response bodies.
//
// Extracted as a leaf package (zero internal dependencies) so that both
// models (for backward-compatible re-export) and scraper packages can
// import it without creating circular dependencies.
package challengedetect

import "strings"

// IsCloudflareChallengePage detects Cloudflare anti-bot/interstitial challenge pages
// from HTML content. These pages are often returned with HTTP 200.
func IsCloudflareChallengePage(body string) bool {
	lower := strings.ToLower(strings.TrimSpace(body))
	if lower == "" {
		return false
	}

	// High confidence markers for actual Cloudflare challenge pages.
	// Note: /cdn-cgi/challenge-platform/scripts/jsd/ is used by legitimate pages,
	// so we check for the challenge orchestration endpoint specifically.
	highConfidence := []string{
		"/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page/v1",
		"/cdn-cgi/challenge-platform/h/b/orchestrate/chl_hr/v1",
		"/cdn-cgi/challenge-platform/h/b/orchestrate/chl_page",
		"cf-browser-verification",
		"cf_chl_",
		"cf-challenge",
		"checking your browser before accessing",
		"enable javascript and cookies to continue",
		"ddos protection by cloudflare",
	}
	for _, marker := range highConfidence {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	// Fallback scoring for softer variants.
	score := 0
	for _, marker := range []string{
		"cloudflare",
		"attention required",
		"just a moment",
		"ray id",
		"cf-ray",
		"/cdn-cgi/",
		"captcha",
		"turnstile",
	} {
		if strings.Contains(lower, marker) {
			score++
		}
	}

	return score >= 3
}
