package minnanoav

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
)

func validateScraperSettings(ss *models.ScraperSettings) error {
	u, err := url.Parse(strings.TrimSpace(ss.BaseURL))
	if err != nil {
		return err
	}
	if u.Scheme != "" && u.Hostname() != "" {
		// Configured mirrors are fine; private/loopback/link-local targets are
		// never scrapable — the setting must not turn the scraper into an
		// internal-request primitive (codex round 10). Validation here stays
		// deterministic and offline-safe: only lexical host checks run at
		// config time; DNS-dependent host/IP enforcement runs at request time
		// in the SSRF-pinned transport (codex round 11 — a DNS outage must
		// not make startup/hot-reload reject the valid default base URL).
		if ssrf.IsBlockedHost(u.Hostname()) {
			return fmt.Errorf("minnanoav.base_url %q targets a blocked or internal host", ss.BaseURL)
		}
	}
	return config.ValidateHTTPBaseURL("minnanoav.base_url", ss.BaseURL)
}
