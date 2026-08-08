package minnanoav

import (
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
		// internal-request primitive (codex round 10).
		if err := ssrf.CheckURL(ss.BaseURL); err != nil {
			return err
		}
	}
	return config.ValidateHTTPBaseURL("minnanoav.base_url", ss.BaseURL)
}
