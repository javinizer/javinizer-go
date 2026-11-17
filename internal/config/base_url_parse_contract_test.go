package config

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// Both public entry points must reject malformed URLs before the scraper host
// policy runs, with the same field-specific diagnostic and wrapped parse error.
func TestBaseURLValidationSharesParseDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		raw        string
		diagnostic string
		parseError bool
	}{
		{" ", "must be a valid HTTP or HTTPS URL", false},
		{"ftp://example.com", "must be a valid HTTP or HTTPS URL", false},
		{"http://", "must be a valid HTTP or HTTPS URL with a host", false},
		{"http:///path", "must be a valid HTTP or HTTPS URL with a host", false},
		{"https://example.com:bad", "must be a valid URL:", true},
		{"https://example.com/%zz", "must be a valid URL:", true},
		{"https://example.com\n/path", "must be a valid URL:", true},
	} {
		raw := tc.raw
		t.Run(raw, func(t *testing.T) {
			general := ValidateHTTPBaseURL("scrapers.sample.base_url", raw)
			scraper := ValidateScraperBaseURL("scrapers.sample.base_url", raw, []string{"example.com"})
			require.ErrorContains(t, general, "scrapers.sample.base_url "+tc.diagnostic)
			require.EqualError(t, scraper, general.Error())
			var generalParse, scraperParse *url.Error
			require.Equal(t, tc.parseError, errors.As(general, &generalParse))
			require.Equal(t, tc.parseError, errors.As(scraper, &scraperParse))
			if generalParse != nil {
				require.Equal(t, generalParse.Error(), scraperParse.Error())
			}
		})
	}
}

func TestScraperBaseURLParsedHostPolicy(t *testing.T) {
	require.NoError(t, ValidateScraperBaseURL("source", "", nil))
	for _, raw := range []string{"https://EXAMPLE.COM", " \thttps://example.com:8443/base?q=1#part\n", "http://user:pass@example.com/path"} {
		require.NoError(t, ValidateScraperBaseURL("source", raw, []string{" EXAMPLE.com "}))
	}
	for _, raw := range []string{"https://example.com.evil.invalid", "https://example.com@evil.invalid", "https://evil.invalid/?host=example.com"} {
		require.NoError(t, ValidateHTTPBaseURL("source", raw))
		require.ErrorContains(t, ValidateScraperBaseURL("source", raw, []string{"example.com"}), "not in the allowed list")
	}
	require.ErrorContains(t, ValidateScraperBaseURL("source", "https://example.com", nil), "not in the allowed list")
}
